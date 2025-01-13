package metric

import (
	"fmt"
	"testing"

	"github.com/kedify/otel-add-on/types"
)

func TestSimpleParserOk(t *testing.T) {
	// setup
	p := NewParser()

	// check
	name, labels, agg, err := p.Parse("avg(metric_foo_bar{a=1, b=2})")
	if name != "metric_foo_bar" || fmt.Sprint(labels) != fmt.Sprint(map[string]any{"b": "2", "a": "1"}) || agg != types.VecAvg || err != nil {
		t.Errorf("expected: [metric_foo_bar, map[a:1 b:2], avg, <nil>], got: [%s, %v, %v, %v]", name, labels, agg, err)
	}

	// check
	name2, labels2, agg2, err2 := p.Parse("avg(metric_foo_bar{a=\"1\", b=\"2\"})")
	if name2 != "metric_foo_bar" || fmt.Sprint(labels2) != fmt.Sprint(map[string]any{"b": "2", "a": "1"}) || agg2 != types.VecAvg || err2 != nil {
		t.Errorf("expected: [metric_foo_bar, map[a:1 b:2], avg, <nil>], got: [%s, %v, %v, %v]", name2, labels2, agg2, err2)
	}
}

func TestSimpleParserFail(t *testing.T) {
	// setup
	p := NewParser()

	// check 1
	_, _, _, err := p.Parse("avg(metric_foo_bar{a=1, b=2=2})")
	if err == nil {
		t.Errorf("expected: _, _, _, err], got: [_, _, _, %v]", err)
	}

	// check 2
	_, _, _, err = p.Parse("avg(metric_foo_bar{a=1, b=2)")
	if err == nil {
		t.Errorf("expected: _, _, _, err], got: [_, _, _, %v]", err)
	}

	// check 3
	_, _, _, err = p.Parse("metric_foo_bara=1, b=2})")
	if err == nil {
		t.Errorf("expected: _, _, _, err], got: [_, _, _, %v]", err)
	}

	// check 4
	_, _, _, err = p.Parse("avg(metric_foo_bar{a:1})")
	if err == nil {
		t.Errorf("expected: _, _, _, err], got: [_, _, _, %v]", err)
	}

	// check 5
	_, _, _, err = p.Parse("metric_foo_bar{}")
	if err == nil {
		t.Errorf("expected: _, _, _, err], got: [_, _, _, %v]", err)
	}
}
func TestSimpleParserDefaultAgg(t *testing.T) {
	// setup
	p := NewParser()

	// check
	name, labels, agg, err := p.Parse("metric_foo{a=1, b=2, c=5}")
	if name != "metric_foo" || fmt.Sprint(labels) != fmt.Sprint(map[string]any{"b": "2", "a": "1", "c": "5"}) || agg != types.VecSum || err != nil {
		t.Errorf("expected: [metric_foo, map[a:1 b:2 c:5], avg, <nil>], got: [%s, %v, %v, %v]", name, labels, agg, err)
	}
}

func TestSimpleParserMin(t *testing.T) {
	// setup
	p := NewParser()

	// check
	name, labels, agg, err := p.Parse("min(metric_foo{ahoj=cau})")
	if name != "metric_foo" || fmt.Sprint(labels) != fmt.Sprint(map[string]any{"ahoj": "cau"}) || agg != types.VecMin || err != nil {
		t.Errorf("expected: [metric_foo, map[ahoj:cau], min, <nil>], got: [%s, %v, %v, %v]", name, labels, agg, err)
	}
}

func TestSimpleParserCount(t *testing.T) {
	// setup
	p := NewParser()

	// check
	name, labels, agg, err := p.Parse("count(metric_foo{ahoj=cau})")
	if name != "metric_foo" || fmt.Sprint(labels) != fmt.Sprint(map[string]any{"ahoj": "cau"}) || agg != types.VecCount || err != nil {
		t.Errorf("expected: [metric_foo, map[ahoj:cau], min, <nil>], got: [%s, %v, %v, %v]", name, labels, agg, err)
	}
}

func TestSimpleParserNoLabels(t *testing.T) {
	// setup
	p := NewParser()

	// check
	name, labels, agg, err := p.Parse("max(metric_foo)")
	if name != "metric_foo" || fmt.Sprint(labels) != fmt.Sprint(map[string]any{}) || agg != types.VecMax || err != nil {
		t.Errorf("expected: [metric_foo, map[], max, <nil>], got: [%s, %v, %v, %v]", name, labels, agg, err)
	}
}

func TestSimpleParserNoLabelsNoAgg(t *testing.T) {
	// setup
	p := NewParser()

	// check
	name, labels, agg, err := p.Parse("hello")
	if name != "hello" || fmt.Sprint(labels) != fmt.Sprint(map[string]any{}) || agg != types.VecSum || err != nil {
		t.Errorf("expected: [hello, map[], sum, <nil>], got: [%s, %v, %v, %v]", name, labels, agg, err)
	}
}
