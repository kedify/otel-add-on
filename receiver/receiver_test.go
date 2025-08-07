package receiver

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/config/configgrpc"
	"go.opentelemetry.io/collector/config/confignet"
	"go.opentelemetry.io/collector/config/configtls"
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
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"k8s.io/utils/ptr"

	"github.com/kedify/otel-add-on/metric"
)

const (
	otlpReceiverName = "receiver_test"
	addr             = "127.0.0.1:4317"

	// dataPointsPerMetricCount how many datapoints are created when testdata.GenerateMetrics method is called for one metric
	dataPointsPerMetricCount = 2

	// paths to certs and keys
	caCertFilePath     = "../certs/rootCA.crt"
	serverCertFilePath = "../certs/server.crt"
	serverKeyFilePath  = "../certs/server.key"
	clientCertFilePath = "../certs/client.crt"
	clientKeyFilePath  = "../certs/client.key"

	// internal collector metrics
	metricNameAcceptedPoints = "otelcol_receiver_accepted_metric_points"
	metricNameRefusedPoints  = "otelcol_receiver_refused_metric_points"
)

var (
	debugTests     = os.Getenv("DEBUG") == "true"
	otlpReceiverID = component.MustNewIDWithName("otlp", otlpReceiverName)
)

// both client and server has their own cert-and-key pair and common the caCert
func TestOTLPReceiverMutualTLSWithCA(t *testing.T) {
	tt := componenttest.NewTelemetry()
	t.Cleanup(func() { require.NoError(t, tt.Shutdown(context.Background())) })

	cfg := createDefaultConfig().(*otlpreceiver.Config)
	cfg.GRPC.NetAddr.Endpoint = addr
	cfg.GRPC.TLSSetting = &configtls.ServerConfig{
		Config: configtls.Config{
			CertFile: serverCertFilePath,
			KeyFile:  serverKeyFilePath,
		},
		ClientCAFile: caCertFilePath,
	}
	cfg.HTTP = nil
	sink := &testmc{
		consumeSuccessCount: ptr.To(0),
		consumeErrCount:     ptr.To(0),
	}
	recv := newReceiver(t, tt.NewTelemetrySettings(), cfg, otlpReceiverID, sink)
	require.NotNil(t, recv)
	require.NoError(t, recv.Start(context.Background(), componenttest.NewNopHost()))
	t.Cleanup(func() { require.NoError(t, recv.Shutdown(context.Background())) })
	clientCert, err := tls.LoadX509KeyPair(clientCertFilePath, clientKeyFilePath)
	require.NoError(t, err)
	caPool := x509.NewCertPool()
	caCert, err := os.ReadFile(caCertFilePath)
	require.NoError(t, err)
	caPool.AppendCertsFromPEM(caCert)
	tc := credentials.NewTLS(&tls.Config{
		InsecureSkipVerify: false,
		Certificates:       []tls.Certificate{clientCert},
		RootCAs:            caPool,
		ServerName:         "keda-otel-scaler.keda.svc",
	})

	cc, err := grpc.NewClient(addr, grpc.WithTransportCredentials(tc))
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, cc.Close())
	}()

	assertCanReceiveMetrics(t, sink, cc, tt)
}

// client has caCert, server has its cert and key
func TestOTLPReceiverTLSCaCertOnly(t *testing.T) {
	tt := componenttest.NewTelemetry()
	t.Cleanup(func() { require.NoError(t, tt.Shutdown(context.Background())) })

	cfg := createDefaultConfig().(*otlpreceiver.Config)
	cfg.GRPC.NetAddr.Endpoint = addr
	cfg.GRPC.TLSSetting = &configtls.ServerConfig{
		Config: configtls.Config{
			CertFile: serverCertFilePath,
			KeyFile:  serverKeyFilePath,
		},
	}
	cfg.HTTP = nil
	sink := &testmc{
		consumeSuccessCount: ptr.To(0),
		consumeErrCount:     ptr.To(0),
	}
	recv := newReceiver(t, tt.NewTelemetrySettings(), cfg, otlpReceiverID, sink)
	require.NotNil(t, recv)
	require.NoError(t, recv.Start(context.Background(), componenttest.NewNopHost()))
	t.Cleanup(func() { require.NoError(t, recv.Shutdown(context.Background())) })
	// keda-otel-scaler.keda.svc is one of the server cert's SANs (*.keda.svc)
	tc, err := credentials.NewClientTLSFromFile(caCertFilePath, "keda-otel-scaler.keda.svc")
	require.NoError(t, err)

	cc, err := grpc.NewClient(addr, grpc.WithTransportCredentials(tc))
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, cc.Close())
	}()

	assertCanReceiveMetrics(t, sink, cc, tt)
}

