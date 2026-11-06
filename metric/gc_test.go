package metric

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/kedify/otel-add-on/types"
	"github.com/kedify/otel-add-on/util"
)

func scrapeInternalMetrics(t *testing.T, metrics *InternalMetrics) string {
	t.Helper()
	registry := prometheus.NewRegistry()
	for _, collector := range metrics.registry() {
		registry.MustRegister(collector)
	}
	response := httptest.NewRecorder()
	promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
	if response.Code != 200 {
		t.Fatalf("scrape failed: %s", response.Body)
	}
	return response.Body.String()
}

func TestGCRemovesOnlyIdleExportedRecordsAndCanRecreateThem(t *testing.T) {
	metrics := &InternalMetrics{}
	metrics.Init()
	metrics.SetMetricValue("queue", "stale_query", "last_one", "expression", 1)
	metrics.SetMetricValue("queue", "shared_query", "last_one", "expression", 2)
	metrics.SetMetricValueClamped("queue", "shared_query", "last_one", "expression", "deleted-so", "test", 3)
	metrics.SetMetricValueClamped("queue", "shared_query", "last_one", "expression", "active-so", "test", 4)
	metrics.IncMetricRead("queue")
	metrics.IncMetricWrite("queue")
	metrics.SetRuntimeInfo(&util.Config{})
	now := time.Now()
	for labels := range metrics.valueUpdates {
		if labels.labels == "stale_query" || labels.scaledObject == "deleted-so" {
			metrics.valueUpdates[labels] = now.Add(-2 * time.Minute)
		} else {
			metrics.valueUpdates[labels] = now
		}
	}
	// Scraping is not query activity and must not extend the idle deadline.
	scrapeInternalMetrics(t, metrics)
	if removed := metrics.CollectStale(now, time.Minute); removed != 2 {
		t.Fatalf("removed %d records, want 2", removed)
	}
	body := scrapeInternalMetrics(t, metrics)
	for _, stale := range []string{`labels="stale_query"`, `scaledObject="deleted-so"`} {
		if strings.Contains(body, stale) {
			t.Errorf("stale record still exported: %s", stale)
		}
	}
	for _, active := range []string{`labels="shared_query"`, `scaledObject="active-so"`, KedaOtelScalerRuntimeInfo, KedaOtelScalerMetricPointsRead, KedaOtelScalerMetricPointsWritten} {
		if !strings.Contains(body, active) {
			t.Errorf("active/unrelated record removed: %s", active)
		}
	}
	if len(metrics.valueUpdates) != 2 {
		t.Fatal("expired activity metadata was retained")
	}
	// A new query after GC must re-register its gauge, not update a detached child.
	metrics.SetMetricValue("queue", "stale_query", "last_one", "expression", 9)
	metrics.SetMetricValueClamped("queue", "shared_query", "last_one", "expression", "deleted-so", "test", 10)
	body = scrapeInternalMetrics(t, metrics)
	if !strings.Contains(body, `labels="stale_query"`) || !strings.Contains(body, `scaledObject="deleted-so"`) {
		t.Fatal("query records were not recreated after expiry")
	}
}

func TestGCQueryRefreshAndExactIdleBoundary(t *testing.T) {
	metrics := &InternalMetrics{}
	metrics.Init()
	metrics.SetMetricValue("queue", "query", "last_one", "expression", 1)
	var labels metricValueLabels
	for key := range metrics.valueUpdates {
		labels = key
	}
	metrics.valueUpdates[labels] = time.Now().Add(-time.Hour)
	metrics.SetMetricValue("queue", "query", "last_one", "expression", 2)
	updated := metrics.valueUpdates[labels]
	if got := metrics.CollectStale(updated.Add(time.Minute), time.Minute); got != 0 {
		t.Fatal("record expired at the exact idle boundary")
	}
	if got := metrics.CollectStale(updated.Add(time.Minute+time.Nanosecond), time.Minute); got != 1 {
		t.Fatal("record did not expire after the idle boundary")
	}
}

