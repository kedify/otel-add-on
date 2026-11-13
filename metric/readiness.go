package metric

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/kedify/otel-add-on/types"
)

// ErrHistoryNotReady marks unavailable history so callers can return a retryable
// error rather than interpreting a replica's warmup as zero demand.
var ErrHistoryNotReady = errors.New("HA metric history is not ready")

func (m ms) freshHASample(entry types.NewMetricEntry, now time.Time) bool {
	age := now.Unix() - int64(timestampToSeconds(entry.MeasurementTime))
	return age >= -5 && age <= int64(m.stalePeriodSeconds) && !math.IsNaN(entry.MeasurementValue) && !math.IsInf(entry.MeasurementValue, 0)
}

// haReadiness is protected by the store mutex. A replica warms independently
// from its first fresh write; producer timestamps determine ingestion freshness.
type haReadiness struct {
	warmup       time.Duration
	firstWrite   time.Time
	firstSample  uint32
	latestSample uint32
	now          func() time.Time
}

func (m ms) observeHA(entry types.NewMetricEntry, now time.Time) {
	sample := timestampToSeconds(entry.MeasurementTime)
	if m.ha.firstWrite.IsZero() || now.Unix()-int64(m.ha.latestSample) > int64(m.stalePeriodSeconds) {
		m.ha.firstWrite = now
		m.ha.firstSample = sample
	}
	if sample > m.ha.latestSample {
		m.ha.latestSample = sample
	}
}

// ResetReadiness starts a new warmup after the diagnostic API clears the store.
// Concurrent writes before this notification are handled conservatively: the
// next fresh sample starts the new replica-wide warmup period.
func (m ms) ResetReadiness() {
	if m.ha == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resetHALocked()
}

func (m ms) resetHALocked() {
	m.ha.firstWrite = time.Time{}
	m.ha.firstSample = 0
	m.ha.latestSample = 0
}

func (m ms) resetHAIfEmpty() {
	hasData := false
	m.store.Range(func(_ string, series *types.Map[types.LabelsHash, *types.MetricData]) bool {
		series.Range(func(types.LabelsHash, *types.MetricData) bool { hasData = true; return false })
		return !hasData
	})
	if !hasData {
		m.resetHALocked()
	}
}

// Ready gates HA query discovery. Ingestion and liveness remain independent.
// This is a conservative replica-wide warmup; GetSeries also checks the history
// of each matching series, including series that arrive after the Pod is ready.
func (m ms) Ready() error {
	if m.ha == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := m.ha.now()
	if m.ha.firstWrite.IsZero() || now.Unix()-int64(m.ha.latestSample) > int64(m.stalePeriodSeconds) {
		return fmt.Errorf("%w: no fresh metric samples", ErrHistoryNotReady)
	}
	if now.Sub(m.ha.firstWrite) < m.ha.warmup {
		return fmt.Errorf("%w: replica is warming up", ErrHistoryNotReady)
	}
	if m.ha.latestSample <= m.ha.firstSample {
		return fmt.Errorf("%w: need samples at distinct timestamps", ErrHistoryNotReady)
	}
	return nil
}

func (m ms) checkHistory(md *types.MetricData, op types.OperationOverTime, now time.Time) error {
	if op == types.OpLastOne {
		return nil
	}
	if md.FirstWrite.IsZero() || now.Sub(md.FirstWrite) < m.ha.warmup {
		return fmt.Errorf("%w: series is warming up for %s", ErrHistoryNotReady, op)
	}
	var first uint32
	for _, sample := range md.Data {
		age := now.Unix() - int64(sample.Time)
		if age < -5 || age > int64(m.stalePeriodSeconds) {
			continue
		}
		if first != 0 && sample.Time != first {
			return nil
		}
		first = sample.Time
	}
	return fmt.Errorf("%w: series needs samples at distinct timestamps for %s", ErrHistoryNotReady, op)
}
