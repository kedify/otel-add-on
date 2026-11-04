package metric

import (
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/kedify/otel-add-on/types"
	"github.com/kedify/otel-add-on/util"
)

func compileQuery(t *testing.T, input string) types.Query {
	t.Helper()
	query, err := NewParser().Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	return query
}

func TestExpressionArithmetic(t *testing.T) {
	store := newMetricStore(60, false, false)
	for _, tt := range []struct {
		input string
		want  float64
	}{
		{"1 + 2 * 3", 7}, {"(1 + 2) * 3", 9}, {"10 - 3 - 2", 5}, {"24 / 4 / 2", 3},
		{"11 % 4 * 2", 6}, {"2 ^ 3 ^ 2", 512}, {"-2 ^ 2", -4}, {"(-2) ^ 2", 4},
		{"2 ^ -2", .25}, {"-2 ^ -2", -.25}, {"--2 + +-3", -1}, {"2 * -3 ^ 2", -18},
		{".5 + 1. + 1e-2 + 1E+1", 11.51}, {"\n +2\n*\t(3 - 1)\n", 4},
	} {
		t.Run(tt.input, func(t *testing.T) {
			query := compileQuery(t, tt.input)
			if len(query.MetricNames()) != 0 {
				t.Fatalf("scalar expression references metrics: %v", query.MetricNames())
			}
			value, found, err := query.Evaluate(store, types.OpLastOne)
			assertMetricFound(t, value, found, err, tt.want)
		})
	}
}

func TestExpressionFunctionsAndAggregation(t *testing.T) {
	store := newMetricStore(60, false, false)
	setupMetrics(store, "rabbitmq_queue_messages_ready", 1, types.Labels{"queue": "one"}, 1)
	setupMetrics(store, "rabbitmq_queue_messages_ready", 1, types.Labels{"queue": "nine"}, 9)
	const vector = `log2(clamp_min(rabbitmq_queue_messages_ready - 1, 1)) +
		clamp_max(rabbitmq_queue_messages_ready, 1) + rabbitmq_queue_messages_ready / 10`
	for _, tt := range []struct {
		query string
		want  float64
	}{
		{"max(\n" + vector + "\n)", 4.9}, {vector, 6}, {"avg(" + vector + ")", 3},
		{"min(" + vector + ")", 1.1}, {"count(" + vector + ")", 2},
		{"sum(max(" + vector + "))", 4.9}, {"count(sum(" + vector + "))", 1},
		{"clamp_max(" + vector + ", 2)", 3.1},
		{"clamp_max(log2(rabbitmq_queue_messages_ready + 1), 2)", 3},
		{"2 * rabbitmq_queue_messages_ready + 1", 22}, {"20 / rabbitmq_queue_messages_ready", 20 + 20.0/9},
		{"20 - rabbitmq_queue_messages_ready", 30}, {"2 ^ rabbitmq_queue_messages_ready", 514},
		{"10 % rabbitmq_queue_messages_ready", 1}, {"-rabbitmq_queue_messages_ready", -10},
		{"rabbitmq_queue_messages_ready ^ 2", 82},
		{"sum(rabbitmq_queue_messages_ready) + avg(rabbitmq_queue_messages_ready)", 15},
		{`rabbitmq_queue_messages_ready{queue!="nine"} + rabbitmq_queue_messages_ready{queue="one"}`, 2},
	} {
		t.Run(tt.query, func(t *testing.T) {
			value, found, err := compileQuery(t, tt.query).Evaluate(store, types.OpLastOne)
			assertMetricFound(t, value, found, err, tt.want)
		})
	}
}