func TestGCQueryAttemptsRefreshOnlyTheirExistingRecords(t *testing.T) {
	metrics := &InternalMetrics{}
	metrics.Init()
	metrics.SetMetricValue("queue", "query", "last_one", "expression", 1)
	metrics.SetMetricValueClamped("queue", "query", "last_one", "expression", "active-so", "test", 2)
	metrics.SetMetricValueClamped("queue", "query", "last_one", "expression", "deleted-so", "test", 3)
	for labels := range metrics.valueUpdates {
		metrics.valueUpdates[labels] = time.Now().Add(-time.Hour)
	}
	// A REST attempt refreshes the shared raw value, not any SO's clamped value.
	metrics.TouchMetricValues("queue", "query", "last_one", "expression", "", "")
	for labels, updated := range metrics.valueUpdates {
		if labels.clamped && time.Since(updated) < time.Minute {
			t.Fatal("REST activity refreshed a ScaledObject-specific record")
		}
	}
	// A KEDA attempt refreshes only the matching SO, even with no new result.
	metrics.TouchMetricValues("queue", "query", "last_one", "expression", "active-so", "test")
	metrics.TouchMetricValues("missing", "new_query", "last_one", "expression", "new-so", "test")
	if len(metrics.valueUpdates) != 3 {
		t.Fatal("a query attempt created a gauge without a result")
	}
	if removed := metrics.CollectStale(time.Now(), time.Minute); removed != 1 {
		t.Fatalf("removed %d records, want only the deleted SO", removed)
	}
}

func putGCSeries(store types.MemStore, name string, id string, sampleTime time.Time) {
	store.Put(types.NewMetricEntry{Name: types.MetricName(name), Labels: types.Labels{"id": id},
		MeasurementTime: pcommon.Timestamp(sampleTime.Unix()), MeasurementValue: 42})
}

func TestGCRemovesUnreadSeriesAndEmptyFamilies(t *testing.T) {
	store := NewMetricStore(&util.Config{MetricStoreRetentionSeconds: 3600}).(ms)
	now := time.Now()
	putGCSeries(store, "unread", "gone", now)
	putGCSeries(store, "mixed", "gone", now)
	putGCSeries(store, "mixed", "live", now)
	store.store.Range(func(_ string, series *types.Map[types.LabelsHash, *types.MetricData]) bool {
		series.Range(func(_ types.LabelsHash, md *types.MetricData) bool {
			if md.Labels["id"] == "gone" {
				md.LastWrite = now.Add(-2 * time.Minute)
			}
			return true
		})
		return true
	})
	// Even reads of the orphaned series must not turn into writes/extend its TTL.
	if _, err := store.GetSeries("mixed", nil, types.OpLastOne); err != nil {
		t.Fatal(err)
	}
	if removed := store.CollectStale(now, time.Minute); removed != 2 {
		t.Fatalf("removed %d series, want 2", removed)
	}
	if _, found := store.store.Load("unread"); found {
		t.Fatal("empty, unread metric family was retained")
	}
	series, err := store.GetSeries("mixed", nil, types.OpLastOne)
	if err != nil || len(series) != 1 || series[0].Labels["id"] != "live" {
		t.Fatalf("active series not preserved: %v, %v", series, err)
	}
}

