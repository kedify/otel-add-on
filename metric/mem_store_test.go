package metric

import (
	"math"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
)

const eps = .001

func TestMemStorePutOneAndGetOne(t *testing.T) {
	// setup
	ms := NewMetricStore(5)
	ms.Put(MetricEntry{
		Name: "metric1",
		ObservedValue: ObservedValue{
			Value:      42.,
			LastUpdate: pcommon.Timestamp(time.Now().Unix()),
		},
		Labels: map[string]string{
			"a": "1",
			"b": "2",
		},
	})

	// check
	val, stale, found := ms.Get("metric1", map[string]string{"b": "2", "a": "1"}, Sum)
	if bool(stale) || !bool(found) || !equals(val, 42.) {
		t.Errorf("expected: [42.0, false, true], got: [%f, %v, %v]", val, bool(stale), bool(found))
	}
}

func TestMemStoreGetNotFound(t *testing.T) {
	// setup
	ms := NewMetricStore(5)
	ms.Put(MetricEntry{
		Name: "metric1",
		ObservedValue: ObservedValue{
			Value:      42.,
			LastUpdate: pcommon.Timestamp(time.Now().Unix()),
		},
		Labels: map[string]string{
			"a": "1",
			"b": "2",
		},
	})

	// check
	_, _, found1 := ms.Get("metric-404", map[string]string{"b": "2", "a": "1"}, Sum)
	if found1 {
		t.Errorf("expected: [false], got: [%v]", bool(found1))
	}

	_, _, found2 := ms.Get("metric-1", map[string]string{"bb": "2", "a": "1"}, Sum)
	if found2 {
		t.Errorf("expected: [false], got: [%v]", bool(found2))
	}

	_, _, found3 := ms.Get("metric-1", map[string]string{"bb": "2", "a": "1", "c": "3"}, Sum)
	if found3 {
		t.Errorf("expected: [false], got: [%v]", bool(found3))
	}

	_, _, found4 := ms.Get("metric-1", map[string]string{}, Sum)
	if found4 {
		t.Errorf("expected: [false], got: [%v]", bool(found4))
	}
}

func TestMemStoreOverrideLatest(t *testing.T) {
	// setup
	ms := NewMetricStore(5)
	ms.Put(MetricEntry{
		Name: "metric1",
		ObservedValue: ObservedValue{
			Value:      42.,
			LastUpdate: pcommon.Timestamp(time.Now().Unix() - 1),
		},
		Labels: map[string]string{
			"a": "1",
			"b": "2",
		},
	})
	ms.Put(MetricEntry{
		Name: "metric1",
		ObservedValue: ObservedValue{
			Value:      45.,
			LastUpdate: pcommon.Timestamp(time.Now().Unix()),
		},
		Labels: map[string]string{
			"a": "1",
			"b": "2",
		},
	})

	// check
	val, stale, found := ms.Get("metric1", map[string]string{"b": "2", "a": "1"}, Sum)
	if bool(stale) || !bool(found) || !equals(val, 45.) {
		t.Errorf("expected: [45.0, false, true], got: [%f, %v, %v]", val, bool(stale), bool(found))
	}
}

func TestMemStorePutTwoAndGetTwo(t *testing.T) {
	// setup
	ms := NewMetricStore(5)
	ms.Put(MetricEntry{
		Name: "metric1",
		ObservedValue: ObservedValue{
			Value:      42.,
			LastUpdate: pcommon.Timestamp(time.Now().Unix() - 1),
		},
		Labels: map[string]string{
			"a": "1",
			"b": "2",
		},
	})
	ms.Put(MetricEntry{
		Name: "metric2",
		ObservedValue: ObservedValue{
			Value:      45.,
			LastUpdate: pcommon.Timestamp(time.Now().Unix()),
		},
		Labels: map[string]string{
			"aa": "10",
			"bb": "20",
		},
	})

	// check
	val1, stale1, found1 := ms.Get("metric1", map[string]string{"b": "2", "a": "1"}, Sum)
	if bool(stale1) || !bool(found1) || !equals(val1, 42.) {
		t.Errorf("expected: [45.0, false, true], got: [%f, %v, %v]", val1, bool(stale1), bool(found1))
	}
	val2, stale2, found2 := ms.Get("metric2", map[string]string{"bb": "20", "aa": "10"}, Sum)
	if bool(stale2) || !bool(found2) || !equals(val2, 45.) {
		t.Errorf("expected: [45.0, false, true], got: [%f, %v, %v]", val2, bool(stale2), bool(found2))
	}
}