func TestExpressionScalar(t *testing.T) {
	store := newMetricStore(60, false, false)
	setupMetrics(store, "input", 1, types.Labels{"queue": "orders"}, 2)
	setupMetrics(store, "input", 1, types.Labels{"queue": "payments"}, 6)
	for _, tt := range []struct {
		query string
		want  float64
	}{
		{`scalar(input{queue="orders"})`, 2},
		{`scalar(input{queue="orders"} * 0)`, 0},
		{`scalar(-input{queue="orders"})`, -2},
		{"scalar(sum(input))", 8},
		{"scalar(log2(sum(input)))", 3},
		{`scalar(sum(input)) - scalar(input{queue="orders"})`, 6},
		{"-scalar(sum(input))", -8},
		{"scalar(log2(sum(input))) - input", -2},
		{"input - scalar(log2(sum(input)))", 2},
		{"scalar(sum(input)) / input", 4 + 8.0/6},
		{"input / scalar(sum(input))", 1},
		{"clamp_min(input, scalar(avg(input)))", 10},
		{`(scalar(sum(input)) - input) + input{queue="orders"}`, 8},
		// NaN intermediates follow floating-point arithmetic, like other functions.
		{"scalar(missing) ^ 0", 1},
		{"scalar(input) ^ 0", 1},
	} {
		t.Run(tt.query, func(t *testing.T) {
			value, found, err := compileQuery(t, tt.query).Evaluate(store, types.OpLastOne)
			assertMetricFound(t, value, found, err, tt.want)
		})
	}
	for _, input := range []string{
		"scalar(input)", "scalar(input * 0)", "scalar(missing)",
		`scalar(input{queue="absent"})`, "scalar(sum(missing))",
	} {
		t.Run(input, func(t *testing.T) {
			_, found, err := compileQuery(t, input).Evaluate(store, types.OpLastOne)
			if bool(found) || err == nil || !strings.Contains(err.Error(), "non-finite") {
				t.Fatalf("expected non-finite error, got found=%v, error=%v", found, err)
			}
		})
	}
}

func TestExpressionScalarHTTPRequests(t *testing.T) {
	store := newMetricStore(60, false, false)
	for _, series := range []struct {
		method string
		value  float64
	}{{"POST", 2605}, {"PUT", 77}, {"DELETE", 2}} {
		setupMetrics(store, "http_request_duration_seconds_count", 1,
			types.Labels{"method": series.method, "path": "metrics", "status": "200", "pod_name": "one"}, series.value)
	}
	const requests = `http_request_duration_seconds_count{method=~"POST|PUT",status="200",path=~".*s"}`
	const deletes = `http_request_duration_seconds_count{method="DELETE",path="metrics",status="200"}`
	const vectorQuery = "log2(sum(" + requests + ")) - " + deletes
	const scalarQuery = "scalar(log2(sum(" + requests + "))) - " + deletes
	for _, tt := range []struct {
		deleteSeries int
		want         float64
	}{{1, math.Log2(2682) - 2}, {2, 2*math.Log2(2682) - 5}} {
		t.Run(fmt.Sprintf("%d DELETE series", tt.deleteSeries), func(t *testing.T) {
			if tt.deleteSeries == 2 {
				setupMetrics(store, "http_request_duration_seconds_count", 1,
					types.Labels{"method": "DELETE", "path": "metrics", "status": "200", "pod_name": "two"}, 3)
			}
			value, found, err := compileQuery(t, scalarQuery).Evaluate(store, types.OpLastOne)
			assertMetricFound(t, value, found, err, tt.want)
			_, found, err = compileQuery(t, vectorQuery).Evaluate(store, types.OpLastOne)
			if err != nil || found {
				t.Fatalf("unmatched vectors should remain empty: found=%v, error=%v", found, err)
			}
		})
	}
}

