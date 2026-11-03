package metric

import (
	"fmt"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/kedify/otel-add-on/types"
	"github.com/kedify/otel-add-on/util"
)

func TestMemStoreAdvancedLabelMatching(t *testing.T) {
	for _, lazySeries := range []bool{false, true} {
		for _, lazyAggregates := range []bool{false, true} {
			t.Run(fmt.Sprintf("lazySeries=%t/lazyAggregates=%t", lazySeries, lazyAggregates), func(t *testing.T) {
				store := NewMetricStore(&util.Config{
					MetricStoreRetentionSeconds: 60,
					MetricStoreLazySeries:       lazySeries,
					MetricStoreLazyAggregates:   lazyAggregates,
					MetricStoreValueIfNotFound:  -99,
				})
				name, matchers, agg, err := NewParser().Parse(`queue_depth{queue=~"orders-.*",environment!="dev"}`)
				if err != nil {
					t.Fatal(err)
				}
				// Register the query before ingesting data, including in lazy modes.
				value, found, err := store.Get(name, matchers, types.OpLastOne, agg)
				assertMetric(t, value, found, err, -99, false, nil)
				for _, series := range []struct {
					labels types.Labels
					value  float64
				}{
					{types.Labels{"queue": "orders-us", "environment": "prod"}, 10},
					{types.Labels{"queue": "orders-eu", "environment": "staging"}, 20},
					{types.Labels{"queue": "orders-unknown"}, 5},
					{types.Labels{"queue": "orders-dev", "environment": "dev"}, 100},
					{types.Labels{"queue": "payments", "environment": "prod"}, 100},
					{types.Labels{"queue": "xorders-us", "environment": "prod"}, 100},
					{types.Labels{"environment": "prod"}, 100},
				} {
					setupMetrics(store, string(name), 1, series.labels, series.value, series.value)
				}
				for _, tt := range []struct {
					agg  types.AggregationOverVectors
					want float64
				}{{types.VecSum, 35}, {types.VecAvg, 35.0 / 3}, {types.VecMin, 5}, {types.VecMax, 20}, {types.VecCount, 3}} {
					value, found, err := store.Get(name, matchers, types.OpLastOne, tt.agg)
					assertMetricFound(t, value, found, err, tt.want)
				}
				for _, tt := range []struct {
					query string
					want  float64
				}{
					{`queue_depth{queue=~"orders-.*",environment!="dev",environment!="staging"}`, 15},
					{`queue_depth{queue=~"orders-.*",queue!~".*-dev",environment=~"prod|staging"}`, 30},
					{`queue_depth{queue=~"orders-.*",environment=""}`, 5},
				} {
					name, matchers, agg, err := NewParser().Parse(tt.query)
					if err != nil {
						t.Fatal(err)
					}
					value, found, err := store.Get(name, matchers, types.OpLastOne, agg)
					assertMetricFound(t, value, found, err, tt.want)
				}
				name, matchers, agg, err = NewParser().Parse(`queue_depth{queue=~"missing-.*"}`)
				if err != nil {
					t.Fatal(err)
				}
				value, found, err = store.Get(name, matchers, types.OpLastOne, agg)
				assertMetric(t, value, found, err, -99, false, nil)
			})
		}
	}
}

func TestMemStoreIncludesExactLabelSetAndSupersets(t *testing.T) {
	store := newMetricStore(60, false, false)
	setupMetrics(store, "queue_depth", 1, nil, 2)
	setupMetrics(store, "queue_depth", 1, types.Labels{"queue": "orders"}, 5)
	setupMetrics(store, "queue_depth", 1, types.Labels{"queue": "orders", "environment": "prod"}, 10)
	setupMetrics(store, "queue_depth", 1, types.Labels{"queue": "payments"}, 100)
	for _, tt := range []struct {
		query string
		want  float64
	}{
		{`queue_depth{queue="orders"}`, 15},
		{`count(queue_depth{queue="orders"})`, 2},
		{"queue_depth", 117},
		{"count(queue_depth)", 4},
	} {
		name, matchers, agg, err := NewParser().Parse(tt.query)
		if err != nil {
			t.Fatal(err)
		}
		value, found, err := store.Get(name, matchers, types.OpLastOne, agg)
		assertMetricFound(t, value, found, err, tt.want)
	}
}