func TestMemStoreSum(t *testing.T) {
	// setup
	ms := NewMetricStore(5)
	ms.Put(MetricEntry{
		Name: "metric1",
		ObservedValue: ObservedValue{
			Value:      1.,
			LastUpdate: pcommon.Timestamp(time.Now().Unix() - 1),
		},
		Labels: map[string]string{
			"a": "1",
			"b": "1",
			"c": "1",
		},
	})
	ms.Put(MetricEntry{
		Name: "metric1",
		ObservedValue: ObservedValue{
			Value:      2.,
			LastUpdate: pcommon.Timestamp(time.Now().Unix()),
		},
		Labels: map[string]string{
			"a": "1",
			"b": "2",
			"c": "1",
		},
	})
	ms.Put(MetricEntry{
		Name: "metric1",
		ObservedValue: ObservedValue{
			Value:      3.,
			LastUpdate: pcommon.Timestamp(time.Now().Unix()),
		},
		Labels: map[string]string{
			"a": "1",
			"b": "3",
			"c": "2",
		},
	})
	ms.Put(MetricEntry{
		Name: "metric1",
		ObservedValue: ObservedValue{
			Value:      4.,
			LastUpdate: pcommon.Timestamp(time.Now().Unix()),
		},
		Labels: map[string]string{
			"a": "1",
			"b": "4",
			"c": "2",
		},
	})
	ms.Put(MetricEntry{
		Name: "metric2",
		ObservedValue: ObservedValue{
			Value:      5.,
			LastUpdate: pcommon.Timestamp(time.Now().Unix()),
		},
		Labels: map[string]string{
			"a": "1",
			"b": "4",
			"c": "2",
		},
	})

	// check
	val1, stale1, found1 := ms.Get("metric1", map[string]string{"a": "1", "c": "1"}, Sum)
	if bool(stale1) || !bool(found1) || !equals(val1, 3.) {
		t.Errorf("expected: [3.0, false, true], got: [%f, %v, %v]", val1, bool(stale1), bool(found1))
	}

	val2, stale2, found2 := ms.Get("metric1", map[string]string{"a": "1", "c": "2"}, Sum)
	if bool(stale2) || !bool(found2) || !equals(val2, 7.) {
		t.Errorf("expected: [7.0, false, true], got: [%f, %v, %v]", val2, bool(stale2), bool(found2))
	}

	val3, stale3, found3 := ms.Get("metric1", map[string]string{"a": "1"}, Sum)
	if bool(stale3) || !bool(found3) || !equals(val3, 10.) {
		t.Errorf("expected: [10.0, false, true], got: [%f, %v, %v]", val3, bool(stale3), bool(found3))
	}
}

func TestMemStoreAvg(t *testing.T) {
	// setup
	ms := NewMetricStore(5)
	ms.Put(MetricEntry{
		Name: "metric1",
		ObservedValue: ObservedValue{
			Value:      1.,
			LastUpdate: pcommon.Timestamp(time.Now().Unix() - 1),
		},
		Labels: map[string]string{
			"a": "1",
			"b": "1",
			"c": "1",
		},
	})
	ms.Put(MetricEntry{
		Name: "metric1",
		ObservedValue: ObservedValue{
			Value:      2.,
			LastUpdate: pcommon.Timestamp(time.Now().Unix()),
		},
		Labels: map[string]string{
			"a": "1",
			"b": "2",
			"c": "1",
		},
	})
	ms.Put(MetricEntry{
		Name: "metric1",
		ObservedValue: ObservedValue{
			Value:      3.,
			LastUpdate: pcommon.Timestamp(time.Now().Unix()),
		},
		Labels: map[string]string{
			"a": "1",
			"b": "3",
			"c": "2",
		},
	})
	ms.Put(MetricEntry{
		Name: "metric1",
		ObservedValue: ObservedValue{
			Value:      4.,
			LastUpdate: pcommon.Timestamp(time.Now().Unix()),
		},
		Labels: map[string]string{
			"a": "1",
			"b": "4",
			"c": "2",
		},
	})
	ms.Put(MetricEntry{
		Name: "metric2",
		ObservedValue: ObservedValue{
			Value:      5.,
			LastUpdate: pcommon.Timestamp(time.Now().Unix()),
		},
		Labels: map[string]string{
			"a": "1",
			"b": "4",
			"c": "2",
		},
	})

	// check
	val1, stale1, found1 := ms.Get("metric1", map[string]string{"a": "1", "c": "1"}, Avg)
	if bool(stale1) || !bool(found1) || !equals(val1, 1.5) {
		t.Errorf("expected: [1.5, false, true], got: [%f, %v, %v]", val1, bool(stale1), bool(found1))
	}

	val2, stale2, found2 := ms.Get("metric1", map[string]string{"a": "1", "c": "2"}, Avg)
	if bool(stale2) || !bool(found2) || !equals(val2, 3.5) {
		t.Errorf("expected: [3.5, false, true], got: [%f, %v, %v]", val2, bool(stale2), bool(found2))
	}

	val3, stale3, found3 := ms.Get("metric1", map[string]string{"a": "1"}, Avg)
	if bool(stale3) || !bool(found3) || !equals(val3, 2.5) {
		t.Errorf("expected: [2.5, false, true], got: [%f, %v, %v]", val3, bool(stale3), bool(found3))
	}
}

