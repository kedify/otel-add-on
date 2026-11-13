package metric

import (
	"fmt"
	"math"
	"slices"

	"github.com/kedify/otel-add-on/types"
	"github.com/kedify/otel-add-on/util"
)

type expressionValue struct {
	scalar float64
	vector []types.Series
}

func (q *compiledQuery) MetricNames() []types.MetricName {
	return slices.Clone(q.names)
}

func (q *compiledQuery) Evaluate(store types.MemStore, timeOp types.OperationOverTime) (float64, types.Found, error) {
	if err := util.CheckTimeOp(timeOp); err != nil {
		return 0, false, err
	}
	// Load each metric once, before traversing the tree. This registers every
	// metric in lazy modes even if an earlier selector is empty.
	metrics := make(map[types.MetricName][]types.Series, len(q.names))
	for _, name := range q.names {
		series, err := store.GetSeries(name, nil, timeOp)
		if err != nil {
			return 0, false, err
		}
		metrics[name] = series
	}
	result, err := q.root.evaluate(metrics)
	if err != nil {
		return 0, false, err
	}
	value := result.scalar
	if q.root.kind == vectorType {
		if len(result.vector) == 0 {
			return 0, false, nil
		}
		for _, series := range result.vector {
			value += series.Value
		}
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false, fmt.Errorf("metric query returned a non-finite result: %v", value)
	}
	return value, true, nil
}

func (e *expression) evaluate(metrics map[types.MetricName][]types.Series) (expressionValue, error) {
	switch e.op {
	case "number":
		return expressionValue{scalar: e.number}, nil
	case "selector":
		var result []types.Series
		for _, series := range metrics[e.name] {
			if e.matchers.Matches(series.Labels) {
				if series.HistoryError != nil {
					return expressionValue{}, series.HistoryError
				}
				result = append(result, series)
			}
		}
		return expressionValue{vector: result}, nil
	}
	left, err := e.args[0].evaluate(metrics)
	if err != nil {
		return expressionValue{}, err
	}
	if e.op == "unary+" {
		return left, nil
	}
	if e.op == "unary-" {
		left.scalar = -left.scalar
		for i := range left.vector {
			left.vector[i].Value = -left.vector[i].Value
		}
		return left, nil
	}
	if len(e.args) == 1 {
		switch e.op {
		case "vector":
			return expressionValue{vector: []types.Series{{Value: left.scalar}}}, nil
		case "scalar":
			if len(left.vector) != 1 {
				return expressionValue{scalar: math.NaN()}, nil
			}
			return expressionValue{scalar: left.vector[0].Value}, nil
		case "ln", "log2":
			for i := range left.vector {
				if e.op == "ln" {
					left.vector[i].Value = math.Log(left.vector[i].Value)
				} else {
					left.vector[i].Value = math.Log2(left.vector[i].Value)
				}
			}
			return left, nil
		default: // Aggregations return one unlabeled vector element, or an empty vector.
			if len(left.vector) == 0 {
				return left, nil
			}
			var value float64
			for i, series := range left.vector {
				value = calculateAggregate(series.Value, i+1, value, types.AggregationOverVectors(e.op))
			}
			return expressionValue{vector: []types.Series{{Value: value}}}, nil
		}
	}
	right, err := e.args[1].evaluate(metrics)
	if err != nil {
		return expressionValue{}, err
	}
	if e.op == "or" {
		return unionVectors(left.vector, right.vector), nil
	}
	if e.op == "clamp_min" || e.op == "clamp_max" {
		for i := range left.vector {
			if e.op == "clamp_min" {
				left.vector[i].Value = math.Max(left.vector[i].Value, right.scalar)
			} else {
				left.vector[i].Value = math.Min(left.vector[i].Value, right.scalar)
			}
		}
		return left, nil
	}
	if e.args[0].kind == scalarType && e.args[1].kind == scalarType {
		return expressionValue{scalar: arithmetic(e.op, left.scalar, right.scalar)}, nil
	}
	if e.args[0].kind == vectorType && e.args[1].kind == vectorType {
		return matchVectors(e.op, left.vector, right.vector)
	}
	if e.args[0].kind == vectorType {
		for i := range left.vector {
			left.vector[i].Value = arithmetic(e.op, left.vector[i].Value, right.scalar)
		}
		return left, nil
	}
	for i := range right.vector {
		right.vector[i].Value = arithmetic(e.op, left.scalar, right.vector[i].Value)
	}
	return right, nil
}

func arithmetic(op string, left, right float64) float64 {
	switch op {
	case "+":
		return left + right
	case "-":
		return left - right
	case "*":
		return left * right
	case "/":
		return left / right
	case "%":
		return math.Mod(left, right)
	case "^":
		return math.Pow(left, right)
	default:
		panic("unknown compiled arithmetic operator: " + op)
	}
}

func matchingKey(labels types.Labels) types.LabelsHash {
	// Stored label values can be numbers or strings. Match their textual form,
	// independent of map order and metric name, as label selectors do.
	normalized := make(types.Labels, len(labels))
	for key, value := range labels {
		if key != "__name__" {
			normalized[key] = fmt.Sprint(value)
		}
	}
	return hashOfMap(normalized)
}

func indexVector(vector []types.Series) (map[types.LabelsHash]types.Series, error) {
	index := make(map[types.LabelsHash]types.Series, len(vector))
	for _, series := range vector {
		key := matchingKey(series.Labels)
		if _, exists := index[key]; exists {
			return nil, fmt.Errorf("duplicate series for vector matching labels %v (excluding __name__)", series.Labels)
		}
		index[key] = series
	}
	return index, nil
}

func unionVectors(left, right []types.Series) expressionValue {
	// Set operators allow many-to-many matching. Keep every left series, then
	// right series whose matching labels do not appear anywhere on the left.
	leftKeys := make(map[types.LabelsHash]struct{}, len(left))
	for _, series := range left {
		leftKeys[matchingKey(series.Labels)] = struct{}{}
	}
	result := slices.Clone(left)
	for _, series := range right {
		if _, found := leftKeys[matchingKey(series.Labels)]; !found {
			result = append(result, series)
		}
	}
	return expressionValue{vector: result}
}

func matchVectors(op string, left, right []types.Series) (expressionValue, error) {
	if _, err := indexVector(left); err != nil {
		return expressionValue{}, err
	}
	rightIndex, err := indexVector(right)
	if err != nil {
		return expressionValue{}, err
	}
	var result []types.Series
	for _, lhs := range left {
		if rhs, found := rightIndex[matchingKey(lhs.Labels)]; found {
			result = append(result, types.Series{Labels: lhs.Labels, Value: arithmetic(op, lhs.Value, rhs.Value)})
		}
	}
	return expressionValue{vector: result}, nil
}
