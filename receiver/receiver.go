package receiver

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"sync"

	"go.opentelemetry.io/collector/consumer/consumererror"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"

	"github.com/kedify/otel-add-on/types"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componentstatus"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/receiver/otlpreceiver"
	"go.opentelemetry.io/collector/receiver/receiverhelper"
)

const (
	countSuffix = "_count"
	sumSuffix   = "_sum"
	hostName    = "host.name"
)

// otlpReceiver is the type that exposes Trace and Metrics reception.
type otlpReceiver struct {
	cfg        *otlpreceiver.Config
	serverGRPC *grpc.Server

	nextMetrics consumer.Metrics
	shutdownWG  sync.WaitGroup

	obsrepGRPC *receiverhelper.ObsReport

	settings       *receiver.Settings
	metricMemStore types.MemStore
	debug          bool
}

// NewOtlpReceiver just creates the OpenTelemetry receiver services. It is the caller's
// responsibility to invoke the respective Start*Reception methods as well
// as the various Stop*Reception methods to end it.
func NewOtlpReceiver(cfg *otlpreceiver.Config, set *receiver.Settings, memStore types.MemStore, debug bool) (*otlpReceiver, error) {
	r := &otlpReceiver{
		cfg:            cfg,
		nextMetrics:    nil,
		settings:       set,
		metricMemStore: memStore,
		debug:          debug,
	}

	var err error
	r.obsrepGRPC, err = receiverhelper.NewObsReport(receiverhelper.ObsReportSettings{
		ReceiverID:             set.ID,
		Transport:              "grpc",
		ReceiverCreateSettings: *set,
	})
	if err != nil {
		return nil, err
	}

	return r, nil
}

func (r *otlpReceiver) startGRPCServer(host component.Host) error {
	// If GRPC is not enabled, nothing to start.
	if r.cfg.GRPC == nil {
		return nil
	}

	var err error
	if r.serverGRPC, err = r.cfg.GRPC.ToServer(context.Background(), host, r.settings.TelemetrySettings); err != nil {
		return err
	}

	if r.nextMetrics != nil {
		pmetricotlp.RegisterGRPCServer(r.serverGRPC, New(r.nextMetrics, r.obsrepGRPC, r.metricMemStore, r.debug))
	}

	r.settings.Logger.Info("Starting GRPC server", zap.String("endpoint", r.cfg.GRPC.NetAddr.Endpoint))
	var gln net.Listener
	if gln, err = r.cfg.GRPC.NetAddr.Listen(context.Background()); err != nil {
		return err
	}

	r.shutdownWG.Add(1)
	go func() {
		defer r.shutdownWG.Done()

		if errGrpc := r.serverGRPC.Serve(gln); errGrpc != nil && !errors.Is(errGrpc, grpc.ErrServerStopped) {
			componentstatus.ReportStatus(host, componentstatus.NewFatalErrorEvent(errGrpc))
		}
	}()
	return nil
}

// Start runs the trace receiver on the gRPC server. Currently
// it also enables the metrics receiver too.
func (r *otlpReceiver) Start(_ context.Context, host component.Host) error {
	if err := r.startGRPCServer(host); err != nil {
		return err
	}

	return nil
}

// Shutdown is a method to turn off receiving.
func (r *otlpReceiver) Shutdown(_ context.Context) error {
	var err error

	if r.serverGRPC != nil {
		r.serverGRPC.GracefulStop()
	}

	r.shutdownWG.Wait()
	return err
}

func (r *otlpReceiver) RegisterMetricsConsumer(mc consumer.Metrics) {
	r.nextMetrics = mc
}

const dataFormatProtobuf = "protobuf"

// Receiver is the type used to handle metrics from OpenTelemetry exporters.
type Receiver struct {
	pmetricotlp.UnimplementedGRPCServer
	nextConsumer   consumer.Metrics
	obsreport      *receiverhelper.ObsReport
	metricMemStore types.MemStore
	debug          bool
}

// New creates a new Receiver reference.
func New(nextConsumer consumer.Metrics, obsreport *receiverhelper.ObsReport, memStore types.MemStore, debug bool) *Receiver {
	return &Receiver{
		nextConsumer:   nextConsumer,
		obsreport:      obsreport,
		metricMemStore: memStore,
		debug:          debug,
	}
}

// HasCountSum common denominator iface for HistogramDataPoint, ExponentialHistogramDataPoint and SummaryDataPoint
type HasCountSum interface {
	Sum() float64
	Count() uint64
	Timestamp() pcommon.Timestamp
	Attributes() pcommon.Map
}

