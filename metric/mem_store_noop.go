package metric

import (
	"fmt"
	"time"

	t "github.com/kedify/otel-add-on/types"
)

type noopMs struct {
	debug bool
}

func NewNoopMetricStore(debug bool) t.MemStore {
	Metrics().Init()
	return noopMs{debug: debug}
}

func (m noopMs) Get(unescapedName t.MetricName, _searchLabels t.LabelMatchers, _timeOp t.OperationOverTime, _defaultAggregation t.AggregationOverVectors) (float64, t.Found, error) {
	if m.debug {
		fmt.Println("noop metric store called Get: " + unescapedName)
	}
	return -1, false, nil
}

func (m noopMs) Put(entry t.NewMetricEntry) {
	if m.debug {
		fmt.Println("noop metric store called Put: " + entry.Name)
	}
}

func (m noopMs) CollectStale(time.Time, time.Duration) int { return 0 }

func (m noopMs) GetSeries(name t.MetricName, _ t.LabelMatchers, _ t.OperationOverTime) ([]t.Series, error) {
	if m.debug {
		fmt.Println("noop metric store called GetSeries: " + name)
	}
	return nil, nil
}

func (m noopMs) IsSubscribed(_lazyAggregates bool, name t.MetricName, _overTime t.OperationOverTime) bool {
	if m.debug {
		fmt.Println("noop metric store called IsSubscribed: " + name)
	}
	return false
}

func (m noopMs) GetStore() *t.Map[string, *t.Map[t.LabelsHash, *t.MetricData]] {
	return nil
}

// enforce iface impl
var _ t.MemStore = new(noopMs)
