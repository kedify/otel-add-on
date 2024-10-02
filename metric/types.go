package metric

import "go.opentelemetry.io/collector/pdata/pcommon"

type MetricEntry struct {
	ObservedValue
	// metric name
	Name MetricName
	// labels further identifies the collected data points (introducing new dimensions and storing also metadata) ~ tags
	Labels Labels
}

type Aggregation string
type Stale bool
type Found bool
type MetricName string
type Labels map[string]any
type LabelsHash string
type ObservedValue struct {
	// observed value
	Value float64
	// timestamp of last update
	LastUpdate pcommon.Timestamp
	Labels     Labels
}
type StoredMetrics map[LabelsHash]ObservedValue

const (
	Sum Aggregation = "sum"
	Avg Aggregation = "avg"
	Min Aggregation = "min"
	Max Aggregation = "max"
)

type MemStore interface {
	// Get retrieves the latest value from the in-memory metric store
	Get(MetricName, Labels, Aggregation) (float64, Stale, Found)

	// Put stores the value
	Put(MetricEntry)

	// Gc removes the data points older than certain threshold from the store
	Gc()
}

type Parser interface {
	// Parse parses the metric queyr provided as a string
	Parse(string) (MetricName, Labels, Aggregation, error)
}
