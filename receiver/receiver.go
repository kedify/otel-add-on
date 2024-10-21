package receiver

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"sync"

	"go.opentelemetry.io/collector/consumer/consumererror"
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

// newOtlpReceiver just creates the OpenTelemetry receiver services. It is the caller's
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
	if r.serverGRPC, err = r.cfg.GRPC.ToServerWithOptions(context.Background(), host, r.settings.TelemetrySettings); err != nil {
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
		smLen := sm.Len()
		for j := 0; j < smLen; j++ {
			mLen := sm.At(j).Metrics().Len()
			metrics := sm.At(j).Metrics()
			for k := 0; k < mLen; k++ {
				r.p("-  name: %+v\n", metrics.At(k).Name())
				r.p("   type: %+v\n", metrics.At(k).Type())
				var dataPoints pmetric.NumberDataPointSlice
				switch metrics.At(k).Type() {
				case pmetric.MetricTypeGauge:
					dataPoints = metrics.At(k).Gauge().DataPoints()
				case pmetric.MetricTypeSum:
					dataPoints = metrics.At(k).Sum().DataPoints()
				default:
					// ignore others
				}
				for l := 0; l < dataPoints.Len(); l++ {
					datapoint := dataPoints.At(l)
					r.p("     - time: %+v\n", datapoint.Timestamp())
					r.p("       tags: %+v\n", datapoint.Attributes().AsRaw())
					value := math.Max(datapoint.DoubleValue(), float64(datapoint.IntValue()))
					r.p("       value: %+v\n", value)
					r.metricMemStore.Put(types.NewMetricEntry{
						Name:             types.MetricName(metrics.At(k).Name()),
						MeasurementValue: value,
						MeasurementTime:  datapoint.Timestamp(),
						Labels:           datapoint.Attributes().AsRaw(),
					})
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
