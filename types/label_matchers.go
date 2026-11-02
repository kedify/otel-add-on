package types

import (
	"fmt"
	"regexp"
	"regexp/syntax"
)

// MatchOperator is a PromQL label matching operator.
type MatchOperator string

const (
	MatchEqual     MatchOperator = "="
	MatchNotEqual  MatchOperator = "!="
	MatchRegexp    MatchOperator = "=~"
	MatchNotRegexp MatchOperator = "!~"
)

// LabelMatcher is a label constraint. Construct it with NewLabelMatcher so regexes
// are validated and compiled before querying the store.
type LabelMatcher struct {
	Name     string
	Operator MatchOperator
	Value    string
	regex    *regexp.Regexp
}

func NewLabelMatcher(name string, operator MatchOperator, value string) (LabelMatcher, error) {
	m := LabelMatcher{Name: name, Operator: operator, Value: value}
	switch operator {
	case MatchEqual, MatchNotEqual:
	case MatchRegexp, MatchNotRegexp:
		// Parse the pattern on its own first, so unmatched parentheses cannot
		// escape the enclosing group and bypass full-string anchoring.
		parsed, err := syntax.Parse(value, syntax.Perl|syntax.DotNL)
		if err != nil {
			return LabelMatcher{}, fmt.Errorf("invalid regex for label %q: %w", name, err)
		}
		m.regex, err = regexp.Compile("^(?s:" + parsed.String() + ")$")
		if err != nil {
			return LabelMatcher{}, fmt.Errorf("invalid regex for label %q: %w", name, err)
		}
	default:
		return LabelMatcher{}, fmt.Errorf("unknown label matching operator: %q", operator)
	}
	return m, nil
}

func (m LabelMatcher) Matches(value string) bool {
	switch m.Operator {
	case MatchEqual:
		return value == m.Value
	case MatchNotEqual:
		return value != m.Value
	case MatchRegexp:
		return m.regex.MatchString(value)
	case MatchNotRegexp:
		return !m.regex.MatchString(value)
	default:
		return false
	}
}

func (m LabelMatcher) String() string {
	return fmt.Sprintf("%s%s%q", m.Name, m.Operator, m.Value)
}

// LabelMatchers preserves repeated constraints on the same label. All must pass.
type LabelMatchers []LabelMatcher

func (matchers LabelMatchers) Matches(labels Labels) bool {
	for _, matcher := range matchers {
		value := ""
		if v, found := labels[matcher.Name]; found {
			value = fmt.Sprint(v)
		}
		if !matcher.Matches(value) {
			return false
		}
	}
	return true
}
