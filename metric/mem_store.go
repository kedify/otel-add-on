package metric

import (
	"crypto/sha256"
	"fmt"
	"math"
	"sort"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/kedify/otel-add-on/util"
)

type ms struct {
	store              map[MetricName]StoredMetrics
	stalePeriodSeconds int
}

func (m ms) Get(name MetricName, searchLabels Labels, timeOp OperationOverTime, defaultAggregation AggregationOverVectors) (float64, Found, error) {
	now := time.Now().Unix()
	if _, found := m.store[name]; !found {
		// not found
		return -1., false, nil
	}
	if err := checkTimeOp(timeOp); err != nil {
		return -1., false, err
	}
	if err := checkDefaultAggregation(defaultAggregation); err != nil {
		return -1., false, err
	}
	storedMetrics := m.store[name]
	if md, found := storedMetrics[hashOfMap(searchLabels)]; found {
		// found exact label match
		if !m.isStale(md.LastUpdate, now) {
			ret, found := md.AggregatesOverTime[timeOp]
			if !found {
				return -1., false, fmt.Errorf("unknown OperationOverTime: %s", timeOp)
			}
			return ret, true, nil
		}
		return md.AggregatesOverTime[timeOp], true, nil
	}
	// multiple metric vectors match the search criteria
	var accumulator float64
	counter := 0
	for _, md := range storedMetrics {
		match := true
		for searchLabelName, searchLabelVal := range searchLabels {
			if v, found := md.Labels[searchLabelName]; found && v != searchLabelVal {
				match = false
				break
			}
		}
		if match {
			if !m.isStale(md.LastUpdate, now) {
				val, found := md.AggregatesOverTime[timeOp]
				if !found {
					return -1., false, fmt.Errorf("unknown OperationOverTime: %s", timeOp)
				}
				counter += 1
				accumulator = m.calculateAggregate(val, counter, accumulator, defaultAggregation)
			}
		}
	}
	return accumulator, true, nil
}

func checkDefaultAggregation(aggregation AggregationOverVectors) error {
	switch aggregation {
	case VecSum, VecAvg, VecMin, VecMax:
		return nil
	default:
		return fmt.Errorf("unknown AggregationOverVectors:%s", aggregation)
	}
}

func checkTimeOp(op OperationOverTime) error {
	switch op {
	case OpLastOne, OpRate, OpCount, OpAvg, OpMin, OpMax:
		return nil
	default:
		return fmt.Errorf("unknown OperationOverTime:%s", op)
	}
}

func (m ms) Put(entry NewMetricEntry) {
	if _, found := m.store[entry.Name]; !found {
		m.store[entry.Name] = make(map[LabelsHash]MetricData)
	}
	now := time.Now().Unix()
	labelsH := hashOfMap(entry.Labels)
	if _, found := m.store[entry.Name][labelsH]; !found {
		// new MetricData
		m.store[entry.Name][labelsH] = newMetricDatapoint(entry)
	} else {
		// found
		md := m.store[entry.Name][labelsH]
		notStale := util.Filter(md.Data, func(val ObservedValue) bool {
			return !m.isStale(val.Time, now)
		})
		fmt.Sprintf("not stale: %v", notStale)
		md.Data = append(notStale, ObservedValue{
			Time:  entry.Time,
			Value: entry.Value,
		})
		m.updateAggregatesOverTime(md)
		md.LastUpdate = entry.Time
		m.store[entry.Name][labelsH] = md
	}
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

func (m ms) isStale(datapoint pcommon.Timestamp, now int64) bool {
	return now-int64(m.stalePeriodSeconds) > int64(datapoint)
}

func (m ms) calculateAggregate(value float64, counter int, accumulator float64, aggregation AggregationOverVectors) float64 {
	if counter == 1 {
		return value
	}
	switch aggregation {
	case VecSum:
		return accumulator + value
	case VecAvg:
		// calculate the avg on the fly to avoid potential overflows,
		// idea: each number adds 1/count of itself to the final result
		c := float64(counter)
		cMinusOne := float64(counter - 1)
		return ((accumulator / c) * cMinusOne) + (value / c)
	case VecMin:
		return math.Min(accumulator, value)
	case VecMax:
		return math.Max(accumulator, value)
	default:
		panic("unknown aggregation function: " + aggregation)
	}
}

func newMetricDatapoint(entry NewMetricEntry) MetricData {
	return MetricData{
		Labels:     entry.Labels,
		LastUpdate: entry.Time,
		Data: []ObservedValue{
			{
				Time:  entry.Time,
				Value: entry.Value,
			},
		},
		AggregatesOverTime: map[OperationOverTime]float64{
			OpMin:     entry.Value,
			OpMax:     entry.Value,
			OpAvg:     entry.Value,
			OpLastOne: entry.Value,
			OpCount:   1,
			OpRate:    0,
		},
	}
}

func (m ms) updateAggregatesOverTime(md MetricData) {
	for i := 0; i < len(md.Data); i++ {
		for _, op := range []OperationOverTime{OpMin, OpMax, OpAvg} {
			md.AggregatesOverTime[op] = m.calculateAggregate(md.Data[i].Value, i+1, md.AggregatesOverTime[op], AggregationOverVectors(op))
		}
	}
	md.AggregatesOverTime[OpRate] = (md.Data[len(md.Data)-1].Value - md.Data[0].Value) / float64(md.Data[len(md.Data)-1].Time-md.Data[0].Time)
	md.AggregatesOverTime[OpCount] = float64(len(md.Data))
	md.AggregatesOverTime[OpLastOne] = md.Data[len(md.Data)-1].Value
}

// enforce iface impl
var _ MemStore = new(ms)
