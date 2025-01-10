package metric

import (
	"math"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/kedify/otel-add-on/types"
)

const (
	Eps         = .001
	NotFoundVal = -1.
)

func TestMemStorePutOneAndGetOne(t *testing.T) {
	// setup
	ms := NewMetricStore(5)
	ms.Put(types.NewMetricEntry{
		Name:             "metric1",
		MeasurementTime:  pcommon.Timestamp(time.Now().Unix()),
		MeasurementValue: 42.,
		Labels: map[string]any{
			"a": "1",
			"b": "2",
		},
	})

	// check
	val, found, err := ms.Get("metric1", map[string]any{"b": "2", "a": "1"}, types.OpLastOne, types.VecSum)
	assertMetricFound(t, val, found, err, 42.)
}

func TestMemStoreEscapeMetrics(t *testing.T) {
	// setup
	ms := NewMetricStore(5)
	ms.Put(types.NewMetricEntry{
		Name:             "metric/one",
		MeasurementTime:  pcommon.Timestamp(time.Now().Unix()),
		MeasurementValue: 42.,
		Labels: map[string]any{
			"a": "1",
			"b": "2",
		},
	})
	ms.Put(types.NewMetricEntry{
		Name:             "metric.two",
		MeasurementTime:  pcommon.Timestamp(time.Now().Unix()),
		MeasurementValue: 43.,
		Labels: map[string]any{
			"a": "2",
		},
	})

	// checks
	val1, found1, err1 := ms.Get("metric_one", map[string]any{"b": "2", "a": "1"}, types.OpLastOne, types.VecSum)
	assertMetricFound(t, val1, found1, err1, 42.)
	val2, found2, err2 := ms.Get("metric.one", map[string]any{"b": "2", "a": "1"}, types.OpLastOne, types.VecSum)
	assertMetricFound(t, val2, found2, err2, 42.)
	val3, found3, err3 := ms.Get("metric_two", map[string]any{"a": "2"}, types.OpLastOne, types.VecSum)
	assertMetricFound(t, val3, found3, err3, 43.)
}

func TestMemStoreErr(t *testing.T) {
	// setup
	ms := NewMetricStore(5)
	ms.Put(types.NewMetricEntry{
		Name:             "metric1",
		MeasurementTime:  pcommon.Timestamp(time.Now().Unix()),
		MeasurementValue: 42.,
		Labels: map[string]any{
			"a": "1",
			"b": "2",
		},
	})

	// check
	_, _, err1 := ms.Get("metric1", map[string]any{"b": "2", "a": "1"}, types.OpLastOne+"_typo", types.VecSum)
	assertMetricErr(t, err1)
	_, _, err2 := ms.Get("metric1", map[string]any{"b": "2", "a": "1"}, types.OpLastOne, "typo_"+types.VecSum)
	assertMetricErr(t, err2)
}

func TestMemStoreGetNotFound(t *testing.T) {
	// setup
	ms := NewMetricStore(5)
	ms.Put(types.NewMetricEntry{
		Name:             "metric1",
		MeasurementTime:  pcommon.Timestamp(time.Now().Unix()),
		MeasurementValue: 42.,
		Labels: map[string]any{
			"a": "1",
			"b": "2",
		},
	})

	// check
	val1, found1, err1 := ms.Get("metric-404", map[string]any{"b": "2", "a": "1"}, types.OpLastOne, types.VecSum)
	assertMetricNotFound(t, val1, found1, err1)
	if found1 {
		t.Errorf("expected: [false], got: [%v]", bool(found1))
	}

	val2, found2, err2 := ms.Get("metric-1", map[string]any{"bb": "2", "a": "1"}, types.OpLastOne, types.VecSum)
	assertMetricNotFound(t, val2, found2, err2)

	val3, found3, err3 := ms.Get("metric-1", map[string]any{"bb": "2", "a": "1", "c": "3"}, types.OpLastOne, types.VecSum)
	assertMetricNotFound(t, val3, found3, err3)

	val4, found4, err4 := ms.Get("metric-1", map[string]any{}, types.OpLastOne, types.VecSum)
	assertMetricNotFound(t, val4, found4, err4)
}

