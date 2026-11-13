package metric

import (
	"errors"
	"math"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/kedify/otel-add-on/types"
	"github.com/kedify/otel-add-on/util"
)

func TestHAHistory(t *testing.T) {
	legacy := NewMetricStore(&util.Config{MetricStoreRetentionSeconds: 10}).(ms)
	if err := legacy.Ready(); err != nil {
		t.Fatal(err)
	}
	store := NewMetricStore(&util.Config{HAEnabled: true, MetricStoreRetentionSeconds: 10, HAWarmupSeconds: 5}).(ms)
	now := time.Now().Truncate(time.Second)
	store.ha.now = func() time.Time { return now }
	put := func(name string, at time.Time, value float64) {
		store.Put(types.NewMetricEntry{Name: types.MetricName(name), MeasurementTime: pcommon.NewTimestampFromTime(at), MeasurementValue: value})
	}
	assertReady := func(want bool) {
		t.Helper()
		if err := store.Ready(); (err == nil) != want {
			t.Fatalf("ready=%t, want %t: %v", err == nil, want, err)
		}
	}
	assertReady(false)
	put("old", now.Add(-time.Hour), 1)
	put("future", now.Add(time.Hour), 1)
	put("invalid", now, math.NaN())
	assertReady(false)
	put("counter", now, 10)
	assertReady(false)
	if _, _, err := store.Get("counter", nil, types.OpLastOne, types.VecSum); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Get("counter", nil, types.OpRate, types.VecSum); !errors.Is(err, ErrHistoryNotReady) {
		t.Fatalf("cold rate: %v", err)
	}
	now = now.Add(5 * time.Second)
	put("counter", now, 20)
	assertReady(true)
	for _, op := range []types.OperationOverTime{types.OpLastOne, types.OpRate, types.OpAvg, types.OpMin, types.OpMax, types.OpCount} {
		if _, found, err := store.Get("counter", nil, op, types.VecSum); err != nil || !found {
			t.Fatalf("warm %s: found=%t, %v", op, found, err)
		}
	}

	// Expressions load one snapshot per metric, then apply selector labels.
	// A cold series for another tenant must not poison an already-warm query.
	store.Put(types.NewMetricEntry{Name: "counter", Labels: types.Labels{"tenant": "late"}, MeasurementTime: pcommon.NewTimestampFromTime(now), MeasurementValue: 1})
	for _, query := range []string{`counter{tenant!="late"}`, `counter{tenant="late"}`} {
		compiled, err := NewParser().Parse(query)
		if err != nil {
			t.Fatal(err)
		}
		value, found, err := compiled.Evaluate(store, types.OpRate)
		if query == `counter{tenant!="late"}` {
			if err != nil || !found || value != 2 {
				t.Fatalf("unrelated warming series affected query: %v, %v", value, err)
			}
		} else if !errors.Is(err, ErrHistoryNotReady) {
			t.Fatalf("selected cold series was accepted: %v", err)
		}
	}
	put("new_series", now, 1)
	if _, _, err := store.Get("new_series", nil, types.OpAvg, types.VecSum); !errors.Is(err, ErrHistoryNotReady) {
		t.Fatalf("new series average: %v", err)
	}
	if _, _, err := store.Get("new_series", nil, types.OpLastOne, types.VecSum); err != nil {
		t.Fatal(err)
	}
	now = now.Add(5 * time.Second)
	put("new_series", now.Add(-5*time.Second), 1)
	if _, _, err := store.Get("new_series", nil, types.OpRate, types.VecSum); !errors.Is(err, ErrHistoryNotReady) {
		t.Fatalf("duplicate timestamps must not form a rate: %v", err)
	}
	put("new_series", now, 2)
	if _, _, err := store.Get("new_series", nil, types.OpRate, types.VecSum); err != nil {
		t.Fatal(err)
	}
	now = now.Add(11 * time.Second)
	assertReady(false)
	put("counter", now, 30)
	assertReady(false)
	if _, _, err := store.Get("counter", nil, types.OpAvg, types.VecSum); !errors.Is(err, ErrHistoryNotReady) {
		t.Fatalf("resumed series must warm again: %v", err)
	}
	now = now.Add(5 * time.Second)
	put("counter", now, 40)
	assertReady(true)
	if _, _, err := store.Get("counter", nil, types.OpRate, types.VecSum); err != nil {
		t.Fatal(err)
	}
	store.GetStore().Range(func(name string, _ *types.Map[types.LabelsHash, *types.MetricData]) bool {
		store.GetStore().Delete(name)
		return true
	})
	store.ResetReadiness()
	assertReady(false)
	put("counter", now, 50)
	assertReady(false)
	now = now.Add(5 * time.Second)
	put("counter", now, 60)
	assertReady(true)
	if removed := store.CollectStale(now.Add(2*time.Second), time.Second); removed == 0 {
		t.Fatal("expected GC to empty the store")
	}
	assertReady(false)
	defaults := NewMetricStore(&util.Config{HAEnabled: true, MetricStoreRetentionSeconds: 7}).(ms)
	if defaults.ha.warmup != 7*time.Second {
		t.Fatal("default warmup must follow retention")
	}
}
