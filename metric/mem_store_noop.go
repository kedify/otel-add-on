package metric

import (
	"fmt"

	t "github.com/kedify/otel-add-on/types"
)

type noopMs struct {
}

func NewNoopMetricStore() t.MemStore {
	return noopMs{}
}

func (m noopMs) Get(unescapedName t.MetricName, searchLabels t.Labels, timeOp t.OperationOverTime, defaultAggregation t.AggregationOverVectors) (float64, t.Found, error) {
	fmt.Println("noop metric store called Get: " + unescapedName)
	return -1, false, nil
}

func (m noopMs) Put(entry t.NewMetricEntry) {
	fmt.Println("noop metric store called Put: " + entry.Name)
}

func (m noopMs) IsSubscribed(lazyAggregates bool, name t.MetricName, overTime t.OperationOverTime) bool {
	fmt.Println("noop metric store called IsSubscribed: " + name)
	return false
}

func (m noopMs) GetStore() *t.Map[string, *t.Map[t.LabelsHash, *t.MetricData]] {
	return nil
}

// enforce iface impl
var _ t.MemStore = new(noopMs)
