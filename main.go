// The OTEL Scaler is the standard implementation for a KEDA external scaler
// which can be found at https://keda.sh/docs/2.15/concepts/external-scalers/
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/go-logr/logr"
	"github.com/jkremser/otel-add-on/build"
	"github.com/jkremser/otel-add-on/metric"
	"github.com/jkremser/otel-add-on/receiver"
	"github.com/jkremser/otel-add-on/scaler"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/config/configgrpc"
	"go.opentelemetry.io/collector/config/confignet"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pmetric"
	rec "go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/receiver/otlpreceiver"
	"golang.org/x/sync/errgroup"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	clientset "github.com/kedacore/http-add-on/operator/generated/clientset/versioned"
	informers "github.com/kedacore/http-add-on/operator/generated/informers/externalversions"
	informershttpv1alpha1 "github.com/kedacore/http-add-on/operator/generated/informers/externalversions/http/v1alpha1"
	"github.com/kedacore/http-add-on/pkg/util"
)

var (
	setupLog = ctrl.Log.WithName("setup")
)

// +kubebuilder:rbac:groups="",resources=endpoints,verbs=get;list;watch
// +kubebuilder:rbac:groups=http.keda.sh,resources=httpscaledobjects,verbs=get;list;watch

func main() {
	// todo: get rid of http addon dependencies
	fmt.Printf("\n\n\nAhoj")
	cfg := scaler.MustParseConfig()
	otlpReceiverPort := cfg.OTLPReceiverPort
	//namespace := cfg.TargetNamespace
	//svcName := cfg.TargetService
	//deplName := cfg.TargetDeployment
	//targetPortStr := fmt.Sprintf("%d", cfg.TargetPort)
	targetPendingRequests := cfg.TargetPendingRequests

	opts := zap.Options{}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	k8sCfg, err := ctrl.GetConfig()
	if err != nil {
		setupLog.Error(err, "Kubernetes client config not found")
		os.Exit(1)
	}
	//k8sCl, err := kubernetes.NewForConfig(k8sCfg)
	//if err != nil {
	//	setupLog.Error(err, "creating new Kubernetes ClientSet")
	//	os.Exit(1)
	//}

	// create the endpoints informer
	//endpInformer := k8s.NewInformerBackedEndpointsCache(
	//	ctrl.Log,
	//	k8sCl,
	//	cfg.DeploymentCacheRsyncPeriod,
	//)

	httpCl, err := clientset.NewForConfig(k8sCfg)
	if err != nil {
		setupLog.Error(err, "creating new HTTP ClientSet")
		os.Exit(1)
	}
	sharedInformerFactory := informers.NewSharedInformerFactory(httpCl, cfg.ConfigMapCacheRsyncPeriod)
	httpsoInformer := informershttpv1alpha1.New(sharedInformerFactory, "", nil).HTTPScaledObjects()

	ctx := ctrl.SetupSignalHandler()
	ctx = util.ContextWithLogger(ctx, setupLog)

	eg, ctx := errgroup.WithContext(ctx)
	ms := metric.NewMetricStore(5)

	// start the endpoints informer
	//eg.Go(func() error {
	//	setupLog.Info("starting the endpoints informer")
	//	endpInformer.Start(ctx)
	//	return nil
	//})
	//
	//// start the httpso informer
	//eg.Go(func() error {
	//	setupLog.Info("starting the httpso informer")
	//	httpsoInformer.Informer().Run(ctx.Done())
	//	return nil
	//})

	//eg.Go(func() error {
	//	setupLog.Info("starting the queue pinger")
	//
	//	//if err := pinger.start(ctx, time.NewTicker(cfg.QueueTickDuration), endpInformer); !util.IsIgnoredErr(err) {
	//	//	setupLog.Error(err, "queue pinger failed")
	//	//	return err
	//	//}
	//
	//	return nil
	//})

	eg.Go(func() error {
		addr := fmt.Sprintf("0.0.0.0:%d", otlpReceiverPort)
		setupLog.Info("starting the grpc server for OTLP receiver", "address", addr)
		conf := &otlpreceiver.Config{
			Protocols: otlpreceiver.Protocols{
				GRPC: &configgrpc.ServerConfig{
					NetAddr: confignet.AddrConfig{
						Endpoint:  addr,
						Transport: confignet.TransportTypeTCP4,
					},
					//TLSSetting: &configtls.ServerConfig{},
					//TLSSetting: &configtls.ServerConfig{
					//	Config: configtls.Config{
					//		CAFile: "",
					//		CertFile: "",
					//		KeyFile: "",
					//	},
					//	ClientCAFile: "",
					//},
				},
			},
		}
		settings := &rec.Settings{
			ID:                component.MustNewIDWithName("bar", "foo"),
			BuildInfo:         component.NewDefaultBuildInfo(),
			TelemetrySettings: componenttest.NewNopTelemetrySettings(),
		}
		r, err := receiver.NewOtlpReceiver(conf, settings, ms)

		r.RegisterMetricsConsumer(mc{})

		r.Start(ctx, componenttest.NewNopHost())
		if err != nil {
			setupLog.Error(err, "otlp receiver failed to create")
			return err
		}

		setupLog.Info("starting the grpc server KEDA external push...")
		// todo: port cfg
		if err := startGrpcServer(ctx, cfg, ctrl.Log, otlpReceiverPort+1, httpsoInformer, int64(targetPendingRequests)); !util.IsIgnoredErr(err) {
			setupLog.Error(err, "grpc server failed")
			return err
		}

		return nil
	})

	build.PrintComponentInfo(ctrl.Log, "OTEL addon for KEDA")

	if err := eg.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		setupLog.Error(err, "fatal error")
		os.Exit(1)
	}

	setupLog.Info("Bye!")
}

