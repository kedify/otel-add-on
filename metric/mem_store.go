package metric

import (
	"crypto/sha256"
	"fmt"
	"math"
	"sort"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/kedify/otel-add-on/types"
	"github.com/kedify/otel-add-on/util"
)

type ms struct {
	store              map[types.MetricName]types.StoredMetrics
	stalePeriodSeconds int
}

func (m ms) Get(name types.MetricName, searchLabels types.Labels, timeOp types.OperationOverTime, defaultAggregation types.AggregationOverVectors) (float64, types.Found, error) {
	now := time.Now().Unix()
	if _, found := m.store[name]; !found {
		// not found
		return -1., false, nil
	}
	if err := util.CheckTimeOp(timeOp); err != nil {
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

func checkDefaultAggregation(aggregation types.AggregationOverVectors) error {
	switch aggregation {
	case types.VecSum, types.VecAvg, types.VecMin, types.VecMax:
		return nil
	default:
		return fmt.Errorf("unknown AggregationOverVectors:%s", aggregation)
	}
}

func (m ms) Put(entry types.NewMetricEntry) {
	if _, found := m.store[entry.Name]; !found {
		m.store[entry.Name] = make(map[types.LabelsHash]types.MetricData)
	}
	now := time.Now().Unix()
	labelsH := hashOfMap(entry.Labels)
	if _, found := m.store[entry.Name][labelsH]; !found {
		// new MetricData
		m.store[entry.Name][labelsH] = newMetricDatapoint(entry)
	} else {
		// found
		md := m.store[entry.Name][labelsH]
		notStale := util.Filter(md.Data, func(val types.ObservedValue) bool {
			return !m.isStale(val.Time, now)
		})
		fmt.Sprintf("not stale: %v", notStale)
		md.Data = append(notStale, types.ObservedValue{
			Time:  entry.Time,
			Value: entry.Value,
		})
		m.updateAggregatesOverTime(md)
		md.LastUpdate = entry.Time
		m.store[entry.Name][labelsH] = md
	}
}

func NewMetricStore(stalePeriodSeconds int) types.MemStore {
	return ms{
		store:              make(map[types.MetricName]types.StoredMetrics),
		stalePeriodSeconds: stalePeriodSeconds,
	}
}

func hashOfMap(m types.Labels) types.LabelsHash {
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
	return types.LabelsHash(fmt.Sprintf("%x", h.Sum(nil)))
}

func (m ms) isStale(datapoint pcommon.Timestamp, now int64) bool {
	return now-int64(m.stalePeriodSeconds) > int64(datapoint)
}

func (m ms) calculateAggregate(value float64, counter int, accumulator float64, aggregation types.AggregationOverVectors) float64 {
	if counter == 1 {
		return value
	}
	switch aggregation {
	case types.VecSum:
		return accumulator + value
	case types.VecAvg:
		// calculate the avg on the fly to avoid potential overflows,
		// idea: each number adds 1/count of itself to the final result
		c := float64(counter)
		cMinusOne := float64(counter - 1)
		return ((accumulator / c) * cMinusOne) + (value / c)
	case types.VecMin:
		return math.Min(accumulator, value)
	case types.VecMax:
		return math.Max(accumulator, value)
	default:
		panic("unknown aggregation function: " + aggregation)
	}
}

func newMetricDatapoint(entry types.NewMetricEntry) types.MetricData {
	return types.MetricData{
		Labels:     entry.Labels,
		LastUpdate: entry.Time,
		Data: []types.ObservedValue{
			{
				Time:  entry.Time,
				Value: entry.Value,
			},
		},
		AggregatesOverTime: map[types.OperationOverTime]float64{
			types.OpMin:     entry.Value,
			types.OpMax:     entry.Value,
			types.OpAvg:     entry.Value,
			types.OpLastOne: entry.Value,
			types.OpCount:   1,
			types.OpRate:    0,
		},
	}
}

func (m ms) updateAggregatesOverTime(md types.MetricData) {
	for i := 0; i < len(md.Data); i++ {
		for _, op := range []types.OperationOverTime{types.OpMin, types.OpMax, types.OpAvg} {
			md.AggregatesOverTime[op] = m.calculateAggregate(md.Data[i].Value, i+1, md.AggregatesOverTime[op], types.AggregationOverVectors(op))
		}
	}
	md.AggregatesOverTime[types.OpRate] = (md.Data[len(md.Data)-1].Value - md.Data[0].Value) / float64(md.Data[len(md.Data)-1].Time-md.Data[0].Time)
	md.AggregatesOverTime[types.OpCount] = float64(len(md.Data))
	md.AggregatesOverTime[types.OpLastOne] = md.Data[len(md.Data)-1].Value
}

// enforce iface impl
var _ types.MemStore = new(ms)
