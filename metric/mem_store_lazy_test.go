package metric

import (
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"

	ty "github.com/kedify/otel-add-on/types"
	"github.com/kedify/otel-add-on/util"
)

func TestLazyMemStorePutOneAndGetNothing(t *testing.T) {
	// setup
	ms := NewMetricStore(&util.Config{
		MetricStoreRetentionSeconds: 5,
		MetricStoreLazySeries:       true,
	})
	ms.Put(ty.NewMetricEntry{
		Name:             "metric1",
		MeasurementTime:  pcommon.Timestamp(time.Now().Unix()),
		MeasurementValue: 42.,
		Labels: map[string]any{
			"a": "1",
			"b": "2",
		},
	})

	// check
	assertNotThere(t, ms, "metric1", map[string]any{"b": "2", "a": "1"}, ty.OpLastOne, ty.VecSum)
}

func TestLazyMemStoreGetPutAndGet(t *testing.T) {
	// setup
	ms := NewMetricStore(&util.Config{
		MetricStoreRetentionSeconds: 70,
		MetricStoreLazySeries:       true,
	})
	labels := map[string]any{
		"a": "1",
	}
	name := "m3t/r1c"
	assertNotThere(t, ms, name, labels, ty.OpMin, ty.VecSum)
	setupMetrics(ms, name, 10, labels, 1., 2., 3., 4., 5.)

	// min
	assertMetricFoundAndEqualTo(t, ms, name, labels, ty.OpMin, ty.VecSum, 1.)

	// max
	assertMetricFoundAndEqualTo(t, ms, name, labels, ty.OpMax, ty.VecSum, 5.)

	// rate
	assertMetricFoundAndEqualTo(t, ms, name, labels, ty.OpRate, ty.VecSum, .1)
}

func TestMemStoreLazyAggregatesFirstCallNotFound(t *testing.T) {
	// setup
	ms := NewMetricStore(&util.Config{
		MetricStoreRetentionSeconds: 5,
		MetricStoreLazyAggregates:   true,
	})
	ms.Put(ty.NewMetricEntry{
		Name:             "metric1",
		MeasurementTime:  pcommon.Timestamp(time.Now().Unix()),
		MeasurementValue: 42.,
		Labels: map[string]any{
			"a": "1",
			"b": "2",
		},
	})

	// check
	assertNotThere(t, ms, "metric1", map[string]any{"b": "2", "a": "1"}, ty.OpLastOne, ty.VecSum)
}

func TestMemStoreLazyAggregatesOneAgg(t *testing.T) {
	// setup
	ms := NewMetricStore(&util.Config{
		MetricStoreRetentionSeconds: 70,
		MetricStoreLazyAggregates:   true,
	})
	labels := map[string]any{
		"a": "1",
	}
	name := "m3tr1c"
	assertNotThere(t, ms, name, labels, ty.OpMin, ty.VecSum)
	setupMetrics(ms, name, 10, labels, 1., 2., 3., 4., 5.)

	// min
	assertMetricFoundAndEqualTo(t, ms, name, labels, ty.OpMin, ty.VecSum, 1.)

	// max
	assertNotThere(t, ms, name, labels, ty.OpMax, ty.VecSum)
}

func TestMemStoreLazyAggregatesAddingOnTheFly(t *testing.T) {
	// setup
	ms := NewMetricStore(&util.Config{
		MetricStoreRetentionSeconds: 70,
		MetricStoreLazyAggregates:   true,
	})
	labels := map[string]any{
		"a": "1",
	}
	name := "m3tr1c"
	assertNotThere(t, ms, name, labels, ty.OpMin, ty.VecSum)
	setupMetrics(ms, name, 10, labels, 1., 2., 3., 4., 5.)

	// min
	assertMetricFoundAndEqualTo(t, ms, name, labels, ty.OpMin, ty.VecSum, 1.)

	// max
	assertNotThere(t, ms, name, labels, ty.OpMax, ty.VecSum)
	// check if initialized to 0 value
	assertMetricFoundAndEqualTo(t, ms, name, labels, ty.OpMax, ty.VecSum, 0.)
	setupMetrics(ms, name, 10, labels, 6., 7., 8., 9., 10.)
	assertMetricFoundAndEqualTo(t, ms, name, labels, ty.OpMax, ty.VecSum, 10.)
}

func TestLazyMemStoreAndLazyAggregatesComplex(t *testing.T) {
	// setup
	ms := NewMetricStore(&util.Config{
		MetricStoreRetentionSeconds: 700,
		MetricStoreLazySeries:       true,
		MetricStoreLazyAggregates:   true,
	})
	labels := map[string]any{
		"a": "1",
		"b": "2",
	}
	name := "m3tr1c"
	now := time.Now().Unix() - 600
	// all these should be rejected
	setupMetricsWithNow(ms, now, name, 10, labels, 1., 2., 3., 4., 5.)

	// min
	assertNotThere(t, ms, name, labels, ty.OpMin, ty.VecSum)
	assertNotThere(t, ms, name, labels, ty.OpMin, ty.VecSum)
	now += 100
	setupMetricsWithNow(ms, now, name, 10, labels, 6., 7., 8., 9., 10.)
	assertMetricFoundAndEqualTo(t, ms, name, labels, ty.OpMin, ty.VecSum, 6.)

	// max
	assertNotThere(t, ms, name, labels, ty.OpMax, ty.VecSum)
	now += 100
	setupMetricsWithNow(ms, now, name, 10, labels, 1., 2., 3., 4., 5.)
	assertMetricFoundAndEqualTo(t, ms, name, labels, ty.OpMax, ty.VecSum, 10.)

	// rate
	assertNotThere(t, ms, name, labels, ty.OpRate, ty.VecSum)
	now += 100
	setupMetricsWithNow(ms, now, name, 10, labels, 20., 30., 40., 50., 600.)
	// (600 - 6) / (600 - 360) - 6. is the first value, 600 the last one and 240 is the delta in time of measurements
	assertMetricFoundAndEqualTo(t, ms, name, labels, ty.OpRate, ty.VecSum, 2.475)

	// count
	assertNotThere(t, ms, name, labels, ty.OpCount, ty.VecSum)
	now += 100
	setupMetricsWithNow(ms, now, name, 10, labels, 5., 4., 3., 4., 5., 1.)
	assertMetricFoundAndEqualTo(t, ms, name, labels, ty.OpCount, ty.VecSum, 21.)

	// avg
	assertNotThere(t, ms, name, labels, ty.OpAvg, ty.VecSum)
	now += 100
	setupMetricsWithNow(ms, now, name, 10, labels, 1., 2., 3., 4., 5.)
	// all the above inserted values / count (26)
	assertMetricFoundAndEqualTo(t, ms, name, labels, ty.OpAvg, ty.VecSum, 32.)

	labels2 := map[string]any{
		"a": "1",
		"b": "3",
	}
	// check if initialized to 0 value
	assertMetricFoundAndEqualTo(t, ms, name, labels2, ty.OpCount, ty.VecSum, 0.)
	setupMetricsWithNow(ms, now, name, 10, labels2, 1., 2., 3., 4., 5.)
	assertMetricFoundAndEqualTo(t, ms, name, labels, ty.OpCount, ty.VecSum, 26.)
	assertMetricFoundAndEqualTo(t, ms, name, labels2, ty.OpCount, ty.VecSum, 5.)
	assertMetricFoundAndEqualTo(t, ms, name, map[string]any{"a": "1"}, ty.OpCount, ty.VecSum, 31.)
}
