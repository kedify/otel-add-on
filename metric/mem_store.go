package metric

import (
	"crypto/sha256"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"

	t "github.com/kedify/otel-add-on/types"
	"github.com/kedify/otel-add-on/util"
)

type ms struct {
	store              *t.Map[string, *t.Map[t.LabelsHash, *t.MetricData]]
	stalePeriodSeconds int
	metricsExporter    *InternalMetrics
	lazySeries         bool
	lazyAggregates     bool
	subscriptions      map[t.MetricName][]t.OperationOverTime
}

func NewMetricStore(stalePeriodSeconds int, lazySeries, lazyAggregates bool) t.MemStore {
	m := Metrics()
	m.Init()
	return ms{
		store:              &t.Map[string, *t.Map[t.LabelsHash, *t.MetricData]]{},
		stalePeriodSeconds: stalePeriodSeconds,
		metricsExporter:    m,
		lazySeries:         lazySeries,
		lazyAggregates:     lazyAggregates,
		subscriptions:      map[t.MetricName][]t.OperationOverTime{},
	}
}

// Get returns the float value from the metric store after applying all the search criteria
func (m ms) Get(unescapedName t.MetricName, searchLabels t.Labels, timeOp t.OperationOverTime, defaultAggregation t.AggregationOverVectors) (float64, t.Found, error) {
	name := escapeName(unescapedName)
	value, found, err := m.get(name, searchLabels, timeOp, defaultAggregation)
	if err == nil {
		m.metricsExporter.IncMetricRead(string(name))
		m.metricsExporter.SetMetricValue(string(name), fmt.Sprint(searchLabels), string(timeOp), string(defaultAggregation), value)
	}
	return value, found, err
}

func (m ms) get(name t.MetricName, searchLabels t.Labels, timeOp t.OperationOverTime, vecOp t.AggregationOverVectors) (float64, t.Found, error) {
	now := time.Now().Unix()
	if err := util.CheckTimeOp(timeOp); err != nil {
		return -1., false, err
	}
	if err := checkVectorAggregation(vecOp); err != nil {
		return -1., false, err
	}
	if m.lazySeries || m.lazyAggregates {
		if firstTime := subscribe(m.subscriptions, m.lazyAggregates, name, timeOp); firstTime {
			return -1., false, nil
		}
	}
	storedMetrics, foundMetric := m.store.Load(string(name))
	if !foundMetric {
		// not found
		return -1., false, nil
	}
	if md, f := storedMetrics.Load(hashOfMap(searchLabels)); f {
		// found exact label match
		if vecOp == t.VecCount {
			return 1, true, nil
		}
		if !m.isStale(md.LastUpdate, now) {
			ret, f := md.AggregatesOverTime.Load(timeOp)
			if !f {
				return -1., false, fmt.Errorf("unknown OperationOverTime: %s", timeOp)
			}
			return ret, true, nil
		} else {
			defer func() {
				storedMetrics.Delete(hashOfMap(searchLabels))
			}()
		}
		v, _ := md.AggregatesOverTime.Load(timeOp)
		return v, true, nil
	}
	// multiple metric vectors match the search criteria
	var accumulator float64
	counter := 0
	storedMetrics.Range(func(_ t.LabelsHash, md *t.MetricData) bool {
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
				accumulator = m.calculateAggregate(val, counter, accumulator, vecOp)
			} else {
				defer func() {
					storedMetrics.Delete(hashOfMap(searchLabels))
				}()
			}
		}
		return true
	})
	return accumulator, true, nil
}

// Put stores the t.NewMetricEntry in the metric store and updates the stored aggregates
func (m ms) Put(entry t.NewMetricEntry) {
	name := escapeName(entry.Name)
	if (m.lazySeries || m.lazyAggregates) && len(m.subscriptions[name]) == 0 {
		// nobody has been listening yet
		return
	}
	metrics, _ := m.store.LoadOrStore(string(name), &t.Map[t.LabelsHash, *t.MetricData]{})
	labelsH := hashOfMap(entry.Labels)
	md, found := metrics.LoadOrStore(labelsH, newFirstMetricDatapoint(entry, m.lazyAggregates))
	m.metricsExporter.IncMetricWrite(string(name))
	now := time.Now().Unix()
	if found {
		notStale := util.Filter(md.Data, func(val t.ObservedValue) bool {
			return !m.isStale(val.Time, now)
		})
		timeInSeconds := timestampToSeconds(entry.MeasurementTime)
		md.Data = append(notStale, t.ObservedValue{
			Time:  timeInSeconds,
			Value: entry.MeasurementValue,
		})
		m.updateAggregatesOverTime(md, name)
		md.LastUpdate = timeInSeconds
	}
	metrics.Store(labelsH, md)
	m.store.Store(string(name), metrics)
}

func (m ms) IsSubscribed(lazyAggregates bool, name t.MetricName, overTime t.OperationOverTime) bool {
	ops, found := m.subscriptions[name]
	if !lazyAggregates {
		return found
	} else {
		return slices.Contains(ops, overTime)
	}
}