func TestGCUsesReceiptTimeAndRefreshesOnWrite(t *testing.T) {
	store := NewMetricStore(&util.Config{MetricStoreRetentionSeconds: 120}).(ms)
	now := time.Now()
	// Future producer timestamps cannot keep abandoned series alive forever;
	// recently received old timestamps cannot cause premature idle collection.
	putGCSeries(store, "old_sample", "old", now.Add(-time.Hour))
	putGCSeries(store, "future_sample", "future", now.Add(time.Hour))
	if removed := store.CollectStale(now, time.Minute); removed != 0 {
		t.Fatal("fresh writes were considered idle based on producer timestamps")
	}
	if removed := store.CollectStale(now.Add(2*time.Minute), time.Minute); removed != 2 {
		t.Fatal("future-dated producer data did not expire after writes stopped")
	}
	putGCSeries(store, "refreshed", "live", now)
	family, _ := store.store.Load("refreshed")
	key := hashOfMap(types.Labels{"id": "live"})
	previous, _ := family.Load(key)
	previous.LastWrite = now.Add(-time.Hour)
	putGCSeries(store, "refreshed", "live", now)
	current, _ := family.Load(key)
	if !previous.LastWrite.Before(current.LastWrite) {
		t.Fatal("write did not refresh the idle timestamp")
	}
	if got := store.CollectStale(current.LastWrite.Add(time.Minute), time.Minute); got != 0 {
		t.Fatal("series expired at the exact idle boundary")
	}
	if got := store.CollectStale(current.LastWrite.Add(time.Minute+time.Nanosecond), time.Minute); got != 1 {
		t.Fatal("series did not expire after the idle boundary")
	}
}

func TestBackgroundGCCollectsWithoutRequestsAndStops(t *testing.T) {
	store := NewMetricStore(&util.Config{MetricStoreRetentionSeconds: 120})
	metrics := &InternalMetrics{}
	metrics.Init()
	putGCSeries(store, "unread", "gone", time.Now())
	metrics.SetMetricValue("queue", "query", "last_one", "expression", 42)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		RunGarbageCollector(ctx, store, metrics, 5*time.Millisecond)
	}()
	deadline := time.After(5 * time.Second)
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case <-deadline:
			t.Fatal("background GC did not remove idle metrics without reads")
		case <-poll.C:
			_, found := store.GetStore().Load("unread")
			if !found && !strings.Contains(scrapeInternalMetrics(t, metrics), KedaOtelScalerValue+"{") {
				cancel()
				select {
				case <-done:
					return
				case <-time.After(time.Second):
					t.Fatal("GC did not stop after cancellation")
				}
			}
		}
	}
}

func TestGCDisabledAndConcurrentUpdates(t *testing.T) {
	metrics := &InternalMetrics{}
	metrics.Init()
	store := NewMetricStore(&util.Config{MetricStoreRetentionSeconds: 120})
	putGCSeries(store, "queue", "live", time.Now())
	metrics.SetMetricValue("queue", "query", "last_one", "expression", 1)
	if store.CollectStale(time.Now().Add(time.Hour), 0) != 0 || metrics.CollectStale(time.Now().Add(time.Hour), 0) != 0 {
		t.Fatal("zero idle timeout did not disable cleanup")
	}
	RunGarbageCollector(context.Background(), store, metrics, 0)
	var wg sync.WaitGroup
	for worker := range 4 {
		wg.Go(func() {
			for range 100 {
				if worker%2 == 0 {
					putGCSeries(store, "queue", "live", time.Now())
					metrics.SetMetricValue("queue", "query", "last_one", "expression", 42)
					metrics.SetMetricValueClamped("queue", "query", "last_one", "expression", "so", "test", 42)
				} else {
					store.CollectStale(time.Now().Add(time.Hour), time.Minute)
					metrics.CollectStale(time.Now().Add(time.Hour), time.Minute)
					if _, err := store.GetSeries("queue", nil, types.OpLastOne); err != nil {
						t.Error(err)
					}
				}
			}
		})
	}
	wg.Wait()
	putGCSeries(store, "queue", "live", time.Now())
	metrics.SetMetricValue("queue", "query", "last_one", "expression", 42)
	metrics.SetMetricValueClamped("queue", "query", "last_one", "expression", "so", "test", 42)
	if series, err := store.GetSeries("queue", nil, types.OpLastOne); err != nil || len(series) != 1 {
		t.Fatalf("final write was lost: %v, %v", series, err)
	}
	body := scrapeInternalMetrics(t, metrics)
	if !strings.Contains(body, KedaOtelScalerValue+"{") || !strings.Contains(body, KedaOtelScalerValueClamped+"{") {
		t.Fatal("final query updated a detached gauge")
	}
}
