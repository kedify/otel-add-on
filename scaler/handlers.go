// Handlers contains the gRPC implementation for an external scaler as defined
// by the KEDA documentation at https://keda.sh/docs/2.0/concepts/external-scalers/#built-in-scalers-interface
// This is the interface KEDA will poll in order to get the metric value used for scaling
package scaler

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	"github.com/kedacore/keda/v2/pkg/scalers/externalscaler"

	"github.com/kedify/otel-add-on/types"
	"github.com/kedify/otel-add-on/util"

	"google.golang.org/protobuf/types/known/emptypb"
)

var streamInterval time.Duration

func init() {
	defaultMS := 500
	timeoutMS, err := util.ResolveOsEnvInt("IS_ACTIVE_POLLING_INTERVAL_MS", defaultMS)
	if err != nil {
		timeoutMS = defaultMS
	}
	streamInterval = time.Duration(timeoutMS) * time.Millisecond
}

type impl struct {
	lggr         logr.Logger
	metricStore  types.MemStore
	metricParser types.Parser
	//soInformer   informersv1alpha1.ScaledObjectInformer
	//targetMetric int64
	externalscaler.UnimplementedExternalScalerServer
}

func New(
	lggr logr.Logger,
	metricStore types.MemStore,
	metricParser types.Parser,
) *impl {
	return &impl{
		lggr:         lggr,
		metricStore:  metricStore,
		metricParser: metricParser,
		//soInformer:   soInformer,
		//targetMetric: defaultTargetMetric,
	}
}

func (e *impl) Ping(context.Context, *emptypb.Empty) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

func (e *impl) IsActive(
	ctx context.Context,
	sor *externalscaler.ScaledObjectRef,
) (*externalscaler.IsActiveResponse, error) {
	lggr := e.lggr.WithName("IsActive")

	gmr, err := e.GetMetrics(ctx, &externalscaler.GetMetricsRequest{
		ScaledObjectRef: sor,
	})
	if err != nil {
		lggr.Error(err, "GetMetrics failed", "scaledObjectRef", sor.String())
		return nil, err
	}

	metricValues := gmr.GetMetricValues()
	if err := errors.New("len(metricValues) != 1"); len(metricValues) != 1 {
		lggr.Error(err, "invalid GetMetricsResponse", "scaledObjectRef", sor.String(), "getMetricsResponse", gmr.String())
		return nil, err
	}
	metricValue := metricValues[0].GetMetricValue()
	active := metricValue > 0

	return &externalscaler.IsActiveResponse{Result: active}, nil
}

func (e *impl) StreamIsActive(
	scaledObject *externalscaler.ScaledObjectRef,
	server externalscaler.ExternalScaler_StreamIsActiveServer,
) error {
	// this function communicates with KEDA via the 'server' parameter.
	// we call server.Send (below) every streamInterval, which tells it to immediately
	// ping our IsActive RPC
	ticker := time.NewTicker(streamInterval)
	defer ticker.Stop()
	for {
		select {
		case <-server.Context().Done():
			return nil
		case <-ticker.C:
			active, err := e.IsActive(server.Context(), scaledObject)
			if err != nil {
				e.lggr.Error(
					err,
					"error getting active status in stream",
				)
				return err
			}
			err = server.Send(&externalscaler.IsActiveResponse{
				Result: active.Result,
			})
			if err != nil {
				e.lggr.Error(
					err,
					"error sending the active result in stream",
				)
				return err
			}
		}
	}
}

func (e *impl) GetMetricSpec(
	_ context.Context,
	sor *externalscaler.ScaledObjectRef,
) (*externalscaler.GetMetricSpecResponse, error) {
	lggr := e.lggr.WithName("GetMetricSpec")

	namespacedName := util.NamespacedNameFromScaledObjectRef(sor)
	metricName := fmt.Sprintf("%s-%s", namespacedName.Namespace, namespacedName.Name)

	scalerMetadata := sor.GetScalerMetadata()
	if scalerMetadata == nil {
		lggr.Info("unable to get SO metadata", "name", sor.Name, "namespace", sor.Namespace)
		return nil, fmt.Errorf("GetMetricSpec")
	}
	targetValue, err := util.GetTargetValue(scalerMetadata)
	if err != nil {
		lggr.Error(err, "unable to get target value from SO metadata", "name", sor.Name, "namespace", sor.Namespace)
		return nil, err
	}

	res := &externalscaler.GetMetricSpecResponse{
		MetricSpecs: []*externalscaler.MetricSpec{
			{
				MetricName: string(metricName),
				TargetSize: targetValue,
			},
		},
	}
	fmt.Printf("GetMetricSpec: %v", res)
	fmt.Printf("GetMetricSpec: name: %v target: %v", string(metricName), targetValue)
	return res, nil
}

func (e *impl) interceptorMetricSpec(metricName string, interceptorTargetPendingRequests string) (*externalscaler.GetMetricSpecResponse, error) {
	lggr := e.lggr.WithName("interceptorMetricSpec")

	targetPendingRequests, err := strconv.ParseInt(interceptorTargetPendingRequests, 10, 64)
	if err != nil {
		lggr.Error(err, "unable to parse interceptorTargetPendingRequests", "value", interceptorTargetPendingRequests)
		return nil, err
	}

	res := &externalscaler.GetMetricSpecResponse{
		MetricSpecs: []*externalscaler.MetricSpec{
			{
				MetricName: metricName,
				TargetSize: targetPendingRequests,
			},
		},
	}
	return res, nil
}