func TestMemStoreOperationLastOne(t *testing.T) {
	// setup
	ms := NewMetricStore(5)
	ms.Put(types.NewMetricEntry{
		Name:             "metric1",
		MeasurementTime:  pcommon.Timestamp(time.Now().Unix() - 1),
		MeasurementValue: 42.,
		Labels: map[string]any{
			"a": "1",
			"b": "2",
		},
	})
	ms.Put(types.NewMetricEntry{
		Name:             "metric1",
		MeasurementTime:  pcommon.Timestamp(time.Now().Unix()),
		MeasurementValue: 45.,
		Labels: map[string]any{
			"a": "1",
			"b": "2",
		},
	})

	// check
	val, found, err := ms.Get("metric1", map[string]any{"b": "2", "a": "1"}, types.OpLastOne, types.VecSum)
	assertMetricFound(t, val, found, err, 45.)
}

func TestMemStorePutTwoAndGetTwo(t *testing.T) {
	// setup
	ms := NewMetricStore(5)
	ms.Put(types.NewMetricEntry{
		Name:             "metric1",
		MeasurementTime:  pcommon.Timestamp(time.Now().Unix() - 1),
		MeasurementValue: 42.,
		Labels: map[string]any{
			"a": "1",
			"b": "2",
		},
	})
	ms.Put(types.NewMetricEntry{
		Name:             "metric2",
		MeasurementTime:  pcommon.Timestamp(time.Now().Unix()),
		MeasurementValue: 45.,
		Labels: map[string]any{
			"aa": "10",
			"bb": "20",
		},
	})

	// check
	val1, found1, err1 := ms.Get("metric1", map[string]any{"b": "2", "a": "1"}, types.OpLastOne, types.VecSum)
	assertMetricFound(t, val1, found1, err1, 42.)

	val2, found2, err2 := ms.Get("metric2", map[string]any{"bb": "20", "aa": "10"}, types.OpLastOne, types.VecSum)
	assertMetricFound(t, val2, found2, err2, 45.)
}

func TestMemStoreSumAcrossDifferentMetrics(t *testing.T) {
	// setup
	ms := NewMetricStore(5)
	ms.Put(types.NewMetricEntry{
		Name:             "metric1",
		MeasurementTime:  pcommon.Timestamp(time.Now().Unix() - 1),
		MeasurementValue: 1.,
		Labels: map[string]any{
			"a": "1",
			"b": "1",
			"c": "1",
		},
	})
	ms.Put(types.NewMetricEntry{
		Name:             "metric1",
		MeasurementTime:  pcommon.Timestamp(time.Now().Unix()),
		MeasurementValue: 2.,
		Labels: map[string]any{
			"a": "1",
			"b": "2",
			"c": "1",
		},
	})
	ms.Put(types.NewMetricEntry{
		Name:             "metric1",
		MeasurementTime:  pcommon.Timestamp(time.Now().Unix()),
		MeasurementValue: 3.,
		Labels: map[string]any{
			"a": "1",
			"b": "3",
			"c": "2",
		},
	})
	ms.Put(types.NewMetricEntry{
		Name:             "metric1",
		MeasurementTime:  pcommon.Timestamp(time.Now().Unix()),
		MeasurementValue: 4.,
		Labels: map[string]any{
			"a": "1",
			"b": "4",
			"c": "2",
		},
	})
	ms.Put(types.NewMetricEntry{
		Name:             "metric2",
		MeasurementTime:  pcommon.Timestamp(time.Now().Unix()),
		MeasurementValue: 5.,
		Labels: map[string]any{
			"a": "1",
			"b": "4",
			"c": "2",
		},
	})

	// check
	val1, found1, err1 := ms.Get("metric1", map[string]any{"a": "1", "c": "1"}, types.OpLastOne, types.VecSum)
	assertMetricFound(t, val1, found1, err1, 3.)

	val2, found2, err2 := ms.Get("metric1", map[string]any{"a": "1", "c": "2"}, types.OpLastOne, types.VecSum)
	assertMetricFound(t, val2, found2, err2, 7.)

	val3, found3, err3 := ms.Get("metric1", map[string]any{"a": "1"}, types.OpLastOne, types.VecSum)
	assertMetricFound(t, val3, found3, err3, 10.)
}