func TestExpressionVectorAndOr(t *testing.T) {
	store := newMetricStore(60, false, false)
	setupMetrics(store, "input", 1, types.Labels{"queue": "orders"}, 2)
	setupMetrics(store, "input", 1, types.Labels{"queue": "payments"}, 6)
	setupMetrics(store, "other", 1, types.Labels{"queue": "orders"}, 10)
	setupMetrics(store, "other", 1, types.Labels{"queue": "other"}, 3)
	store.Put(types.NewMetricEntry{Name: "stale", MeasurementValue: 9, MeasurementTime: pcommon.Timestamp(time.Now().Unix() - 120)})
	for _, tt := range []struct {
		query string
		want  float64
	}{
		{"vector(0)", 0}, {"vector(-2 ^ 2)", -4}, {"scalar(vector(7))", 7},
		{"vector(scalar(sum(input)))", 8},
		{"sum(missing) or vector(0)", 0}, {"sum(input) or vector(99)", 8},
		{"sum(input * 0) or vector(99)", 0}, {"stale or vector(7)", 7},
		{"input or missing", 8}, {"missing or input", 8},
		{"input or other", 11}, {"other or input", 19},
		{"(input or other) + input", 16},
		{"count(input or vector(0))", 3}, // The unlabeled zero is a separate series.
		{"vector(1) or vector(2) + vector(3)", 1},
		{"vector(1) + vector(2) or vector(3)", 3},
		{"vector(1) or vector(2) or vector(3)", 1},
		{"vector(1)or(vector(2))", 1}, {"sum(missing)\n or\tvector(7)", 7},
		{"log2(sum(input)) - (sum(missing) or vector(0))", 3},
		{"clamp_min(input, scalar(sum(missing) or vector(4)))", 10},
		{"vector(1) or vector(scalar(missing))", 1},
	} {
		t.Run(tt.query, func(t *testing.T) {
			value, found, err := compileQuery(t, tt.query).Evaluate(store, types.OpLastOne)
			assertMetricFound(t, value, found, err, tt.want)
		})
	}
	if _, found, err := compileQuery(t, "missing or also_missing").Evaluate(store, types.OpLastOne); err != nil || found {
		t.Fatalf("union of empty vectors should remain empty: found=%v, error=%v", found, err)
	}
	for _, input := range []string{"vector(1 / 0) or vector(0)", "vector(scalar(missing)) or vector(0)"} {
		_, _, err := compileQuery(t, input).Evaluate(store, types.OpLastOne)
		if err == nil || !strings.Contains(err.Error(), "non-finite") {
			t.Fatalf("or must not replace non-finite samples for %s: %v", input, err)
		}
	}
}

func TestExpressionOrManyToMany(t *testing.T) {
	store := newMetricStore(60, false, false)
	for _, series := range []struct {
		name, queue, metricName string
		value                   float64
	}{
		{"left", "orders", "one", 1}, {"left", "orders", "two", 2},
		{"right", "orders", "three", 100},
		{"right", "payments", "one", 3}, {"right", "payments", "two", 4},
	} {
		setupMetrics(store, series.name, 1, types.Labels{"queue": series.queue, "__name__": series.metricName}, series.value)
	}
	value, found, err := compileQuery(t, "left or right").Evaluate(store, types.OpLastOne)
	assertMetricFound(t, value, found, err, 10)
	value, found, err = compileQuery(t, "count(left or right)").Evaluate(store, types.OpLastOne)
	assertMetricFound(t, value, found, err, 4)
}

func TestExpressionFallbackHTTPRequests(t *testing.T) {
	store := newMetricStore(60, false, false)
	setupMetrics(store, "http_request_duration_seconds_count", 1, types.Labels{"method": "POST", "path": "metrics", "status": "200"}, 2605)
	setupMetrics(store, "http_request_duration_seconds_count", 1, types.Labels{"method": "PUT", "path": "metrics", "status": "200"}, 77)
	const requests = `http_request_duration_seconds_count{method=~"POST|PUT",status="200",path=~".*s"}`
	const updates = `http_request_duration_seconds_count{method="UPDATE",path="metrics",status="200"}`
	query := compileQuery(t, "log2(sum("+requests+")) - (sum("+updates+") or vector(0))")
	value, found, err := query.Evaluate(store, types.OpLastOne)
	assertMetricFound(t, value, found, err, math.Log2(2682))
	for _, count := range []float64{1, 5} {
		setupMetrics(store, "http_request_duration_seconds_count", 1, types.Labels{"method": "UPDATE", "path": "metrics", "status": "200"}, count)
		value, found, err = query.Evaluate(store, types.OpLastOne)
		assertMetricFound(t, value, found, err, math.Log2(2682)-count)
	}
}

func TestExpressionLazyFallback(t *testing.T) {
	for _, lazySeries := range []bool{false, true} {
		for _, lazyAggregates := range []bool{false, true} {
			t.Run(fmt.Sprintf("series=%t/aggregates=%t", lazySeries, lazyAggregates), func(t *testing.T) {
				store := newMetricStore(60, lazySeries, lazyAggregates)
				query := compileQuery(t, "sum(first) or sum(second) or vector(0)")
				value, found, err := query.Evaluate(store, types.OpLastOne)
				assertMetricFound(t, value, found, err, 0)
				for _, name := range query.MetricNames() {
					if (lazySeries || lazyAggregates) && !store.IsSubscribed(lazyAggregates, name, types.OpLastOne) {
						t.Fatalf("fallback did not subscribe %s", name)
					}
				}
				setupMetrics(store, "second", 1, nil, 3, 5)
				value, found, err = query.Evaluate(store, types.OpLastOne)
				assertMetricFound(t, value, found, err, 5)
				setupMetrics(store, "first", 1, nil, 2, 4)
				value, found, err = query.Evaluate(store, types.OpLastOne)
				assertMetricFound(t, value, found, err, 4)
			})
		}
	}
}