// Export implements the service Export metrics func.
func (r *Receiver) Export(ctx context.Context, req pmetricotlp.ExportRequest) (pmetricotlp.ExportResponse, error) {
	md := req.Metrics()
	dataPointCount := md.DataPointCount()
	if dataPointCount == 0 {
		return pmetricotlp.NewExportResponse(), nil
	}
	// using the printf instead of logger makes the metric data nicer in logs
	r.p("\n\nData point count: %d\n", dataPointCount)
	resLen := md.ResourceMetrics().Len()
	for i := 0; i < resLen; i++ {
		sm := md.ResourceMetrics().At(i).ScopeMetrics()
		pod, podFound := md.ResourceMetrics().At(i).Resource().Attributes().AsRaw()[hostName]
		if !podFound {
			pod, podFound = md.ResourceMetrics().At(i).Resource().Attributes().AsRaw()["pod"]
		}
		smLen := sm.Len()
		for j := 0; j < smLen; j++ {
			mLen := sm.At(j).Metrics().Len()
			metrics := sm.At(j).Metrics()
			for k := 0; k < mLen; k++ {
				mType := metrics.At(k).Type()
				r.p("-  name: %+v\n", metrics.At(k).Name())
				if mType == pmetric.MetricTypeHistogram || mType == pmetric.MetricTypeExponentialHistogram || mType == pmetric.MetricTypeSummary {
					r.p("   exposed as: %+v\n", metrics.At(k).Name()+countSuffix)
					r.p("   exposed as: %+v\n", metrics.At(k).Name()+sumSuffix)
				}
				r.p("   type: %+v\n", metrics.At(k).Type())
				metricName := metrics.At(k).Name()
				var dataPoints pmetric.NumberDataPointSlice
				switch mType {
				case pmetric.MetricTypeGauge:
					dataPoints = metrics.At(k).Gauge().DataPoints()
					r.storeDatapoints(metricName, dataPoints, pod, podFound)
				case pmetric.MetricTypeSum:
					dataPoints = metrics.At(k).Sum().DataPoints()
					r.storeDatapoints(metricName, dataPoints, pod, podFound)
				case pmetric.MetricTypeHistogram:
					histograms := metrics.At(k).Histogram().DataPoints()
					for m := 0; m < histograms.Len(); m++ {
						histogram := histograms.At(m)
						r.storeCountAndSumDatapoints(metricName, histogram, pod, podFound)
					}
				case pmetric.MetricTypeExponentialHistogram:
					exHistograms := metrics.At(k).ExponentialHistogram().DataPoints()
					for m := 0; m < exHistograms.Len(); m++ {
						exHistogram := exHistograms.At(m)
						r.storeCountAndSumDatapoints(metricName, exHistogram, pod, podFound)
					}
				case pmetric.MetricTypeSummary:
					summaryDataPoints := metrics.At(k).Summary().DataPoints()
					for m := 0; m < summaryDataPoints.Len(); m++ {
						summaryDataPoint := summaryDataPoints.At(m)
						r.storeCountAndSumDatapoints(metricName, summaryDataPoint, pod, podFound)
					}
				default:
					// ignore others (MetricTypeEmpty)
					return pmetricotlp.NewExportResponse(), nil
				}
			}
		}
	}

	ctx = r.obsreport.StartMetricsOp(ctx)
	err := r.nextConsumer.ConsumeMetrics(ctx, md)
	r.obsreport.EndMetricsOp(ctx, dataFormatProtobuf, dataPointCount, err)

	if err != nil {
		return pmetricotlp.NewExportResponse(), GetStatusFromError(err)
	}

	return pmetricotlp.NewExportResponse(), nil
}

func (r *Receiver) p(format string, a ...any) {
	if r.debug {
		fmt.Printf(format, a...)
	}
}

func addPodLabel(orig map[string]any, name any, found bool) map[string]any {
	if found {
		orig[hostName] = name
	}
	return orig
}

func (r *Receiver) storeDatapoints(name string, dataPoints pmetric.NumberDataPointSlice, podName any, podFound bool) {
	for l := 0; l < dataPoints.Len(); l++ {
		datapoint := dataPoints.At(l)
		r.p("     - time: %+v\n", datapoint.Timestamp())
		r.p("       tags: %+v\n", datapoint.Attributes().AsRaw())
		value := math.Max(datapoint.DoubleValue(), float64(datapoint.IntValue()))
		r.p("       value: %+v\n", value)
		r.metricMemStore.Put(types.NewMetricEntry{
			Name:             types.MetricName(name),
			MeasurementValue: value,
			MeasurementTime:  datapoint.Timestamp(),
			Labels:           addPodLabel(datapoint.Attributes().AsRaw(), podName, podFound),
		})
	}
}

func (r *Receiver) storeCountAndSumDatapoints(name string, dataPoint HasCountSum, podName any, podFound bool) {
	labels := dataPoint.Attributes().AsRaw()
	r.p("     - time: %+v\n", dataPoint.Timestamp())
	r.p("       tags: %+v\n", labels)
	r.p("       count: %+v\n", dataPoint.Count())
	r.p("       sum: %+v\n", dataPoint.Sum())
	r.metricMemStore.Put(types.NewMetricEntry{
		Name:             types.MetricName(name + countSuffix),
		MeasurementValue: float64(dataPoint.Count()),
		MeasurementTime:  dataPoint.Timestamp(),
		Labels:           addPodLabel(labels, podName, podFound),
	})
	r.metricMemStore.Put(types.NewMetricEntry{
		Name:             types.MetricName(name + sumSuffix),
		MeasurementValue: dataPoint.Sum(),
		MeasurementTime:  dataPoint.Timestamp(),
		Labels:           addPodLabel(labels, podName, podFound),
	})
}

func GetStatusFromError(err error) error {
	s, ok := status.FromError(err)
	if !ok {
		// Default to a retryable error
		// https://github.com/open-telemetry/opentelemetry-proto/blob/main/docs/specification.md#failures
		code := codes.Unavailable
		if consumererror.IsPermanent(err) {
			// If an error is permanent but doesn't have an attached gRPC status, assume it is server-side.
			code = codes.Internal
		}
		s = status.New(code, err.Error())
	}
	return s.Err()
}
