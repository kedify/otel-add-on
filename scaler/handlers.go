// Handlers contains the gRPC implementation for an external scaler as defined
// by the KEDA documentation at https://keda.sh/docs/2.0/concepts/external-scalers/#built-in-scalers-interface
// This is the interface KEDA will poll in order to get the metric value used for scaling
package scaler

import (
	"context"
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
	externalscaler.UnimplementedExternalScalerServer
	cfg *util.Config
}

func New(
	lggr logr.Logger,
	metricStore types.MemStore,
	metricParser types.Parser,
	cfg *util.Config,
) *impl {
	return &impl{
		lggr:            lggr,
		metricStore:     metricStore,
		metricParser:    metricParser,
		metricsExporter: metric.Metrics(),
		cfg:             cfg,
	}
}

func (e *impl) Ping(context.Context, *emptypb.Empty) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

func (e *impl) IsActive(
	_ context.Context,
	sor *externalscaler.ScaledObjectRef,
) (*externalscaler.IsActiveResponse, error) {
	lggr := e.lggr.WithName("IsActive")

	value, err := e.getMetric(sor)
	if err != nil {
		lggr.Error(err, "getMetric failed", "scaledObjectRef", sor.String())
		return nil, err
	}

	active := value > 0
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
	query, err := util.GetMetricQuery(lggr, sor.GetScalerMetadata(), e.metricParser)
	if err != nil {
		return nil, err
	}
	if e.cfg.MetricStoreLazySeries || e.cfg.MetricStoreLazyAggregates {
		opOverTime := util.GetOperationOverTime(lggr, sor.GetScalerMetadata())
		for _, name := range query.MetricNames() {
			if _, err := e.metricStore.GetSeries(name, nil, opOverTime); err != nil {
				return nil, fmt.Errorf("unable to initialize metric %s in lazy mode: %w", name, err)
			}
		}
	}

	namespacedName := util.NamespacedNameFromScaledObjectRef(sor)
	kedaMetricName := fmt.Sprintf("%s-%s", namespacedName.Namespace, namespacedName.Name)

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
				MetricName:      kedaMetricName,
				TargetSizeFloat: targetValue,
				// when both are sent, the TargetSizeFloat has precedence (sending the old one for backward compat)
				TargetSize: int64(math.Ceil(targetValue)),
			},
		},
	}
	lggr.V(1).Info("got metric value: ", "GetMetricSpecResponse", res)
	return res, nil
}

func (e *impl) GetMetrics(
	_ context.Context,
	metricRequest *externalscaler.GetMetricsRequest,
) (*externalscaler.GetMetricsResponse, error) {
	sor := metricRequest.ScaledObjectRef
	value, err := e.getMetric(sor)
	if err != nil {
		return nil, err
	}

	res := &externalscaler.GetMetricsResponse{
		MetricValues: []*externalscaler.MetricValue{
			{
				MetricName:       metricRequest.GetMetricName(),
				MetricValueFloat: value,
				// when both are sent, the MetricValueFloat has precedence (sending the old one for backward compat)
				MetricValue: int64(math.Ceil(value)),
			},
		},
	}

	return res, nil
}

func (e *impl) getMetric(sor *externalscaler.ScaledObjectRef) (float64, error) {
	lggr := e.lggr.WithName("getMetric")
	query, err := util.GetMetricQuery(lggr, sor.GetScalerMetadata(), e.metricParser)
	if err != nil {
		return e.cfg.MetricStoreValueIfNotFound, err
	}
	opOverTime := util.GetOperationOverTime(lggr, sor.GetScalerMetadata())
	value, found, err := query.Evaluate(e.metricStore, opOverTime)
	lggr.Info("got metric value: ", "query", sor.GetScalerMetadata()[util.MetadataMetricQuery], "value", value, "found", found, "error", err)
	if err != nil {
		return e.cfg.MetricStoreValueIfNotFound, err
	}
	if !found {
		if e.cfg.MetricStoreErrIfNotFound {
			return e.cfg.MetricStoreValueIfNotFound, fmt.Errorf("not found")
		}
		return e.cfg.MetricStoreValueIfNotFound, nil
	}
	e.metricsExporter.SetMetricValue(fmt.Sprint(query.MetricNames()), sor.GetScalerMetadata()[util.MetadataMetricQuery], string(opOverTime), "expression", value)
	value = util.ClampValue(lggr, value, sor.GetScalerMetadata())
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return e.cfg.MetricStoreValueIfNotFound, fmt.Errorf("metric value after metadata clamps is non-finite: %v", value)
	}
	e.metricsExporter.SetMetricValueClamped(fmt.Sprint(query.MetricNames()), sor.GetScalerMetadata()[util.MetadataMetricQuery], string(opOverTime), "expression", sor.GetName(), sor.GetNamespace(), value)
	return value, nil
}