func TestRabbitMQExampleExpression(t *testing.T) {
	manifest, err := os.ReadFile("../examples/rabbitmq/rabbitmq-so.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var scaledObject struct {
		Spec struct {
			Triggers []struct{ Metadata map[string]string }
		}
	}
	if err := yaml.NewYAMLOrJSONDecoder(strings.NewReader(string(manifest)), 4096).Decode(&scaledObject); err != nil {
		t.Fatal(err)
	}
	metadata := scaledObject.Spec.Triggers[0].Metadata
	store := newMetricStore(60, false, false)
	for _, series := range []struct {
		queue string
		value float64
	}{{"example-queue1", 1}, {"example-queue2", 9}, {"unselected-queue", 1000}} {
		setupMetrics(store, "rabbitmq_detailed_queue_messages_ready", 1, types.Labels{"queue": series.queue}, series.value)
	}
	input := metadata[util.MetadataMetricQuery]
	value, found, err := compileQuery(t, input).Evaluate(store, types.OperationOverTime(metadata[util.MetadataOperationOverTime]))
	assertMetricFound(t, value, found, err, 4.9)
	withoutMax := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(input), "max("), ")")
	value, found, err = compileQuery(t, withoutMax).Evaluate(store, types.OpLastOne)
	assertMetricFound(t, value, found, err, 6)
}

func TestExpressionNaturalLogAndNonfinite(t *testing.T) {
	store := newMetricStore(60, false, false)
	for _, tt := range []struct {
		label string
		value float64
	}{{"one", 1}, {"e", math.E}, {"zero", 0}, {"negative", -1}} {
		setupMetrics(store, "input", 1, types.Labels{"value": tt.label}, tt.value)
	}
	for _, tt := range []struct {
		query string
		want  float64
	}{
		{`ln(input{value="one"})`, 0}, {`ln(input{value="e"})`, 1},
		{`ln(input{value=~"one|e"})`, 1}, {`clamp_min(ln(input{value="zero"}), -2)`, -2},
		{`clamp_max(input{value="one"} / 0, 10)`, 10},
		{`count(ln(input{value=~"zero|negative"}))`, 2},
	} {
		value, found, err := compileQuery(t, tt.query).Evaluate(store, types.OpLastOne)
		assertMetricFound(t, value, found, err, tt.want)
	}
	for _, input := range []string{
		`ln(input{value="zero"})`, `ln(input{value="negative"})`,
		`log2(input{value="negative"})`, `sum(ln(input))`, "1 / 0", "0 / 0", "1 % 0", "(-1) ^ .5", "10 ^ 1000",
	} {
		t.Run(input, func(t *testing.T) {
			_, _, err := compileQuery(t, input).Evaluate(store, types.OpLastOne)
			if err == nil || !strings.Contains(err.Error(), "non-finite") {
				t.Fatalf("expected non-finite error, got %v", err)
			}
		})
	}
}