func TestMemStoreAvg(t *testing.T) {
	// setup
	ms := NewMetricStore(5)
	ms.Put(types.NewMetricEntry{
		Name:             "metric1",
		MeasurementTime:  pcommon.Timestamp(time.Now().Unix() - 1),
		MeasurementValue: 1.,
		Labels: map[string]any{
			"a": "1",
			"b": "1",
			"c": "1",
		},
	})
	ms.Put(types.NewMetricEntry{
		Name:             "metric1",
		MeasurementTime:  pcommon.Timestamp(time.Now().Unix()),
		MeasurementValue: 2.,
		Labels: map[string]any{
			"a": "1",
			"b": "2",
			"c": "1",
		},
	})
	ms.Put(types.NewMetricEntry{
		Name:             "metric1",
		MeasurementTime:  pcommon.Timestamp(time.Now().Unix()),
		MeasurementValue: 3.,
		Labels: map[string]any{
			"a": "1",
			"b": "3",
			"c": "2",
		},
	})
	ms.Put(types.NewMetricEntry{
		Name:             "metric1",
		MeasurementTime:  pcommon.Timestamp(time.Now().Unix()),
		MeasurementValue: 4.,
		Labels: map[string]any{
			"a": "1",
			"b": "4",
			"c": "2",
		},
	})
	ms.Put(types.NewMetricEntry{
		Name:             "metric2",
		MeasurementTime:  pcommon.Timestamp(time.Now().Unix()),
		MeasurementValue: 5.,
		Labels: map[string]any{
			"a": "1",
			"b": "4",
			"c": "2",
		},
	})

	// check
	val1, found1, err1 := ms.Get("metric1", map[string]any{"a": "1", "c": "1"}, types.OpLastOne, types.VecAvg)
	assertMetricFound(t, val1, found1, err1, 1.5)

	val2, found2, err2 := ms.Get("metric1", map[string]any{"a": "1", "c": "2"}, types.OpLastOne, types.VecAvg)
	assertMetricFound(t, val2, found2, err2, 3.5)

	val3, found3, err3 := ms.Get("metric1", map[string]any{"a": "1"}, types.OpLastOne, types.VecAvg)
	assertMetricFound(t, val3, found3, err3, 2.5)
}

func TestMemStoreMinMax(t *testing.T) {
	// setup
	ms := NewMetricStore(5)
	ms.Put(types.NewMetricEntry{
		Name:             "metric1",
		MeasurementTime:  pcommon.Timestamp(time.Now().Unix() - 1),
		MeasurementValue: 1.,
		Labels: map[string]any{
			"a": "1",
			"b": "1",
			"c": "1",
		},
	})
	ms.Put(types.NewMetricEntry{
		Name:             "metric1",
		MeasurementTime:  pcommon.Timestamp(time.Now().Unix()),
		MeasurementValue: 2.,
		Labels: map[string]any{
			"a": "1",
			"b": "2",
			"c": "1",
		},
	})
	ms.Put(types.NewMetricEntry{
		Name:             "metric1",
		MeasurementTime:  pcommon.Timestamp(time.Now().Unix()),
		MeasurementValue: 3.,
		Labels: map[string]any{
			"a": "1",
			"b": "3",
			"c": "2",
		},
	})
	ms.Put(types.NewMetricEntry{
		Name:             "metric1",
		MeasurementTime:  pcommon.Timestamp(time.Now().Unix()),
		MeasurementValue: 4.,
		Labels: map[string]any{
			"a": "1",
			"b": "4",
			"c": "2",
		},
	})
	ms.Put(types.NewMetricEntry{
		Name:             "metric2",
		MeasurementTime:  pcommon.Timestamp(time.Now().Unix()),
		MeasurementValue: 5.,
		Labels: map[string]any{
			"a": "1",
			"b": "4",
			"c": "2",
		},
	})

	// check
	val1, found1, err1 := ms.Get("metric1", map[string]any{"a": "1", "c": "1"}, types.OpLastOne, types.VecMin)
	assertMetricFound(t, val1, found1, err1, 1.)

	val2, found2, err2 := ms.Get("metric1", map[string]any{"a": "1", "c": "2"}, types.OpLastOne, types.VecMin)
	assertMetricFound(t, val2, found2, err2, 3.)

	val3, found3, err3 := ms.Get("metric1", map[string]any{"a": "1"}, types.OpLastOne, types.VecMin)
	assertMetricFound(t, val3, found3, err3, 1.)

	val4, found4, err4 := ms.Get("metric1", map[string]any{"a": "1"}, types.OpLastOne, types.VecMax)
	assertMetricFound(t, val4, found4, err4, 4.)
}

func TestMemStoreAvgOverTime(t *testing.T) {
	// setup
	ms := NewMetricStore(60)
	labels := map[string]any{
		"a": "1",
	}
	name := "m3t/r1c"
	setupMetrics(ms, name, 10, labels, 1., 2., 3., 4., 5.)
	val, found, err := ms.Get(types.MetricName(name), labels, types.OpAvg, types.VecSum)
	assertMetricFound(t, val, found, err, 3.)
}

