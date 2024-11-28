// Handlers contains the gRPC implementation for an external scaler as defined
// by the KEDA documentation at https://keda.sh/docs/2.0/concepts/external-scalers/#built-in-scalers-interface
// This is the interface KEDA will poll in order to get the metric value used for scaling
package scaler

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/go-logr/logr"
	"github.com/kedacore/keda/v2/pkg/scalers/externalscaler"

	"github.com/kedify/otel-add-on/metric"
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
	lggr            logr.Logger
	metricStore     types.MemStore
	metricParser    types.Parser
	metricsExporter *metric.InternalMetrics
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
		lggr:            lggr,
		metricStore:     metricStore,
		metricParser:    metricParser,
		metricsExporter: metric.Metrics(),
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
				MetricName: metricName,
				TargetSize: targetValue,
			},
		},
	}
	lggr.V(1).Info("got metric value: ", "GetMetricSpecResponse", res)
	return res, nil
}

func (e *impl) GetMetrics(
	ctx context.Context,
	metricRequest *externalscaler.GetMetricsRequest,
) (*externalscaler.GetMetricsResponse, error) {
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
	lggr.Info("got metric value: ", "name", metricName, "labels", labels, "value", value, "found", found, "error", err)
	if !found {
		return nil, fmt.Errorf("not found")
	}
	if err != nil {
		return nil, err
	}
	value = util.ClampValue(lggr, value, scalerMetadata)
	e.metricsExporter.SetMetricValueClamped(string(metricName), fmt.Sprint(labels), string(opOverTime), string(agg), sor.GetName(), sor.GetNamespace(), value)

	res := &externalscaler.GetMetricsResponse{
		MetricValues: []*externalscaler.MetricValue{
			{
				MetricName:  metricRequest.GetMetricName(),
				MetricValue: int64(math.Round(value)),
			},
		},
	}

	return res, nil
}