func TestExpressionCompileErrors(t *testing.T) {
	for _, tt := range []struct{ input, error string }{
		{"ln()", "expects 1"}, {"ln(input, 1)", "expects 1"}, {"clamp_min(input)", "expects 2"},
		{"scalar()", "expects 1"}, {"scalar(input, 1)", "expects 1"},
		{"scalar(1)", "must be a vector"}, {"scalar(scalar(input))", "must be a vector"},
		{"sum(scalar(input))", "must be a vector"}, {"log2(scalar(input))", "must be a vector"},
		{"vector()", "expects 1"}, {"vector(1, 2)", "expects 1"}, {"vector(input)", "must be a scalar"},
		{"input or 0", "operands must be vectors"}, {"0 or input", "operands must be vectors"},
		{"scalar(input) or vector(0)", "operands must be vectors"}, {"input or", "expected expression"},
		{"input orders", "unexpected input"}, {"input or1", "unexpected input"},
		{"input or.foo", "unexpected input"}, {"input orvector(0)", "unexpected input"},
		{"clamp_max(input, 1, 2)", "expects 2"}, {"sum(input, input)", "expects 1"},
		{"sum(1)", "must be a vector"}, {"ln(2)", "must be a vector"}, {"log2(2)", "must be a vector"},
		{"clamp_min(1, 2)", "must be a vector"}, {"clamp_min(input, sum(input))", "must be a scalar"},
		{"sqrt(input)", "unsupported function"}, {"rate(input)", "unsupported function"},
		{"input > 2", "comparisons"}, {"input == 2", "comparisons"}, {"input[5m]", "range selectors"},
		{"(input)[5m:1m]", "subqueries"}, {"input offset 5m", "offsets"}, {"input @ 123", "modifiers"},
		{"sum by(queue)(input)", "grouping"}, {"sum(input) without(queue)", "grouping"},
		{"input + on(queue) other", "unsupported"}, {"input * ignoring(queue) other", "unsupported"},
		{"1e+", "numeric literal"}, {".", "numeric literal"}, {"(1 + 2", "closing parenthesis"},
		{"1 +", "expected expression"}, {"", "expected expression"}, {"\n  \t", "expected expression"},
		{"input other", "unexpected input"}, {"clamp_min(input,)", "unexpected input"},
	} {
		t.Run(tt.input, func(t *testing.T) {
			_, err := NewParser().Parse(tt.input)
			if err == nil || !strings.Contains(err.Error(), tt.error) {
				t.Fatalf("want error containing %q, got %v", tt.error, err)
			}
		})
	}
}

func TestExpressionCrossMetricMatching(t *testing.T) {
	store := newMetricStore(60, false, false)
	for _, series := range []struct {
		name   string
		labels types.Labels
		value  float64
	}{
		{"left", types.Labels{"queue": "orders", "id": 1, "__name__": "left"}, 8},
		{"right", types.Labels{"id": "1", "__name__": "right", "queue": "orders"}, 2},
		{"left", types.Labels{"queue": "left-only"}, 100},
		{"right", types.Labels{"queue": "right-only"}, 1000},
		{"right", types.Labels{"queue": "orders", "id": "1", "extra": "label"}, 2000},
	} {
		setupMetrics(store, series.name, 1, series.labels, series.value)
	}
	for _, tt := range []struct {
		query string
		want  float64
	}{
		{"left / right", 4}, {"left - right", 6}, {"left + right", 10}, {"left * right", 16},
		{"count(left / right)", 1}, {`(left * 2) / clamp_min(right{queue="orders"}, 1)`, 8},
		{`sum(left) / sum(right)`, 108.0 / 3002},
	} {
		value, found, err := compileQuery(t, tt.query).Evaluate(store, types.OpLastOne)
		assertMetricFound(t, value, found, err, tt.want)
	}
	setupMetrics(store, "left", 1, types.Labels{"queue": "orders", "id": 1, "__name__": "duplicate"}, 10)
	for _, input := range []string{"left + right", "right + left", "left + missing"} {
		_, _, err := compileQuery(t, input).Evaluate(store, types.OpLastOne)
		if err == nil || !strings.Contains(err.Error(), "duplicate series") {
			t.Fatalf("expected duplicate matching key error for %s, got %v", input, err)
		}
	}
}

type trackingStore struct {
	types.MemStore
	reads map[types.MetricName]int
}

func (s trackingStore) GetSeries(name types.MetricName, labels types.LabelMatchers, op types.OperationOverTime) ([]types.Series, error) {
	s.reads[name]++
	return s.MemStore.GetSeries(name, labels, op)
}

func TestExpressionLazyModesAndRepeatedSelectors(t *testing.T) {
	for _, lazySeries := range []bool{false, true} {
		for _, lazyAggregates := range []bool{false, true} {
			t.Run(fmt.Sprintf("series=%t/aggregates=%t", lazySeries, lazyAggregates), func(t *testing.T) {
				store := trackingStore{MemStore: newMetricStore(60, lazySeries, lazyAggregates), reads: map[types.MetricName]int{}}
				query := compileQuery(t, `first{queue="orders"} + first{queue!="dev"} + second + third`)
				if !slices.Equal(query.MetricNames(), []types.MetricName{"first", "second", "third"}) {
					t.Fatalf("unexpected references: %v", query.MetricNames())
				}
				_, found, err := query.Evaluate(store, types.OpMin)
				if err != nil || found {
					t.Fatalf("empty store: found=%v, error=%v", found, err)
				}
				for _, name := range query.MetricNames() {
					if store.reads[name] != 1 {
						t.Errorf("read %s %d times, want 1", name, store.reads[name])
					}
					if (lazySeries || lazyAggregates) && !store.IsSubscribed(lazyAggregates, name, types.OpMin) {
						t.Errorf("metric %s was not subscribed", name)
					}
					setupMetrics(store, string(name), 1, types.Labels{"queue": "orders"}, 2, 4)
				}
				value, found, err := query.Evaluate(store, types.OpMin)
				assertMetricFound(t, value, found, err, 8)
				for _, name := range query.MetricNames() {
					if store.reads[name] != 2 {
						t.Errorf("read %s %d times, want 2", name, store.reads[name])
					}
				}
			})
		}
	}
}

