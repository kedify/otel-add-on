package metric

import (
	"crypto/sha256"
	"fmt"
	"math"
	"sort"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
)

type ms struct {
	store              map[MetricName]StoredMetrics
	stalePeriodSeconds int
}

func (m ms) Get(name MetricName, searchLabels Labels, aggregation Aggregation) (float64, Stale, Found) {
	now := time.Now().Unix()
	if _, found := m.store[name]; !found {
		// not found
		return .0, false, false
	}
	storedMetrics := m.store[name]
	if d, found := storedMetrics[hashOfMap(searchLabels)]; found {
		// found exact label match
		return d.Value, m.isStale(d.LastUpdate, now), true
	}
	// multiple metric points match the search criteria
	var accumulator float64
	counter := 0
	for _, value := range storedMetrics {
		// skip expired data points
		if m.isStale(value.LastUpdate, now) {
			//continue
		}
		// all specified labels must match
		match := true
		for searchLabelName, searchLabelVal := range searchLabels {
			if v, found := value.Labels[searchLabelName]; found && v != searchLabelVal {
				match = false
				break
			}
		}
		if match {
			counter += 1
			accumulator = m.calculateAggregate(value.Value, counter, accumulator, aggregation)
		}
	}
	return accumulator, false, counter > 0
}

func (m ms) Put(entry MetricEntry) {
	if _, found := m.store[entry.Name]; !found {
		m.store[entry.Name] = make(map[LabelsHash]ObservedValue)
	}
	m.store[entry.Name][hashOfMap(entry.Labels)] = ObservedValue{
		Value:      entry.Value,
		LastUpdate: entry.LastUpdate,
		Labels:     entry.Labels,
	}
}

func (m ms) Gc() {
	//TODO implement me
	panic("implement me")
}

func NewMetricStore(stalePeriodSeconds int) MemStore {
	return ms{
		store:              make(map[MetricName]StoredMetrics),
		stalePeriodSeconds: stalePeriodSeconds,
	}
}

func hashOfMap(m Labels) LabelsHash {
	h := sha256.New()
	keys := make([]string, len(m))
	i := 0
	for k := range m {
		keys[i] = k
		i++
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := m[k]
		b := sha256.Sum256([]byte(fmt.Sprintf("%v", k)))
		h.Write(b[:])
		b = sha256.Sum256([]byte(fmt.Sprintf("%v", v)))
		h.Write(b[:])
	}
	return LabelsHash(fmt.Sprintf("%x", h.Sum(nil)))
}

func (m ms) isStale(datapoint pcommon.Timestamp, now int64) Stale {
	return now-int64(m.stalePeriodSeconds) > int64(datapoint)
}

func (m ms) calculateAggregate(value float64, counter int, accumulator float64, aggregation Aggregation) float64 {
	if counter == 1 {
		return value
	}
	switch aggregation {
	case Sum:
		return accumulator + value
	case Avg:
		// calculate the avg on the fly to avoid potential overflows,
		// idea: each number adds 1/count of itself to the final result
		c := float64(counter)
		cMinusOne := float64(counter - 1)
		return ((accumulator / c) * cMinusOne) + (value / c)
	case Min:
		return math.Min(accumulator, value)
	case Max:
		return math.Max(accumulator, value)
	default:
		panic("unknown aggregation function: " + aggregation)
	}
}

// enforce iface impl
var _ MemStore = new(ms)
