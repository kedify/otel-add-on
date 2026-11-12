package metric

import (
	"testing"

	"github.com/kedify/otel-add-on/types"
)

func TestSimpleParser(t *testing.T) {
	tests := []struct {
		query    string
		name     types.MetricName
		agg      types.AggregationOverVectors
		matchers types.LabelMatchers
	}{
		{"avg(metric_foo_bar{a=1, b=2})", "metric_foo_bar", types.VecAvg, types.LabelMatchers{
			{Name: "a", Operator: types.MatchEqual, Value: "1"}, {Name: "b", Operator: types.MatchEqual, Value: "2"},
		}},
		{`avg(metric_foo_bar{a="1", b="2"})`, "metric_foo_bar", types.VecAvg, types.LabelMatchers{
			{Name: "a", Operator: types.MatchEqual, Value: "1"}, {Name: "b", Operator: types.MatchEqual, Value: "2"},
		}},
		{"metric_foo{a=1, b=2, c=5}", "metric_foo", types.VecSum, types.LabelMatchers{
			{Name: "a", Operator: types.MatchEqual, Value: "1"}, {Name: "b", Operator: types.MatchEqual, Value: "2"}, {Name: "c", Operator: types.MatchEqual, Value: "5"},
		}},
		{"min(metric_foo{ahoj=cau})", "metric_foo", types.VecMin, types.LabelMatchers{{Name: "ahoj", Operator: types.MatchEqual, Value: "cau"}}},
		{"count(metric_foo{ahoj=cau})", "metric_foo", types.VecCount, types.LabelMatchers{{Name: "ahoj", Operator: types.MatchEqual, Value: "cau"}}},
		{"max(metric_foo)", "metric_foo", types.VecMax, nil},
		{"hello", "hello", types.VecSum, nil},
		{`queue_depth{queue=~"orders-.*",environment!="dev"}`, "queue_depth", types.VecSum, types.LabelMatchers{
			{Name: "queue", Operator: types.MatchRegexp, Value: "orders-.*"}, {Name: "environment", Operator: types.MatchNotEqual, Value: "dev"},
		}},
		{`sum(queue_depth{queue!~"test|dev",queue!="orders-dev",queue!="orders-test"})`, "queue_depth", types.VecSum, types.LabelMatchers{
			{Name: "queue", Operator: types.MatchNotRegexp, Value: "test|dev"},
			{Name: "queue", Operator: types.MatchNotEqual, Value: "orders-dev"}, {Name: "queue", Operator: types.MatchNotEqual, Value: "orders-test"},
		}},
		{`metric{key="a,b=c{}()!~",empty="",spaces=" x "}`, "metric", types.VecSum, types.LabelMatchers{
			{Name: "key", Operator: types.MatchEqual, Value: "a,b=c{}()!~"}, {Name: "empty", Operator: types.MatchEqual, Value: ""}, {Name: "spaces", Operator: types.MatchEqual, Value: " x "},
		}},
		{`metric{key="quote\" slash\\ newline\n tab\t \x41\u00e9\101"}`, "metric", types.VecSum, types.LabelMatchers{
			{Name: "key", Operator: types.MatchEqual, Value: "quote\" slash\\ newline\n tab\t AéA"},
		}},
		{`metric{key='it\'s a string',"http.method"='GET'}`, "metric", types.VecSum, types.LabelMatchers{
			{Name: "key", Operator: types.MatchEqual, Value: "it's a string"}, {Name: "http.method", Operator: types.MatchEqual, Value: "GET"},
		}},
		{"metric{key=`a\\b\r\nc`}", "metric", types.VecSum, types.LabelMatchers{{Name: "key", Operator: types.MatchEqual, Value: "a\\b\r\nc"}}},
		{`metric{key=~"orders-[0-9]{1,3}\\.ready"}`, "metric", types.VecSum, types.LabelMatchers{
			{Name: "key", Operator: types.MatchRegexp, Value: `orders-[0-9]{1,3}\.ready`},
		}},
		{` avg( metric_name_test { queue =~ "orders-(a|b)", environment != 'dev', } ) `, "metric_name_test", types.VecAvg, types.LabelMatchers{
			{Name: "queue", Operator: types.MatchRegexp, Value: "orders-(a|b)"}, {Name: "environment", Operator: types.MatchNotEqual, Value: "dev"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			name, matchers, agg, err := parseSelector(tt.query)
			if err != nil {
				t.Fatal(err)
			}
			if name != tt.name || agg != tt.agg || len(matchers) != len(tt.matchers) {
				t.Fatalf("got (%s, %v, %s), want (%s, %v, %s)", name, matchers, agg, tt.name, tt.matchers, tt.agg)
			}
			for i, want := range tt.matchers {
				got := matchers[i]
				if got.Name != want.Name || got.Operator != want.Operator || got.Value != want.Value {
					t.Errorf("matcher %d: got %v, want %v", i, got, want)
				}
			}
		})
	}
}

func TestSimpleParserFail(t *testing.T) {
	for _, query := range []string{
		"", "  ", "avg(metric_foo_bar{a=1, b=2=2})", "avg(metric_foo_bar{a=1, b=2)",
		"metric_foo_bara=1, b=2})", "avg(metric_foo_bar{a:1})", "metric_foo_bar{}",
		"metric{a=}", `metric{a=="1"}`, `metric{a!="1",,b="2"}`, `metric{a!"1"}`,
		`metric{a~="1"}`, `metric{a=!"1"}`, `metric{a>="1"}`, `metric{a<="1"}`, `metric{a="1" b="2"}`, `metric{a="1"junk}`,
		`metric{a="unterminated}`, `metric{a='unterminated}`, "metric{a=`unterminated}",
		`metric{a="\q"}`, "metric{a=\"new\nline\"}", `metric{a=~"["}`, `metric{a!~"("}`,
		`metric{a=~")|("}`, `metric{a=~"(?=lookahead)"}`, `metric{a=~"(a)\\1"}`,
		`metric{="value"}`, `metric{""="value"}`, `{a="1"}`, `metric{a="1"}junk`,
		`metric{a="1"}}`, `metric{a="1"}{b="2"}`, "unknown(metric)", "metric[5m]",
	} {
		t.Run(query, func(t *testing.T) {
			if _, err := NewParser().Parse(query); err == nil {
				t.Fatalf("expected parse error for %q", query)
			}
		})
	}
}

// Inspect simple selector compilation while retaining the label-matcher regressions.
func parseSelector(input string) (types.MetricName, types.LabelMatchers, types.AggregationOverVectors, error) {
	query, err := NewParser().Parse(input)
	if err != nil {
		return "", nil, "", err
	}
	node := query.(*compiledQuery).root
	agg := types.VecSum
	if node.op != "selector" {
		agg = types.AggregationOverVectors(node.op)
		node = node.args[0]
	}
	return node.name, node.matchers, agg, nil
}