func (e *impl) GetMetrics(
	ctx context.Context,
	metricRequest *externalscaler.GetMetricsRequest,
) (*externalscaler.GetMetricsResponse, error) {
	fmt.Println("called GetMetrics")
	lggr := e.lggr.WithName("GetMetrics")
	sor := metricRequest.ScaledObjectRef

	//namespacedName := k8s.NamespacedNameFromScaledObjectRef(sor)
	//metricName := namespacedName.Name

	scalerMetadata := sor.GetScalerMetadata()
	metricName, labels, agg, err := util.GetMetricQuery(lggr, scalerMetadata, e.metricParser)
	if err != nil {
		return nil, err
	}

	opOverTime := util.GetOperationOvertTime(lggr, scalerMetadata)
	value, found, err := e.metricStore.Get(metricName, labels, opOverTime, agg)
	//lggr.V(1).Info("got metric value: ", "value", value, "found", found, "error", err)
	lggr.Info("got metric value: ", "name", metricName, "labels", labels, "value", value, "found", found, "error", err)
	value = util.ClampValue(lggr, value, scalerMetadata)

	res := &externalscaler.GetMetricsResponse{
		MetricValues: []*externalscaler.MetricValue{
			{
				MetricName:  string(metricName),
				MetricValue: int64(math.Round(value)),
			},
		},
	}
	//fmt.Printf("GetMetrics: %v", res)
	//fmt.Printf("GetMetrics: name: %v target: %v", string(metricName), targetValue)
	return res, nil
}

//// forwardIsActive checks the min replicas on HSO and if desired, forwards the IsActive check from KEDA to interceptors to update their envoy xDS snapshot cache
//func (e *impl) forwardIsActive(ctx context.Context, sor *externalscaler.ScaledObjectRef, active bool) error {
//	sorKey := sor.Namespace + "/" + sor.Name
//	if active {
//		// forwarding activation is asynchronous to not slow down cold starts, it's fine if few more early requests
//		// are sent through the interceptor during activation as long as envoy later routes the heavy traffic directly
//		go func() {
//			// if this fails, it's ok because interceptors will eventually figure this out from target's endpoints
//			// so scaler only logs any errors during activation forwarding
//			err := e.checkAndForwardActivation(context.Background(), sor, active)
//			if err != nil {
//				e.lggr.Error(err, "IsActive forward to interceptors failed", "scaledObjectRef", sorKey, "active", active)
//				return
//			}
//		}()
//		return nil
//	} else {
//		// forwarding deactivation is synchronous and response to KEDA must wait before this completes because scale to 0
//		// and deactivation should not happen before envoy routes requests through the interceptor
//		err := e.checkAndForwardActivation(ctx, sor, active)
//		if err != nil {
//			e.lggr.Error(err, "IsActive forward to interceptors failed", "scaledObjectRef", sorKey, "active", active)
//			return err
//		}
//	}
//	return nil
//}

//// checkAndForwardActivation checks if IsActive value should be forwarded to interceptors based on existing HSO configuration
//func (e *impl) checkAndForwardActivation(ctx context.Context, sor *externalscaler.ScaledObjectRef, active bool) error {
//	httpso, err := e.httpsoInformer.Lister().HTTPScaledObjects(sor.Namespace).Get(sor.Name)
//	if sor.Name == e.pinger.interceptorServiceName {
//		return nil
//	}
//	if kerrors.IsNotFound(err) {
//		e.lggr.V(4).Info("IsActive forward to interceptors skipped because HTTPScaledObject not found", "namespace", sor.Namespace, "name", sor.Name)
//		return nil
//	}
//	if err != nil {
//		return err
//	}
//	if httpso.Spec.Replicas != nil && httpso.Spec.Replicas.Min != nil && *httpso.Spec.Replicas.Min > 0 {
//		// if min replicas is set to larger than 0, interceptor doesn't need to worry about cold starts
//		e.lggr.V(4).Info("IsActive forward to interceptors skipped because min replicas is set", "namespace", sor.Namespace, "name", sor.Name, "value", *httpso.Spec.Replicas.Min)
//		return nil
//	}
//	return e.pinger.forwardIsActive(ctx, sor, active)
//}

//// interceptorsHealthy returns true if 'keda-add-ons-http-interceptor-proxy' service has healthy endpoints and if
//// HTTPScaledObject is not marked with 'http.kedify.io/traffic-autowire=false' then also 'kedify-proxy' needs to have
//// at least one healthy endpoint address
//func (e *impl) interceptorsHealthy(ctx context.Context, hso *httpv1alpha1.HTTPScaledObject) bool {
//	lggr := e.lggr.WithName("checkInterceptors")
//	toCheck := [][]string{{e.pinger.interceptorServiceName + "-proxy", e.pinger.interceptorNS}}
//	if val, found := hso.GetObjectMeta().GetAnnotations()[kedifyAutowireAnnotation]; !found || val != "false" {
//		toCheck = append(toCheck, []string{kedifyProxySvcName, hso.Namespace})
//	}
//	for _, svc := range toCheck {
//		endpoints, err := e.pinger.getEndpointsFn(ctx, svc[1], svc[0])
//		if err != nil {
//			lggr.Error(err, fmt.Sprintf("can't get endpoints for %s/%s", svc[1], svc[0]))
//			return false
//		}
//		if len(endpoints.Subsets) == 0 || len(endpoints.Subsets[0].Addresses) == 0 {
//			lggr.V(2).Info(fmt.Sprintf("no endpoints for %s/%s", svc[1], svc[0]))
//			return false
//		}
//	}
//	return true
//}