func TestExpressionTimeAggregationAndEmptyResults(t *testing.T) {
	store := newMetricStore(60, false, false)
	setupMetrics(store, "input", 1, types.Labels{"queue": "orders"}, 1, 3)
	setupMetrics(store, "input", 1, types.Labels{"queue": "payments"}, 3, 5)
	value, found, err := compileQuery(t, "input ^ 2").Evaluate(store, types.OpAvg)
	assertMetricFound(t, value, found, err, 20) // avg([1,3])^2 + avg([3,5])^2
	value, found, err = compileQuery(t, "max(input ^ 2)").Evaluate(store, types.OpMin)
	assertMetricFound(t, value, found, err, 9)
	store.Put(types.NewMetricEntry{Name: "stale", MeasurementValue: 9, MeasurementTime: pcommon.Timestamp(time.Now().Unix() - 120)})
	for _, input := range []string{
		"missing + 10", "input + missing", "count(missing)", "sum(missing)", "clamp_min(missing, 10)",
		`input{queue="absent"}`, `input{queue="orders"} / input{queue="payments"}`, "stale + 10",
	} {
		_, found, err := compileQuery(t, input).Evaluate(store, types.OpLastOne)
		if err != nil || found {
			t.Errorf("%s: want not found, got found=%v, error=%v", input, found, err)
		}
	}
	if _, _, err := compileQuery(t, "1").Evaluate(store, "unknown"); err == nil {
		t.Fatal("scalar query must still validate the time operation")
	}
}

func TestExpressionNormalizedNamesAndSnapshots(t *testing.T) {
	store := newMetricStore(60, false, false)
	setupMetrics(store, "otel/input-value", 1, types.Labels{"queue": "orders"}, 3)
	query := compileQuery(t, "otel_input_value + otel.input_value")
	if !slices.Equal(query.MetricNames(), []types.MetricName{"otel_input_value"}) {
		t.Fatalf("normalized aliases should be deduplicated: %v", query.MetricNames())
	}
	value, found, err := query.Evaluate(store, types.OpMin)
	assertMetricFound(t, value, found, err, 6)
	series, err := store.GetSeries("otel_input_value", nil, types.OpMin)
	if err != nil || len(series) != 1 {
		t.Fatalf("unexpected snapshot: %v, %v", series, err)
	}
	series[0].Labels["queue"] = "mutated"
	value, found, err = compileQuery(t, `otel_input_value{queue="orders"}`).Evaluate(store, types.OpMin)
	assertMetricFound(t, value, found, err, 3)
	// Slash and hyphen now denote arithmetic; stored names use underscores.
	if !slices.Equal(compileQuery(t, "one/two-three").MetricNames(), []types.MetricName{"one", "two", "three"}) {
		t.Fatal("slash and hyphen must be parsed as arithmetic")
	}
}

func TestExpressionConcurrentEvaluationAndIngestion(t *testing.T) {
	store := NewMetricStore(&util.Config{MetricStoreRetentionSeconds: 60, MetricStoreLazyAggregates: true})
	query := compileQuery(t, "input * input")
	if _, _, err := query.Evaluate(store, types.OpLastOne); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Go(func() {
			for i := 0; i < 30; i++ {
				setupMetrics(store, "input", 1, types.Labels{"queue": "orders"}, float64(i))
				if _, _, err := query.Evaluate(store, types.OpLastOne); err != nil {
					t.Error(err)
				}
				store.IsSubscribed(true, "input", types.OpLastOne)
			}
		})
	}
	wg.Wait()
}