func TestMemStoreAvgOverTimeStale(t *testing.T) {
	// setup
	ms := NewMetricStore(25)
	labels := map[string]any{
		"a": "1",
	}
	name := "m3t/r1c"
	setupMetrics(ms, name, 10, labels, 1., 2., 3., 4., 5.)
	val, found, err := ms.Get(types.MetricName(name), labels, types.OpAvg, types.VecSum)
	assertMetricFound(t, val, found, err, 4.5)
}

func TestMemStoreMinOverTime(t *testing.T) {
	// setup
	ms := NewMetricStore(60)
	labels := map[string]any{
		"a": "1",
	}
	name := "m3t/r1c"
	setupMetrics(ms, name, 10, labels, 99., 2., 1., 4., 5.)
	val, found, err := ms.Get(types.MetricName(name), labels, types.OpMin, types.VecSum)
	assertMetricFound(t, val, found, err, 1.)
}

func TestMemStoreLastOneOverTime(t *testing.T) {
	// setup
	ms := NewMetricStore(60)
	labels := map[string]any{
		"a": "1",
	}
	name := "m3t/r1c"
	setupMetrics(ms, name, 10, labels, 99., 2., 1., 4., 5.)
	val, found, err := ms.Get(types.MetricName(name), labels, types.OpLastOne, types.VecSum)
	assertMetricFound(t, val, found, err, 5.)
}

func TestMemStoreMinOverTimeStale(t *testing.T) {
	// setup
	ms := NewMetricStore(35)
	labels := map[string]any{
		"a": "1",
	}
	name := "m3t/r1c"
	setupMetrics(ms, name, 10, labels, 1., 2., 3., 4., 5.)
	val, found, err := ms.Get(types.MetricName(name), labels, types.OpMin, types.VecSum)
	assertMetricFound(t, val, found, err, 3.)
}

func TestMemStoreCountsOverTime(t *testing.T) {
	// setup
	ms := NewMetricStore(80)
	labels := map[string]any{
		"a": "1",
	}
	name := "m3t/r1c"
	setupMetrics(ms, name, 10, labels, 1., 2., 3., 4., 5., 6.)
	val, found, err := ms.Get(types.MetricName(name), labels, types.OpCount, types.VecSum)
	assertMetricFound(t, val, found, err, 6.)
}

func TestMemStoreRateOverTime1(t *testing.T) {
	// setup
	ms := NewMetricStore(200)
	labels := map[string]any{
		"a": "1",
	}
	name := "m3t/r1c"
	setupMetrics(ms, name, 10, labels, 0., 10., 20., 30., 40., 50., 60.)
	val, found, err := ms.Get(types.MetricName(name), labels, types.OpRate, types.VecSum)
	assertMetricFound(t, val, found, err, 1.)
}

func TestMemStoreRateOverTime2(t *testing.T) {
	// setup
	ms := NewMetricStore(200)
	labels := map[string]any{
		"a": "1",
	}
	name := "m3t/r1c"
	setupMetrics(ms, name, 20, labels, 0., 10., 20., 30., 40., 50., 60., 70.)
	val, found, err := ms.Get(types.MetricName(name), labels, types.OpRate, types.VecSum)
	assertMetricFound(t, val, found, err, .5)
}

func TestMemStoreRateOverTime3(t *testing.T) {
	// setup
	ms := NewMetricStore(200)
	labels := map[string]any{
		"a": "1",
	}
	name := "m3t/r1c"
	setupMetrics(ms, name, 10, labels, 10., 30., 50., 70., 90., 110.)
	val, found, err := ms.Get(types.MetricName(name), labels, types.OpRate, types.VecSum)
	assertMetricFound(t, val, found, err, 2.)
}

func TestMemStoreRateOverTime4(t *testing.T) {
	// setup
	ms := NewMetricStore(500)
	labels := map[string]any{
		"a": "1",
	}
	name := "m3t/r1c"
	setupMetrics(ms, name, 30, labels, 0., 100., 200., 300., 400., 500.)
	val, found, err := ms.Get(types.MetricName(name), labels, types.OpRate, types.VecSum)
	assertMetricFound(t, val, found, err, 3.333)
}