func TestMemStoreMinMax(t *testing.T) {
	// setup
	ms := NewMetricStore(5)
	ms.Put(MetricEntry{
		Name: "metric1",
		ObservedValue: ObservedValue{
			Value:      1.,
			LastUpdate: pcommon.Timestamp(time.Now().Unix() - 1),
		},
		Labels: map[string]string{
			"a": "1",
			"b": "1",
			"c": "1",
		},
	})
	ms.Put(MetricEntry{
		Name: "metric1",
		ObservedValue: ObservedValue{
			Value:      2.,
			LastUpdate: pcommon.Timestamp(time.Now().Unix()),
		},
		Labels: map[string]string{
			"a": "1",
			"b": "2",
			"c": "1",
		},
	})
	ms.Put(MetricEntry{
		Name: "metric1",
		ObservedValue: ObservedValue{
			Value:      3.,
			LastUpdate: pcommon.Timestamp(time.Now().Unix()),
		},
		Labels: map[string]string{
			"a": "1",
			"b": "3",
			"c": "2",
		},
	})
	ms.Put(MetricEntry{
		Name: "metric1",
		ObservedValue: ObservedValue{
			Value:      4.,
			LastUpdate: pcommon.Timestamp(time.Now().Unix()),
		},
		Labels: map[string]string{
			"a": "1",
			"b": "4",
			"c": "2",
		},
	})
	ms.Put(MetricEntry{
		Name: "metric2",
		ObservedValue: ObservedValue{
			Value:      5.,
			LastUpdate: pcommon.Timestamp(time.Now().Unix()),
		},
		Labels: map[string]string{
			"a": "1",
			"b": "4",
			"c": "2",
		},
	})

	// check
	val1, stale1, found1 := ms.Get("metric1", map[string]string{"a": "1", "c": "1"}, Min)
	if bool(stale1) || !bool(found1) || !equals(val1, 1.) {
		t.Errorf("expected: [1., false, true], got: [%f, %v, %v]", val1, bool(stale1), bool(found1))
	}

	val2, stale2, found2 := ms.Get("metric1", map[string]string{"a": "1", "c": "2"}, Min)
	if bool(stale2) || !bool(found2) || !equals(val2, 3.) {
		t.Errorf("expected: [3.0, false, true], got: [%f, %v, %v]", val2, bool(stale2), bool(found2))
	}

	val3, stale3, found3 := ms.Get("metric1", map[string]string{"a": "1"}, Min)
	if bool(stale3) || !bool(found3) || !equals(val3, 1.) {
		t.Errorf("expected: [1.0, false, true], got: [%f, %v, %v]", val3, bool(stale3), bool(found3))
	}

	val4, stale4, found4 := ms.Get("metric1", map[string]string{"a": "1"}, Max)
	if bool(stale4) || !bool(found4) || !equals(val4, 4.) {
		t.Errorf("expected: [4.0, false, true], got: [%f, %v, %v]", val4, bool(stale4), bool(found4))
	}
}

func equals(a, b float64) bool {
	return math.Abs(a-b) < eps
}
