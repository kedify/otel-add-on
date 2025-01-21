package types

import "go.opentelemetry.io/collector/pdata/pcommon"

type NewMetricEntry struct {
	// metric name
	Name MetricName `json:"name"`
	// labels further identifies the collected data points (introducing new dimensions and storing also metadata) ~ tags
	Labels Labels `json:"labels"`
	// observed value
	MeasurementValue float64           `json:"measurementValue"`
	MeasurementTime  pcommon.Timestamp `json:"measurementTime"`
}

type AggregationOverVectors string
type OperationOverTime string
type Stale bool
type Found bool
type MetricName string
type Labels map[string]any
type LabelsHash string
type MetricData struct {
	Labels             Labels                          `json:"labels"`
	Data               []ObservedValue                 `json:"data"`
	AggregatesOverTime Map[OperationOverTime, float64] `json:"aggregatesOverTime"`
	LastUpdate         uint32                          `json:"lastUpdate"`
}

type ObservedValue struct {
	// observed value
	Value float64 `json:"value"`
	// timestamp of last update (in seconds)
	Time uint32 `json:"time"`
}

const (
	// following aggregations can be applied across multiple metric series. This automatically happens if provided
	// set of labels wasn't specific enough to identify just one vector. In which case we first apply the OperationOverTime
	// and on the resulting set of numbers where each represents last_one, rate, min, max, avg of the time serie, we apply
	// this function

	// VecSum sums the numbers
	VecSum AggregationOverVectors = "sum"
	// VecAvg calculate the mean value
	VecAvg AggregationOverVectors = "avg"
	// VecMin calculate the minimum value
	VecMin AggregationOverVectors = "min"
	// VecMax calculate the max value
	VecMax AggregationOverVectors = "max"
	// VecCount calculate the number of occurrences
	VecCount AggregationOverVectors = "count"

	// following operations can be applied on one time serie vector that was captured over time
	// returning just one number

	// OpLastOne returns the last measured value
	OpLastOne OperationOverTime = "last_one"

	// OpRate calculates the per-second growth. Suitable for monotonic time series and is calculated as
	// delta between last and first measured element divided by overTimePeriodSeconds
	OpRate  OperationOverTime = "rate"
	OpCount OperationOverTime = "count"
	OpAvg   OperationOverTime = "avg"
	OpMin   OperationOverTime = "min"
	OpMax   OperationOverTime = "max"

	// when adding new AggregationOverVectors or OperationOverTime, don't forget to change also ValidatingAdmissionPolicy
)

type MemStore interface {
	// Get retrieves the latest value from the in-memory metric store
	Get(MetricName, Labels, OperationOverTime, AggregationOverVectors) (float64, Found, error)

	// Put stores the value
	Put(NewMetricEntry)

	// GetStore returns the internal storage
	GetStore() *Map[string, *Map[LabelsHash, *MetricData]]
}

type Parser interface {
	// Parse parses the metric query provided as a string
	Parse(string) (MetricName, Labels, AggregationOverVectors, error)
}
