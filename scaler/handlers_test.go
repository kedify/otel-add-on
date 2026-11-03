package scaler

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/kedacore/keda/v2/pkg/scalers/externalscaler"
	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/kedify/otel-add-on/metric"
	"github.com/kedify/otel-add-on/types"
	"github.com/kedify/otel-add-on/util"
)

func TestGetMetricsLabelMatchers(t *testing.T) {
	cfg := &util.Config{MetricStoreRetentionSeconds: 60, MetricStoreValueIfNotFound: -99, MetricStoreErrIfNotFound: true}
	store := metric.NewMetricStore(cfg)
	for _, series := range []struct {
		labels types.Labels
		value  float64
	}{
		{types.Labels{"queue": "orders-us", "environment": "prod"}, 10},
		{types.Labels{"queue": "orders-eu", "environment": "staging"}, 20},
		{types.Labels{"queue": "orders-unknown"}, 5},
		{types.Labels{"queue": "orders-dev", "environment": "dev"}, 100},
		{types.Labels{"queue": "payments", "environment": "prod"}, 100},
	} {
		store.Put(types.NewMetricEntry{
			Name: "queue_depth", Labels: series.labels, MeasurementValue: series.value,
			MeasurementTime: pcommon.Timestamp(time.Now().Unix()),
		})
	}
	scaler := New(logr.Discard(), store, metric.NewParser(), cfg)
	for _, tt := range []struct {
		query string
		value float64
		err   string
	}{
		{`queue_depth{queue=~"orders-.*",environment!="dev"}`, 35, ""},
		{`count(queue_depth{queue=~"orders-.*",queue!~".*-dev"})`, 3, ""},
		{`queue_depth{queue=orders-us}`, 10, ""},
		{`queue_depth{queue="missing"}`, 0, "not found"},
		{`queue_depth{queue=~"["}`, 0, "invalid regex"},
		{`queue_depth{queue=="orders"}`, 0, "invalid unquoted value"},
	} {
		t.Run(tt.query, func(t *testing.T) {
			response, err := scaler.GetMetrics(context.Background(), &externalscaler.GetMetricsRequest{
				MetricName: "queue-scaling",
				ScaledObjectRef: &externalscaler.ScaledObjectRef{
					Name: "queue-worker", Namespace: "default",
					ScalerMetadata: map[string]string{util.MetadataMetricQuery: tt.query},
				},
			})
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("expected error containing %q, got %v", tt.err, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(response.MetricValues) != 1 || response.MetricValues[0].MetricName != "queue-scaling" || response.MetricValues[0].MetricValueFloat != tt.value {
				t.Fatalf("unexpected response: %v", response)
			}
		})
	}
}