func startGrpcServer(
	ctx context.Context,
	cfg *scaler.Config,
	lggr logr.Logger,
	port int,
	httpsoInformer informershttpv1alpha1.HTTPScaledObjectInformer,
	targetPendingRequests int64,
) error {
	addr := fmt.Sprintf("0.0.0.0:%d", port)
	lggr.Info("starting grpc server", "address", addr)

	lis, err := net.Listen("tcp", addr)
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
				// if we haven't updated the endpoints in twice QueueTickDuration we drop the check
				//if time.Now().After(pinger.lastPingTime.Add(cfg.QueueTickDuration * 2)) {
				//	hs.SetServingStatus("liveness", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
				//	hs.SetServingStatus("readiness", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
				//} else {
				//	// we propagate pinger status as scaler status
				//	hs.SetServingStatus("liveness", grpc_health_v1.HealthCheckResponse_ServingStatus(pinger.status))
				//	hs.SetServingStatus("readiness", grpc_health_v1.HealthCheckResponse_ServingStatus(pinger.status))
				//}
			}
		}
	}()

	grpc_health_v1.RegisterHealthServer(
		grpcServer,
		hs,
	)

	//externalscaler.RegisterExternalScalerServer(
	//	grpcServer,
	//	newImpl(
	//		lggr,
	//		pinger,
	//		httpsoInformer,
	//		targetPendingRequests,
	//	),
	//)

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

func (m mc) ConsumeMetrics(ctx context.Context, md pmetric.Metrics) error {
	//fmt.Printf("data points num: %d\n", md.ResourceMetrics().Len())
	//fmt.Printf("data points: %+v\n", md.ResourceMetrics())
	//fmt.Printf("internal: %+v\n", md.ResourceMetrics().At(0).Resource().Attributes().AsRaw())
	//fmt.Printf("internal: %+v\n", md.ResourceMetrics().At(0).Resource().Attributes().AsRaw())
	//for i := 0; i < md.ResourceMetrics().Len(); i++ {
	//	fmt.Printf("internal: %+v\n", md.ResourceMetrics().At(i).Resource().Attributes().AsRaw())
	//}
	return nil
}

var _ consumer.Metrics = new(mc)
