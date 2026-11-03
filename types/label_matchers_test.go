package types

import "testing"

func TestLabelMatchers(t *testing.T) {
	// The same matcher must behave identically for absent and explicitly empty labels.
	labelSets := []Labels{nil, {"env": ""}, {"env": "dev"}, {"env": "prod"}, {"env": "xprod"}, {"env": "prod\n"}}
	tests := []struct {
		op      MatchOperator
		value   string
		matches []bool
	}{
		{MatchEqual, "dev", []bool{false, false, true, false, false, false}},
		{MatchEqual, "", []bool{true, true, false, false, false, false}},
		{MatchNotEqual, "dev", []bool{true, true, false, true, true, true}},
		{MatchNotEqual, "", []bool{false, false, true, true, true, true}},
		{MatchRegexp, "prod", []bool{false, false, false, true, false, false}},
		{MatchRegexp, "dev|prod", []bool{false, false, true, true, false, false}},
		{MatchRegexp, ".*", []bool{true, true, true, true, true, true}},
		{MatchRegexp, ".+", []bool{false, false, true, true, true, true}},
		{MatchRegexp, "", []bool{true, true, false, false, false, false}},
		{MatchNotRegexp, "dev", []bool{true, true, false, true, true, true}},
		{MatchNotRegexp, ".*", []bool{false, false, false, false, false, false}},
		{MatchNotRegexp, ".+", []bool{true, true, false, false, false, false}},
		{MatchNotRegexp, "", []bool{false, false, true, true, true, true}},
		{MatchRegexp, "(?-s:.*)", []bool{true, true, true, true, true, false}},
		{MatchRegexp, "(?m:prod$)", []bool{false, false, false, true, false, false}},
	}
	for _, tt := range tests {
		t.Run(string(tt.op)+tt.value, func(t *testing.T) {
			matcher, err := NewLabelMatcher("env", tt.op, tt.value)
			if err != nil {
				t.Fatal(err)
			}
			for i, labels := range labelSets {
				if got := (LabelMatchers{matcher}).Matches(labels); got != tt.matches[i] {
					t.Errorf("%v against %v: got %v, want %v", matcher, labels, got, tt.matches[i])
				}
			}
		})
	}
}

func TestLabelMatchersScalarValues(t *testing.T) {
	for _, tt := range []struct {
		value any
		text  string
	}{{int64(42), "42"}, {true, "true"}, {1.5, "1.5"}, {"orders", "orders"}} {
		matcher, err := NewLabelMatcher("value", MatchEqual, tt.text)
		if err != nil {
			t.Fatal(err)
		}
		if !(LabelMatchers{matcher}).Matches(Labels{"value": tt.value}) {
			t.Errorf("%v did not match %v", matcher, tt.value)
		}
	}
	if !(LabelMatchers(nil)).Matches(Labels{"anything": "value"}) {
		t.Fatal("empty matcher list must match all labels")
	}
}

func TestLabelMatchersRepeatedConstraints(t *testing.T) {
	var matchers LabelMatchers
	for _, value := range []string{"dev", "test"} {
		matcher, err := NewLabelMatcher("env", MatchNotEqual, value)
		if err != nil {
			t.Fatal(err)
		}
		matchers = append(matchers, matcher)
	}
	for _, env := range []string{"dev", "test", "prod"} {
		if got, want := matchers.Matches(Labels{"env": env}), env == "prod"; got != want {
			t.Errorf("env=%s: got %v, want %v", env, got, want)
		}
	}
}

func TestLabelMatcherInvalidOperator(t *testing.T) {
	if _, err := NewLabelMatcher("env", "==", "prod"); err == nil {
		t.Fatal("expected error for unsupported operator")
	}
}