func escapeName(name t.MetricName) t.MetricName {
	return t.MetricName(strings.ReplaceAll(strings.ReplaceAll(string(name), "/", "_"), ".", "_"))
}

func subscribe(subscriptions map[t.MetricName][]t.OperationOverTime, lazyAggregates bool, metricName t.MetricName, op t.OperationOverTime) bool {
	if !slices.Contains(subscriptions[metricName], op) {
		if len(subscriptions[metricName]) == 0 {
			subscriptions[metricName] = []t.OperationOverTime{op}
			return true
		} else {
			subscriptions[metricName] = append(subscriptions[metricName], op)
			return lazyAggregates
		}
	}
	return false
}

func checkVectorAggregation(aggregation t.AggregationOverVectors) error {
	switch aggregation {
	case t.VecSum, t.VecAvg, t.VecMin, t.VecMax, t.VecCount:
		return nil
	default:
		return fmt.Errorf("unknown AggregationOverVectors:%s", aggregation)
	}
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

func hashOfMap(m t.Labels) t.LabelsHash {
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
	return t.LabelsHash(fmt.Sprintf("%x", h.Sum(nil)))
}

func (m ms) isStale(datapoint uint32, now int64) bool {
	return now-int64(m.stalePeriodSeconds) > int64(datapoint)
}

func (m ms) calculateAggregate(value float64, counter int, accumulator float64, aggregation t.AggregationOverVectors) float64 {
	if counter == 1 {
		if aggregation == t.VecCount {
			return 1
		} else {
			return value
		}
	}
	switch aggregation {
	case t.VecSum:
		return accumulator + value
	case t.VecAvg:
		// calculate the avg on the fly to avoid potential overflows,
		// idea: each number adds 1/count of itself to the final result
		c := float64(counter)
		cMinusOne := float64(counter - 1)
		return ((accumulator / c) * cMinusOne) + (value / c)
	case t.VecMin:
		return math.Min(accumulator, value)
	case t.VecMax:
		return math.Max(accumulator, value)
	case t.VecCount:
		return accumulator + 1
	default:
		panic("unknown aggregation function: " + aggregation)
	}
}

func newFirstMetricDatapoint(entry t.NewMetricEntry, lazyAggregates bool) *t.MetricData {
	timeInSeconds := timestampToSeconds(entry.MeasurementTime)
	md := t.MetricData{
		Labels:     entry.Labels,
		LastUpdate: timeInSeconds,
		Data: []t.ObservedValue{
			{
				Time:  timeInSeconds,
				Value: entry.MeasurementValue,
			},
		},
		AggregatesOverTime: t.Map[t.OperationOverTime, float64]{},
	}
	if lazyAggregates {
		md.AggregatesOverTime.Store(t.OpMax, 0)
		md.AggregatesOverTime.Store(t.OpAvg, 0)
		md.AggregatesOverTime.Store(t.OpLastOne, 0)
		md.AggregatesOverTime.Store(t.OpCount, 0)
		md.AggregatesOverTime.Store(t.OpRate, 0)
	} else {
		md.AggregatesOverTime.Store(t.OpMax, entry.MeasurementValue)
		md.AggregatesOverTime.Store(t.OpAvg, entry.MeasurementValue)
		md.AggregatesOverTime.Store(t.OpLastOne, entry.MeasurementValue)
		md.AggregatesOverTime.Store(t.OpCount, 1)
		md.AggregatesOverTime.Store(t.OpRate, 0)
	}
	return &md
}

func (m ms) updateAggregatesOverTime(md *t.MetricData, metricName t.MetricName) {
	ops := []t.OperationOverTime{t.OpRate, t.OpMin, t.OpMax, t.OpAvg, t.OpCount, t.OpLastOne}
	if m.lazyAggregates {
		ops = m.subscriptions[metricName]
	}
	for _, op := range ops {
		m.updateAggregationOverTime(op, md)
	}
}

func (m ms) updateAggregationOverTime(overTime t.OperationOverTime, md *t.MetricData) {
	if overTime == t.OpRate {
		valuesDelta := md.Data[len(md.Data)-1].Value - md.Data[0].Value
		timeDelta := float64(md.Data[len(md.Data)-1].Time - md.Data[0].Time)
		md.AggregatesOverTime.Store(t.OpRate, valuesDelta/timeDelta)
	} else if overTime == t.OpMin || overTime == t.OpMax || overTime == t.OpAvg {
		acc, _ := md.AggregatesOverTime.Load(overTime)
		for i := 0; i < len(md.Data); i++ {
			acc = m.calculateAggregate(md.Data[i].Value, i+1, acc, t.AggregationOverVectors(overTime))
		}
		md.AggregatesOverTime.Store(overTime, acc)
	} else if overTime == t.OpCount {
		md.AggregatesOverTime.Store(t.OpCount, float64(len(md.Data)))
	} else if overTime == t.OpLastOne {
		md.AggregatesOverTime.Store(t.OpLastOne, md.Data[len(md.Data)-1].Value)
	}
}

func (m ms) GetStore() *t.Map[string, *t.Map[t.LabelsHash, *t.MetricData]] {
	return m.store
}

// enforce iface impl
var _ t.MemStore = new(ms)