func TestMemStoreRateOverTimeForgetOld(t *testing.T) {
	// setup
	ms := NewMetricStore(60)
	labels := map[string]any{
		"a": "1",
	}
	name := "m3t/r1c"
	setupMetrics(ms, name, 10, labels, 0., 100., 300., 310., 320., 330., 340., 350.)
	val, found, err := ms.Get(types.MetricName(name), labels, types.OpRate, types.VecSum)
	assertMetricFound(t, val, found, err, 1.)
}

func TestMemStoreSumOverAverages(t *testing.T) {
	// setup
	ms := NewMetricStore(60)
	labels1 := map[string]any{
		"a": "1",
		"b": "2",
	}
	labels2 := map[string]any{
		"a": "1",
		"b": "3",
	}
	name1 := "m3t/r1c"
	setupMetrics(ms, name1, 1, labels1, 1., 2., 3., 4., 5., 6.) // avg is 3.5
	setupMetrics(ms, name1, 1, labels2, 2., 2., 2., 4., 2., 2.) // avg is 2.333
	setupMetrics(ms, "noise", 1, labels2, 1., 2., 3., 4., 5.)   // this shouldn't be included
	val, found, err := ms.Get(types.MetricName(name1), map[string]any{
		"a": "1",
	}, types.OpAvg, types.VecSum)
	assertMetricFound(t, val, found, err, 3.5+2.333)
}

func TestMemStoreCount(t *testing.T) {
	// setup
	ms := NewMetricStore(60)
	labels1 := map[string]any{
		"a": "1",
		"b": "2",
	}
	labels2 := map[string]any{
		"a": "1",
		"b": "3",
	}
	labels3 := map[string]any{
		"a": "1",
		"b": "4",
	}
	labels4 := map[string]any{
		"a": "2",
		"b": "2",
	}
	name1 := "metric_name"
	setupMetrics(ms, name1, 1, labels1, 1., 2.)
	setupMetrics(ms, name1, 1, labels2, 1., 2., 3.)
	setupMetrics(ms, name1, 1, labels3, 1., 2., 3., 4.)
	setupMetrics(ms, name1, 1, labels4, 1., 2., 3., 4., 5.)
	setupMetrics(ms, "noise", 1, labels2, 1., 2., 3., 4., 5.) // this shouldn't be included
	val1, found1, err1 := ms.Get(types.MetricName(name1), map[string]any{
		"a": "1",
	}, types.OpAvg, types.VecCount)
	assertMetricFound(t, val1, found1, err1, 3.)

	val2, found2, err2 := ms.Get(types.MetricName(name1), map[string]any{
		"b": "2",
	}, types.OpAvg, types.VecCount)
	assertMetricFound(t, val2, found2, err2, 2.)
	val3, found3, err3 := ms.Get(types.MetricName(name1), map[string]any{}, types.OpAvg, types.VecCount)
	assertMetricFound(t, val3, found3, err3, 4.)
	val4, found4, err4 := ms.Get(types.MetricName(name1), map[string]any{
		"a": "1",
		"b": "2",
	}, types.OpAvg, types.VecCount)
	assertMetricFound(t, val4, found4, err4, 1.)
}

func setupMetrics(store types.MemStore, name string, secondsStep int64, labels map[string]any, vals ...float64) {
	now := time.Now().Unix()
	for i, v := range vals {
		store.Put(types.NewMetricEntry{
			Name:             types.MetricName(name),
			MeasurementTime:  pcommon.Timestamp(now - int64(len(vals))*secondsStep + int64(i)*secondsStep),
			MeasurementValue: v,
			Labels:           labels,
		})
	}
}

func assertMetric(t *testing.T, val float64, found types.Found, err error, expectedVal float64, expectedFound bool, expectedErr error) {
	if err != expectedErr || bool(found) != expectedFound || !equalsFloat(val, expectedVal) {
		t.Errorf("expected: [%f, %v, %v], got: [%f, %v, %v]", expectedVal, expectedFound, expectedErr, val, found, err)
	}
}

func assertMetricNotFound(t *testing.T, val float64, found types.Found, err error) {
	assertMetric(t, val, found, err, NotFoundVal, false, nil)
}

func assertMetricFound(t *testing.T, val float64, found types.Found, err error, expectedVal float64) {
	assertMetric(t, val, found, err, expectedVal, true, nil)
}

func assertMetricErr(t *testing.T, err error) {
	if err == nil {
		t.Errorf("expected: [err], got: [%v]", err)
	}
}

func equalsFloat(a, b float64) bool {
	return math.Abs(a-b) < Eps
}