func TestMemStoreMatcherStaleness(t *testing.T) {
	for _, query := range []string{`queue_depth{queue="orders-old"}`, `count(queue_depth{queue=~"orders-old"})`, "queue_depth"} {
		t.Run(query, func(t *testing.T) {
			store := NewMetricStore(&util.Config{MetricStoreRetentionSeconds: 60, MetricStoreValueIfNotFound: -99})
			staleLabels := types.Labels{"queue": "orders-old", "environment": "prod"}
			store.Put(types.NewMetricEntry{
				Name: "queue_depth", Labels: staleLabels, MeasurementValue: 100,
				MeasurementTime: pcommon.Timestamp(time.Now().Unix() - 120),
			})
			freshLabels := types.Labels{"queue": "orders-new"}
			setupMetrics(store, "queue_depth", 1, freshLabels, 10)
			name, matchers, agg, err := NewParser().Parse(query)
			if err != nil {
				t.Fatal(err)
			}
			value, found, err := store.Get(name, matchers, types.OpLastOne, agg)
			if query == "queue_depth" {
				assertMetricFound(t, value, found, err, 10)
			} else {
				assertMetric(t, value, found, err, -99, false, nil)
			}
			series, ok := store.GetStore().Load("queue_depth")
			if !ok {
				t.Fatal("metric must remain in storage")
			}
			if _, found := series.Load(hashOfMap(staleLabels)); found {
				t.Error("stale series was not removed using its stored key")
			}
			if _, found := series.Load(hashOfMap(freshLabels)); !found {
				t.Error("fresh series was removed")
			}
		})
	}
}

func TestMemStoreMatchedZeroIsFound(t *testing.T) {
	store := newMetricStore(60, false, false)
	setupMetrics(store, "queue_depth", 1, types.Labels{"queue": "orders"}, 0)
	name, matchers, agg, err := NewParser().Parse(`queue_depth{queue=~"orders.*"}`)
	if err != nil {
		t.Fatal(err)
	}
	value, found, err := store.Get(name, matchers, types.OpLastOne, agg)
	assertMetricFound(t, value, found, err, 0)
}

func TestMemStoreCountWithoutTimeAggregate(t *testing.T) {
	for _, lazySeries := range []bool{false, true} {
		for _, lazyAggregates := range []bool{false, true} {
			t.Run(fmt.Sprintf("lazySeries=%t/lazyAggregates=%t", lazySeries, lazyAggregates), func(t *testing.T) {
				store := NewMetricStore(&util.Config{
					MetricStoreRetentionSeconds: 60,
					MetricStoreLazySeries:       lazySeries,
					MetricStoreLazyAggregates:   lazyAggregates,
					MetricStoreValueIfNotFound:  -99,
				})
				name, matchers, agg, err := NewParser().Parse(`count(queue_depth{queue="orders"})`)
				if err != nil {
					t.Fatal(err)
				}
				value, found, err := store.Get(name, matchers, types.OpMin, agg)
				assertMetric(t, value, found, err, -99, false, nil)
				now := pcommon.Timestamp(time.Now().Unix())
				store.Put(types.NewMetricEntry{
					Name: name, Labels: types.Labels{"queue": "orders"}, MeasurementValue: 10, MeasurementTime: now,
				})
				// A first sample does not yet have an OpMin aggregate; count still
				// selects the series, and does not need to read that aggregate.
				value, found, err = store.Get(name, matchers, types.OpMin, agg)
				assertMetricFound(t, value, found, err, 1)
				for _, labels := range []types.Labels{
					{"queue": "orders", "environment": "prod"},
					{"queue": "payments"},
				} {
					store.Put(types.NewMetricEntry{Name: name, Labels: labels, MeasurementValue: 20, MeasurementTime: now})
				}
				store.Put(types.NewMetricEntry{
					Name: name, Labels: types.Labels{"queue": "orders", "environment": "stale"},
					MeasurementValue: 100, MeasurementTime: now - 120,
				})
				value, found, err = store.Get(name, matchers, types.OpMin, agg)
				assertMetricFound(t, value, found, err, 2)
			})
		}
	}
}
