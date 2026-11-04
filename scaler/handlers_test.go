package scaler

import (
	"context"
	"fmt"
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
		{"max(\nclamp_min(queue_depth / 2, 1)\n)", 50, ""},
		{`ln(queue_depth{queue="orders-us"} / 10)`, 0, ""},
		{"1 + 2 * 3", 7, ""},
		{`scalar(sum(queue_depth{queue=~"orders-us|orders-eu"})) - queue_depth{queue="orders-us"}`, 20, ""},
		{`sum(queue_depth{queue="orders-us"}) - (sum(queue_depth{queue="not-created"}) or vector(0))`, 10, ""},
		{`sum(queue_depth{queue="orders-us"}) - (sum(queue_depth{queue="orders-eu"}) or vector(0))`, -10, ""},
		{"missing or vector(0)", 0, ""},
		{"vector(queue_depth)", 0, "must be a scalar"},
		{"queue_depth or 0", 0, "operands must be vectors"},
		{"scalar(queue_depth)", 0, "non-finite"},
		{"scalar(missing)", 0, "non-finite"},
		{"scalar(1)", 0, "must be a vector"},
		{"1 / 0", 0, "non-finite"},
		{"ln(-queue_depth)", 0, "non-finite"},
		{"missing + 10", 0, "not found"},
		{"sum(1)", 0, "must be a vector"},
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

func TestGetMetricsErrorsWhenAllMatchingSeriesAreStale(t *testing.T) {
	cfg := &util.Config{MetricStoreRetentionSeconds: 60, MetricStoreValueIfNotFound: -99, MetricStoreErrIfNotFound: true}
	store := metric.NewMetricStore(cfg)
	now := time.Now().Unix()
	store.Put(types.NewMetricEntry{
		Name: "queue_depth", Labels: types.Labels{"queue": "orders"}, MeasurementValue: 10,
		MeasurementTime: pcommon.Timestamp(now - 120),
	})
	store.Put(types.NewMetricEntry{
		Name: "queue_depth", Labels: types.Labels{"queue": "payments"}, MeasurementValue: 20,
		MeasurementTime: pcommon.Timestamp(now),
	})
	scaler := New(logr.Discard(), store, metric.NewParser(), cfg)

	// A stale-only selector must error so KEDA's fallback applies, instead of reporting 0 and scaling in.
	for _, query := range []string{`queue_depth{queue="orders"}`, `count(queue_depth{queue="orders"})`} {
		t.Run(query, func(t *testing.T) {
			_, err := scaler.GetMetrics(context.Background(), &externalscaler.GetMetricsRequest{
				MetricName: "queue-scaling",
				ScaledObjectRef: &externalscaler.ScaledObjectRef{
					Name: "queue-worker", Namespace: "default",
					ScalerMetadata: map[string]string{util.MetadataMetricQuery: query},
				},
			})
			if err == nil || !strings.Contains(err.Error(), "not found") {
				t.Fatalf("expected error containing %q, got %v", "not found", err)
			}
		})
	}
}

func TestGetMetricSpecInitializesAllReferences(t *testing.T) {
	for _, lazySeries := range []bool{false, true} {
		for _, lazyAggregates := range []bool{false, true} {
			t.Run(fmt.Sprintf("series=%t/aggregates=%t", lazySeries, lazyAggregates), func(t *testing.T) {
				cfg := &util.Config{
					MetricStoreRetentionSeconds: 60, MetricStoreLazySeries: lazySeries, MetricStoreLazyAggregates: lazyAggregates,
				}
				store := metric.NewMetricStore(cfg)
				scaler := New(logr.Discard(), store, metric.NewParser(), cfg)
				ref := &externalscaler.ScaledObjectRef{
					Name: "workers", Namespace: "default",
					ScalerMetadata: map[string]string{
						util.MetadataMetricQuery: "max(\nfirst + first + second\n)",
						util.MetadataTargetValue: "5", util.MetadataOperationOverTime: "min",
					},
				}
				spec, err := scaler.GetMetricSpec(context.Background(), ref)
				if err != nil || len(spec.MetricSpecs) != 1 || spec.MetricSpecs[0].TargetSizeFloat != 5 {
					t.Fatalf("unexpected spec: %v, error: %v", spec, err)
				}
				for _, name := range []types.MetricName{"first", "second"} {
					if (lazySeries || lazyAggregates) && !store.IsSubscribed(lazyAggregates, name, types.OpMin) {
						t.Fatalf("GetMetricSpec did not subscribe %s", name)
					}
					for i := 0; i < 2; i++ {
						store.Put(types.NewMetricEntry{Name: name, MeasurementValue: 2, MeasurementTime: pcommon.Timestamp(time.Now().Unix())})
					}
				}
				value, err := scaler.getMetric(ref)
				if err != nil || value != 6 {
					t.Fatalf("want 6 after initialization and ingestion, got %v, %v", value, err)
				}
			})
		}
	}

}

func TestGetMetricSpecRejectsMalformedQueries(t *testing.T) {
	cfg := &util.Config{MetricStoreRetentionSeconds: 60}
	scaler := New(logr.Discard(), metric.NewMetricStore(cfg), metric.NewParser(), cfg)
	for _, query := range []string{"", "\n  ", "ln(1)", "unknown(input)", "input[5m]"} {
		_, err := scaler.GetMetricSpec(context.Background(), &externalscaler.ScaledObjectRef{
			ScalerMetadata: map[string]string{util.MetadataMetricQuery: query, util.MetadataTargetValue: "1"},
		})
		if err == nil {
			t.Errorf("GetMetricSpec accepted %q", query)
		}
	}
	if _, err := scaler.GetMetricSpec(context.Background(), &externalscaler.ScaledObjectRef{}); err == nil {
		t.Fatal("GetMetricSpec accepted missing metadata")
	}
}

type failingStore struct{ types.MemStore }

func (failingStore) GetSeries(types.MetricName, types.LabelMatchers, types.OperationOverTime) ([]types.Series, error) {
	return nil, fmt.Errorf("series read failed")
}

func TestExpressionErrorsPrecedeNotFoundAndClamps(t *testing.T) {
	cfg := &util.Config{MetricStoreRetentionSeconds: 60, MetricStoreValueIfNotFound: -99, MetricStoreLazySeries: true}
	store := metric.NewMetricStore(cfg)
	ref := &externalscaler.ScaledObjectRef{ScalerMetadata: map[string]string{
		util.MetadataMetricQuery: "input + missing", util.MetadataClampMin: "1", util.MetadataClampMax: "5", util.MetadataTargetValue: "1",
	}}
	scaler := New(logr.Discard(), failingStore{store}, metric.NewParser(), cfg)
	if _, err := scaler.getMetric(ref); err == nil || !strings.Contains(err.Error(), "series read failed") {
		t.Fatalf("store error was hidden by not-found behavior: %v", err)
	}
	if _, err := scaler.GetMetricSpec(context.Background(), ref); err == nil || !strings.Contains(err.Error(), "series read failed") {
		t.Fatalf("initialization error was hidden: %v", err)
	}
	scaler = New(logr.Discard(), store, metric.NewParser(), cfg)
	for _, tt := range []struct {
		query string
		want  float64
		err   string
	}{
		{"1 + 2 * 3", 5, ""}, {"0 - 2", 1, ""}, {"1 / 0", 0, "non-finite"},
		{"missing + 10", -99, ""},
	} {
		ref.ScalerMetadata[util.MetadataMetricQuery] = tt.query
		value, err := scaler.getMetric(ref)
		if tt.err != "" {
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Errorf("%s: want error %q, got %v", tt.query, tt.err, err)
			}
		} else if err != nil || value != tt.want {
			t.Errorf("%s: want %v, got %v, %v", tt.query, tt.want, value, err)
		}
	}
	ref.ScalerMetadata[util.MetadataMetricQuery] = "1"
	for _, bound := range []string{"NaN", "+Inf"} {
		ref.ScalerMetadata[util.MetadataClampMin] = bound
		delete(ref.ScalerMetadata, util.MetadataClampMax)
		if _, err := scaler.getMetric(ref); err == nil || !strings.Contains(err.Error(), "non-finite") {
			t.Errorf("nonfinite metadata clamp %s was returned: %v", bound, err)
		}
	}
}
