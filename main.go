// The OTel Scaler is the standard implementation for a KEDA external scaler
// which can be found at https://keda.sh/docs/2.15/concepts/external-scalers/
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/config/configgrpc"
	"go.opentelemetry.io/collector/config/confignet"
	"go.opentelemetry.io/collector/config/configtls"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pmetric"
	rec "go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/receiver/otlpreceiver"
	"golang.org/x/sync/errgroup"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/kedacore/keda/v2/pkg/scalers/externalscaler"

	"github.com/kedify/otel-add-on/build"
	"github.com/kedify/otel-add-on/metric"
	"github.com/kedify/otel-add-on/receiver"
	"github.com/kedify/otel-add-on/rest"
	"github.com/kedify/otel-add-on/scaler"
	"github.com/kedify/otel-add-on/types"
	"github.com/kedify/otel-add-on/util"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

var (
	setupLog = ctrl.Log.WithName("setup")
	isDebug  bool
)

func main() {
	cfg := util.MustParseConfig()
	otlpReceiverPort := cfg.OTLPReceiverPort
	restApiPort := cfg.RestApiPort

	lvl := util.SetupLog(cfg.NoColor)
	isDebug = util.IsDebug(lvl)
	if !cfg.NoBanner {
		util.PrintBanner(cfg.NoColor)
	}
	build.PrintComponentInfo(ctrl.Log, lvl, "OTel addon for KEDA")

	ctx := util.ContextWithLogger(ctrl.SetupSignalHandler(), setupLog)
	eg, ctx := errgroup.WithContext(ctx)
	ms := metric.NewMetricStore(cfg)
	mp := metric.NewParser()

	defer metric.Metrics().Unregister()
	eg.Go(func() error {
		var e error
		var info prometheus.Labels
		if info, e = startInternalMetricsServer(ctx, cfg); !util.IsIgnoredErr(e) {
			setupLog.Error(e, "metric server failed")
			return e
		}
		startRestServer(eg, restApiPort, info, ms)

		tlsSettings, tlsEnabled := makeTlsSettings(cfg)
		if e = startReceiver(ctx, otlpReceiverPort, tlsSettings, ms); !util.IsIgnoredErr(e) {
			setupLog.Error(e, "gRPC server failed (OTLP receiver)")
			return e
		}
		if tlsEnabled {
			setupLog.Info("TLS for gRPC server enabled (OTLP receiver)", "tlsSettings", tlsSettings)
		}

		if e = startGrpcServer(ctx, ctrl.Log, ms, mp, cfg); !util.IsIgnoredErr(e) {
			setupLog.Error(e, "gRPC server failed (KEDA external scaler)")
			return e
		}

		return nil
	})

	if err := eg.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		setupLog.Error(err, "fatal error")
		os.Exit(1)
	}

	setupLog.Info("Bye!")
}

func makeTlsSettings(cfg *util.Config) (*configtls.ServerConfig, bool) {
	if len(cfg.TLSCaFile) == 0 && (len(cfg.TLSCertFile) == 0 || len(cfg.TLSKeyFile) == 0) {
		return &configtls.ServerConfig{}, false
	}
	return &configtls.ServerConfig{
		Config: configtls.Config{
			CAFile:         cfg.TLSCaFile,
			CertFile:       cfg.TLSCertFile,
			KeyFile:        cfg.TLSKeyFile,
			ReloadInterval: cfg.CertReloadInterval,
		},
		ClientCAFile:       cfg.TLSCaFile,
		ReloadClientCAFile: true,
	}, true
}

func startRestServer(eg *errgroup.Group, restApiPort int, info prometheus.Labels, ms types.MemStore) {
	eg.Go(func() error {
		return rest.Init(restApiPort, info, ms, isDebug)
	})
}

func startInternalMetricsServer(ctx context.Context, cfg *util.Config) (prometheus.Labels, error) {
	addr := fmt.Sprintf("0.0.0.0:%d", cfg.InternalMetricsPort)
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Metrics: server.Options{
			BindAddress: addr,
		},
		//HealthProbeBindAddress:        probeAddr,
	})
	if err != nil {
		return nil, err
	}
	go func() {
		if err := mgr.Start(ctx); err != nil {
			setupLog.Error(err, "problem running manager")
			os.Exit(1)
		}
	}()
	m := metric.Metrics()
	m.Init()
	if err := m.Register(); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
	info := m.SetRuntimeInfo(cfg)
	return info, nil
}

func startReceiver(ctx context.Context, otlpReceiverPort int, tlsSettings *configtls.ServerConfig, ms types.MemStore) error {
	addr := fmt.Sprintf("0.0.0.0:%d", otlpReceiverPort)
	setupLog.Info("starting the gRPC server for OTLP receiver", "address", addr)
	conf := &otlpreceiver.Config{
		Protocols: otlpreceiver.Protocols{
			GRPC: &configgrpc.ServerConfig{
				NetAddr: confignet.AddrConfig{
					Endpoint:  addr,
					Transport: confignet.TransportTypeTCP4,
				},
				TLSSetting:      tlsSettings,
				IncludeMetadata: true,
			},
		},
	}
	settings := &rec.Settings{
		ID:                component.MustNewIDWithName("id", "otlp-receiver"),
		BuildInfo:         component.NewDefaultBuildInfo(),
		TelemetrySettings: componenttest.NewNopTelemetrySettings(),
	}
	r, e := receiver.NewOtlpReceiver(conf, settings, ms, isDebug)
	if e != nil {
		setupLog.Error(e, "failed to create new OTLP receiver")
		return e
	}
	r.RegisterMetricsConsumer(mc{})

	e = r.Start(ctx, componenttest.NewNopHost())
	if e != nil {
		setupLog.Error(e, "OTLP receiver failed to start")
		return e
	}
	return nil
}

func startGrpcServer(
	ctx context.Context,
	lggr logr.Logger,
	ms types.MemStore,
	mp types.Parser,
	cfg *util.Config,
) error {
	kedaExternalScalerAddr := fmt.Sprintf("0.0.0.0:%d", cfg.KedaExternalScalerPort)
	setupLog.Info("starting the gRPC server for KEDA scaler", "address", kedaExternalScalerAddr)
	lis, err := net.Listen("tcp", kedaExternalScalerAddr)
	if err != nil {
		return err
	}

	grpcServer := grpc.NewServer()
	reflection.Register(grpcServer)

	hs := health.NewServer()
	go func() {
		lggr.Info("starting healthchecks loop")
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			// handle cancellations/timeout
			case <-ctx.Done():
				hs.SetServingStatus("liveness", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
				hs.SetServingStatus("readiness", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
				return
			// do our regularly scheduled work
			case <-ticker.C:
				hs.SetServingStatus("liveness", grpc_health_v1.HealthCheckResponse_SERVING)
				hs.SetServingStatus("readiness", grpc_health_v1.HealthCheckResponse_SERVING)
			}
		}
	}()

	grpc_health_v1.RegisterHealthServer(
		grpcServer,
		hs,
	)

	externalscaler.RegisterExternalScalerServer(
		grpcServer,
		scaler.New(
			lggr,
			ms,
			mp,
			cfg,
		),
	)

	go func() {
		<-ctx.Done()
		grpcServer.GracefulStop()
	}()

	return grpcServer.Serve(lis)
}

type mc struct{}

func (m mc) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: false}
}

func (m mc) ConsumeMetrics(_ context.Context, _ pmetric.Metrics) error {
	// noop, incoming metrics are processed in receiver.Export
	return nil
}

var _ consumer.Metrics = new(mc)
