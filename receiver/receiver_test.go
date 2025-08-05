package receiver

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/config/configgrpc"
	"go.opentelemetry.io/collector/config/confignet"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/consumer/consumererror"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/testdata"
	"go.opentelemetry.io/collector/receiver/otlpreceiver"
	"go.opentelemetry.io/collector/receiver/receivertest"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"k8s.io/utils/ptr"

	"github.com/kedify/otel-add-on/metric"
)

const (
	otlpReceiverName = "receiver_test"
	addr             = "127.0.0.1:4317"
)

var otlpReceiverID = component.MustNewIDWithName("otlp", otlpReceiverName)

func TestOTLPReceiverGRPCMetricsIngest(t *testing.T) {
	type ingestionStateTest struct {
		okToIngest   bool
		permanent    bool
		expectedCode codes.Code
	}

	ingestionStates := []ingestionStateTest{
		{
			okToIngest:   true,
			expectedCode: codes.OK,
		},
		{
			okToIngest:   false,
			expectedCode: codes.Unavailable,
		},
		{
			okToIngest:   false,
			expectedCode: codes.Internal,
			permanent:    true,
		},
		{
			okToIngest:   true,
			expectedCode: codes.OK,
		},
		{
			okToIngest:   true,
			expectedCode: codes.OK,
		},
	}
	expectedReceivedBatches := 0
	for _, ist := range ingestionStates {
		if ist.okToIngest {
			expectedReceivedBatches++
		}
	}
	expectedIngestionBlockedRPCs := len(ingestionStates) - expectedReceivedBatches

	// two random metric types, each having 2 measurements so 4 metric datapoints in total
	dataPointsPerMetricCount := 2
	metricsCount := 2
	md := testdata.GenerateMetrics(metricsCount)
	protoMarshaler := &pmetric.ProtoMarshaler{}
	_, err := protoMarshaler.MarshalMetrics(md)
	require.NoError(t, err)

	tt := componenttest.NewTelemetry()
	t.Cleanup(func() { require.NoError(t, tt.Shutdown(context.Background())) })

	cfg := createDefaultConfig().(*otlpreceiver.Config)
	cfg.GRPC.NetAddr.Endpoint = addr
	cfg.HTTP = nil
	sink := testmc{
		consumeSuccessCount: ptr.To(0),
		consumeErrCount:     ptr.To(0),
	}
	recv := newReceiver(t, tt.NewTelemetrySettings(), cfg, otlpReceiverID, &sink)
	require.NotNil(t, recv)
	require.NoError(t, recv.Start(context.Background(), componenttest.NewNopHost()))
	t.Cleanup(func() { require.NoError(t, recv.Shutdown(context.Background())) })

	cc, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, cc.Close())
	}()

	for _, ingestionState := range ingestionStates {
		if ingestionState.okToIngest {
			sink.SetConsumeError(nil)
		} else {
			if ingestionState.permanent {
				sink.SetConsumeError(consumererror.NewPermanent(errors.New("consumer error")))
			} else {
				sink.SetConsumeError(errors.New("consumer error"))
			}
		}
		_, err = pmetricotlp.NewGRPCClient(cc).Export(context.Background(), pmetricotlp.NewExportRequestFromMetrics(md))
		errStatus, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, ingestionState.expectedCode, errStatus.Code())
	}

	require.NotNil(t, sink.consumeSuccessCount)
	require.Equal(t, *sink.consumeSuccessCount, expectedReceivedBatches, "two calls should be successful")
	require.NotNil(t, sink.consumeErrCount)
	require.Equal(t, *sink.consumeErrCount, expectedIngestionBlockedRPCs, "two calls should be blocked")

	assertReceiverMetrics(t, tt, otlpReceiverID, "grpc", int64(expectedReceivedBatches*metricsCount*dataPointsPerMetricCount), int64(expectedIngestionBlockedRPCs*metricsCount*2))
}

func createDefaultConfig() component.Config {
	grpcCfg := configgrpc.NewDefaultServerConfig()
	grpcCfg.NetAddr = confignet.NewDefaultAddrConfig()
	grpcCfg.NetAddr.Transport = confignet.TransportTypeTCP
	grpcCfg.ReadBufferSize = 512 * 1024

	return &otlpreceiver.Config{
		Protocols: otlpreceiver.Protocols{
			GRPC: grpcCfg,
		},
	}
}

func newReceiver(t *testing.T, settings component.TelemetrySettings, cfg *otlpreceiver.Config, id component.ID, mc consumer.Metrics) component.Component {
	set := receivertest.NewNopSettings(component.MustNewType("otlp"))
	set.TelemetrySettings = settings
	set.ID = id
	memStore := metric.NewNoopMetricStore()
	r, err := NewOtlpReceiver(cfg, &set, memStore, true)
	require.NoError(t, err)
	r.RegisterMetricsConsumer(mc)
	return r
}

func assertReceiverMetrics(t *testing.T, tt *componenttest.Telemetry, id component.ID, transport string, accepted, refused int64) {
	got, err := tt.GetMetric("otelcol_receiver_accepted_metric_points")
	require.NoError(t, err)
	metricdatatest.AssertEqual(t,
		metricdata.Metrics{
			Name:        "otelcol_receiver_accepted_metric_points",
			Description: "Number of metric points successfully pushed into the pipeline. [alpha]",
			Unit:        "{datapoints}",
			Data: metricdata.Sum[int64]{
				Temporality: metricdata.CumulativeTemporality,
				IsMonotonic: true,
				DataPoints: []metricdata.DataPoint[int64]{
					{
						Attributes: attribute.NewSet(
							attribute.String("receiver", id.String()),
							attribute.String("transport", transport)),
						Value: accepted,
					},
				},
			},
		}, got, metricdatatest.IgnoreTimestamp(), metricdatatest.IgnoreExemplars())

	got, err = tt.GetMetric("otelcol_receiver_refused_metric_points")
	require.NoError(t, err)
	metricdatatest.AssertEqual(t,
		metricdata.Metrics{
			Name:        "otelcol_receiver_refused_metric_points",
			Description: "Number of metric points that could not be pushed into the pipeline. [alpha]",
			Unit:        "{datapoints}",
			Data: metricdata.Sum[int64]{
				Temporality: metricdata.CumulativeTemporality,
				IsMonotonic: true,
				DataPoints: []metricdata.DataPoint[int64]{
					{
						Attributes: attribute.NewSet(
							attribute.String("receiver", id.String()),
							attribute.String("transport", transport)),
						Value: refused,
					},
				},
			},
		}, got, metricdatatest.IgnoreTimestamp(), metricdatatest.IgnoreExemplars())
}

type testmc struct {
	mu                  sync.Mutex
	consumeError        error // to be returned by ConsumeMetrics, if set
	consumeSuccessCount *int
	consumeErrCount     *int
}

func (m *testmc) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: false}
}

func (m *testmc) SetConsumeError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.consumeError = err
}

func (m *testmc) ConsumeMetrics(_ context.Context, _ pmetric.Metrics) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// simulating error here
	if m.consumeError != nil {
		m.consumeErrCount = ptr.To(*m.consumeErrCount + 1)
		return m.consumeError
	}

	m.consumeSuccessCount = ptr.To(*m.consumeSuccessCount + 1)
	// noop, we process the metrics in the Export method
	return nil
}

var _ consumer.Metrics = new(testmc)
