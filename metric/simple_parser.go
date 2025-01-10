package metric

import (
	"fmt"
	"strings"

	"github.com/kedify/otel-add-on/types"
)

type p struct {
}

// enforce iface impl
var _ types.Parser = new(p)

func NewParser() types.Parser {
	return p{}
}

func (p p) Parse(metricQuery string) (types.MetricName, types.Labels, types.AggregationOverVectors, error) {
	if metricQuery == "" {
		return "", nil, "", fmt.Errorf("unable to parse metric query: %s", metricQuery)
	}
	mq := strings.TrimSpace(metricQuery)
	aggregateFunction := types.VecSum // default
	for _, aggFn := range []types.AggregationOverVectors{types.VecSum, types.VecAvg, types.VecMin, types.VecMax, types.VecCount} {
		if strings.HasPrefix(mq, string(aggFn)+"(") && strings.HasSuffix(mq, ")") {
			aggregateFunction = aggFn
			mq = strings.TrimPrefix(mq, string(aggFn)+"(")
			mq = strings.TrimSuffix(mq, ")")
		}
	}
	first := strings.Index(mq, "{")
	last := strings.LastIndex(mq, "}")
	if last < first || (last > 0 && first == -1) {
		return "", nil, "", fmt.Errorf("unable to parse metric query: %s", metricQuery)
	}
	if first == -1 && last == -1 { // no labels specified
		return types.MetricName(mq), map[string]any{}, aggregateFunction, nil
	}
	metricName := types.MetricName(mq[:first])
	labels, err := p.ParseLabels(mq[first+1 : last])
	if err != nil {
		return "", nil, "", err
	}
	return metricName, labels, aggregateFunction, nil
}

func (p p) ParseLabels(labelsQuery string) (types.Labels, error) {
	lq := strings.TrimSpace(labelsQuery)
	if lq == "" {
		return nil, fmt.Errorf("unable to parse labels: %s", lq)
	}

	chunks := strings.Split(lq, ",")
	labels := make(map[string]any, len(chunks))
	for _, chunk := range chunks {
		if !strings.Contains(chunk, "=") {
			return nil, fmt.Errorf("unable to parse labels, labels are expected at form {key1=val1, key2=val2}, but got: %s", lq)
		}
		labelRaw := strings.Split(chunk, "=")
		if len(labelRaw) != 2 {
			return nil, fmt.Errorf("unable to parse labels, labels are expected at form {key1=val1, key2=val2}, but got: %s", lq)
		}

		labels[strings.Trim(labelRaw[0], "\" '")] = strings.Trim(labelRaw[1], "\" '")
	}
	return labels, nil
}