// both client and server has their own cert-and-key pair, but we don't check the certificate chain nor the hostname
func TestOTLPReceiverMutualTLSNoCA(t *testing.T) {
	tt := componenttest.NewTelemetry()
	t.Cleanup(func() { require.NoError(t, tt.Shutdown(context.Background())) })

	cfg := createDefaultConfig().(*otlpreceiver.Config)
	cfg.GRPC.NetAddr.Endpoint = addr
	cfg.GRPC.TLSSetting = &configtls.ServerConfig{
		Config: configtls.Config{
			CertFile: serverCertFilePath,
			KeyFile:  serverKeyFilePath,
		},
	}
	cfg.HTTP = nil
	sink := &testmc{
		consumeSuccessCount: ptr.To(0),
		consumeErrCount:     ptr.To(0),
	}
	recv := newReceiver(t, tt.NewTelemetrySettings(), cfg, otlpReceiverID, sink)
	require.NotNil(t, recv)
	require.NoError(t, recv.Start(context.Background(), componenttest.NewNopHost()))
	t.Cleanup(func() { require.NoError(t, recv.Shutdown(context.Background())) })
	clientCert, err := tls.LoadX509KeyPair(clientCertFilePath, clientKeyFilePath)
	require.NoError(t, err)
	tc := credentials.NewTLS(&tls.Config{
		// don't check the cert, but establish TLS
		// this is the insecure_skip_verify configuration option for exporter's tls so we want to support both methods
		InsecureSkipVerify: true,
		Certificates:       []tls.Certificate{clientCert},
	})

	cc, err := grpc.NewClient(addr, grpc.WithTransportCredentials(tc))
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, cc.Close())
	}()

	assertCanReceiveMetrics(t, sink, cc, tt)
}

func assertCanReceiveMetrics(t *testing.T, sink *testmc, cc *grpc.ClientConn, tt *componenttest.Telemetry) {
	metricsCount := 1
	md := testdata.GenerateMetrics(metricsCount)
	sink.SetConsumeError(nil)
	resp, err := pmetricotlp.NewGRPCClient(cc).Export(context.Background(), pmetricotlp.NewExportRequestFromMetrics(md))
	errStatus, ok := status.FromError(err)
	assert.True(t, ok)
	require.Equal(t, codes.OK, errStatus.Code(), errStatus.Message())
	require.Equal(t, resp.PartialSuccess().RejectedDataPoints(), int64(0), "There should be no rejected data points")
	require.NotNil(t, sink.consumeSuccessCount)
	require.Equal(t, *sink.consumeSuccessCount, 1, "one call should be successful")
	require.NotNil(t, sink.consumeErrCount)
	require.Equal(t, *sink.consumeErrCount, 0, "no call should be blocked")

	assertReceiverMetrics(t, tt, otlpReceiverID, "grpc", dataPointsPerMetricCount*metricsCount, 0)
}

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

	assertReceiverMetrics(t, tt, otlpReceiverID, "grpc", expectedReceivedBatches*metricsCount*dataPointsPerMetricCount, expectedIngestionBlockedRPCs*metricsCount*dataPointsPerMetricCount)
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
	memStore := metric.NewNoopMetricStore(debugTests)
	r, err := NewOtlpReceiver(cfg, &set, memStore, debugTests)
	require.NoError(t, err)
	r.RegisterMetricsConsumer(mc)
	return r
}

func assertReceiverMetrics(t *testing.T, tt *componenttest.Telemetry, id component.ID, transport string, accepted, refused int) {
	got, err := tt.GetMetric(metricNameAcceptedPoints)
	require.NoError(t, err)
	metricdatatest.AssertEqual(t,
		metricdata.Metrics{
			Name:        metricNameAcceptedPoints,
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
						Value: int64(accepted),
					},
				},
			},
		}, got, metricdatatest.IgnoreTimestamp(), metricdatatest.IgnoreExemplars())

	got, err = tt.GetMetric(metricNameRefusedPoints)
	require.NoError(t, err)
	metricdatatest.AssertEqual(t,
		metricdata.Metrics{
			Name:        metricNameRefusedPoints,
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
						Value: int64(refused),
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
