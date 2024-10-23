package metric

import (
	"crypto/sha256"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/kedify/otel-add-on/types"
	"github.com/kedify/otel-add-on/util"
)

type ms struct {
	store              *types.Map[types.MetricName, *types.Map[types.LabelsHash, *types.MetricData]]
	stalePeriodSeconds int
	metricsExporter    *InternalMetrics
}

func NewMetricStore(stalePeriodSeconds int) types.MemStore {
	m := Metrics()
	m.Init()
	return ms{
		store:              &types.Map[types.MetricName, *types.Map[types.LabelsHash, *types.MetricData]]{},
		stalePeriodSeconds: stalePeriodSeconds,
		metricsExporter:    m,
	}
}

func (m ms) Get(unescapedName types.MetricName, searchLabels types.Labels, timeOp types.OperationOverTime, defaultAggregation types.AggregationOverVectors) (float64, types.Found, error) {
	name := escapeName(unescapedName)
	value, found, err := m.get(name, searchLabels, timeOp, defaultAggregation)
	if err == nil {
		m.metricsExporter.IncMetricRead(string(name))
		m.metricsExporter.SetMetricValue(string(name), fmt.Sprint(searchLabels), string(timeOp), string(defaultAggregation), value)
	}
	return value, found, err
}

func (m ms) get(name types.MetricName, searchLabels types.Labels, timeOp types.OperationOverTime, defaultAggregation types.AggregationOverVectors) (float64, types.Found, error) {
	now := time.Now().Unix()
	if err := util.CheckTimeOp(timeOp); err != nil {
		return -1., false, err
	}
	if err := checkDefaultAggregation(defaultAggregation); err != nil {
		return -1., false, err
	}
	storedMetrics, found := m.store.Load(name)
	if !found {
		// not found
		return -1., false, nil
	}
	if md, f := storedMetrics.Load(hashOfMap(searchLabels)); f {
		// found exact label match
		if !m.isStale(md.LastUpdate, now) {
			ret, f := md.AggregatesOverTime.Load(timeOp)
			if !f {
				return -1., false, fmt.Errorf("unknown OperationOverTime: %s", timeOp)
			}
			return ret, true, nil
		}
		v, _ := md.AggregatesOverTime.Load(timeOp)
		return v, true, nil
	}
	// multiple metric vectors match the search criteria
	var accumulator float64
	counter := 0
	storedMetrics.Range(func(_ types.LabelsHash, md *types.MetricData) bool {
		match := true
		for searchLabelName, searchLabelVal := range searchLabels {
			if v, found := md.Labels[searchLabelName]; found && v != searchLabelVal {
				match = false
				break
			}
		}
		if match {
			if !m.isStale(md.LastUpdate, now) {
				val, found := md.AggregatesOverTime.Load(timeOp)
				if !found {
					return true
				}
				counter += 1
				accumulator = m.calculateAggregate(val, counter, accumulator, defaultAggregation)
			}
		}
		return true
	})
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
	name := escapeName(entry.Name)
	m.metricsExporter.IncMetricWrite(string(name))
	now := time.Now().Unix()
	labelsH := hashOfMap(entry.Labels)
	metrics, _ := m.store.LoadOrStore(name, &types.Map[types.LabelsHash, *types.MetricData]{})
	md, found := metrics.LoadOrStore(labelsH, newMetricDatapoint(entry))
	if found {
		notStale := util.Filter(md.Data, func(val types.ObservedValue) bool {
			return !m.isStale(val.Time, now)
		})
		timeInSeconds := timestampToSeconds(entry.MeasurementTime)
		md.Data = append(notStale, types.ObservedValue{
			Time:  timeInSeconds,
			Value: entry.MeasurementValue,
		})
		m.updateAggregatesOverTime(md)
		md.LastUpdate = timeInSeconds
	}
	metrics.Store(labelsH, md)
	m.store.Store(name, metrics)
}

func escapeName(name types.MetricName) types.MetricName {
	return types.MetricName(strings.ReplaceAll(string(name), "/", "_"))
}

func timestampToSeconds(timestamp pcommon.Timestamp) uint32 {
	if timestamp > 1729508567000000000 { // nanos -> seconds
		return uint32(timestamp / 1e9)
	}
	if timestamp > 1729508567000000 { // micros -> seconds
		return uint32(timestamp / 1e6)
	}
	if timestamp > 1729508567000 { // millis -> seconds
		return uint32(timestamp / 1e3)
	}
	return uint32(timestamp)
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

func (m ms) isStale(datapoint uint32, now int64) bool {
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

func newMetricDatapoint(entry types.NewMetricEntry) *types.MetricData {
	timeInSeconds := timestampToSeconds(entry.MeasurementTime)
	md := types.MetricData{
		Labels:     entry.Labels,
		LastUpdate: timeInSeconds,
		Data: []types.ObservedValue{
			{
				Time:  timeInSeconds,
				Value: entry.MeasurementValue,
			},
		},
		AggregatesOverTime: types.Map[types.OperationOverTime, float64]{},
	}
	md.AggregatesOverTime.Store(types.OpMin, entry.MeasurementValue)
	md.AggregatesOverTime.Store(types.OpMax, entry.MeasurementValue)
	md.AggregatesOverTime.Store(types.OpAvg, entry.MeasurementValue)
	md.AggregatesOverTime.Store(types.OpLastOne, entry.MeasurementValue)
	md.AggregatesOverTime.Store(types.OpCount, 1)
	md.AggregatesOverTime.Store(types.OpRate, 0)
	return &md
}

func (m ms) updateAggregatesOverTime(md *types.MetricData) {
	for _, op := range []types.OperationOverTime{types.OpMin, types.OpMax, types.OpAvg} {
		acc, _ := md.AggregatesOverTime.Load(op)
		for i := 0; i < len(md.Data); i++ {
			acc = m.calculateAggregate(md.Data[i].Value, i+1, acc, types.AggregationOverVectors(op))
		}
		md.AggregatesOverTime.Store(op, acc)
	}
	valuesDelta := md.Data[len(md.Data)-1].Value - md.Data[0].Value
	timeDelta := float64(md.Data[len(md.Data)-1].Time - md.Data[0].Time)

	md.AggregatesOverTime.Store(types.OpRate, valuesDelta/timeDelta)
	md.AggregatesOverTime.Store(types.OpCount, float64(len(md.Data)))
	md.AggregatesOverTime.Store(types.OpLastOne, md.Data[len(md.Data)-1].Value)
}

// enforce iface impl
var _ types.MemStore = new(ms)
