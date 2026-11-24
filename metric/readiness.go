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

var errNoFreshMetricSamples = fmt.Errorf("%w: no fresh metric samples", ErrHistoryNotReady)

func (m ms) freshHASample(entry types.NewMetricEntry, now time.Time) bool {
	age := now.Unix() - int64(timestampToSeconds(entry.MeasurementTime))
	return age >= -5 && age <= int64(m.stalePeriodSeconds) && !math.IsNaN(entry.MeasurementValue) && !math.IsInf(entry.MeasurementValue, 0)
}

// haReadiness is protected by the store mutex. A replica warms independently
// from its first fresh write; producer timestamps determine ingestion freshness.
type haReadiness struct {
	warmup          time.Duration
	startupDeadline time.Time
	hasWarmed       bool
	firstWrite      time.Time
	firstSample     uint32
	latestSample    uint32
	now             func() time.Time
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

// Ready gates the Pod probe. The startup deadline can make a cold Pod ready.
func (m ms) Ready() error {
	return m.ready(true)
}

// ListenerReady gates KEDA connections. After the startup deadline the listener
// still needs fresh samples, so clients can reconnect if ingestion stops.
func (m ms) ListenerReady() error {
	return m.ready(false)
}

func (m ms) ready(allowEmpty bool) error {
	if m.ha == nil || m.ha.startupDeadline.IsZero() {
		return m.HistoryReady()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.ha.now()
	err := m.historyReadyLocked(now)
	if err == nil {
		m.ha.hasWarmed = true
	} else if !m.ha.hasWarmed && !now.Before(m.ha.startupDeadline) && (allowEmpty || m.hasFreshSamplesLocked(now)) {
		return nil
	}
	return err
}

// HistoryReady checks whether the replica has completed metric warmup.
func (m ms) HistoryReady() error {
	if m.ha == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.historyReadyLocked(m.ha.now())
}

// QueryReady checks whether the replica can serve the requested operation.
// GetSeries checks that each matching series has enough history.
func (m ms) QueryReady(op types.OperationOverTime) error {
	if m.ha == nil || (op != types.OpLastOne && op != types.OpRate) {
		return m.HistoryReady()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.hasFreshSamplesLocked(m.ha.now()) {
		return errNoFreshMetricSamples
	}
	return nil
}

func (m ms) hasFreshSamplesLocked(now time.Time) bool {
	return !m.ha.firstWrite.IsZero() && !m.isStale(m.ha.latestSample, now.Unix())
}

func (m ms) historyReadyLocked(now time.Time) error {
	if !m.hasFreshSamplesLocked(now) {
		return errNoFreshMetricSamples
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
	if op != types.OpRate && (md.FirstWrite.IsZero() || now.Sub(md.FirstWrite) < m.ha.warmup) {
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
