package metric

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/kedify/otel-add-on/types"
)

type p struct {
}

// enforce iface impl
var _ types.Parser = new(p)

func NewParser() types.Parser {
	return p{}
}

func (p p) Parse(metricQuery string) (types.MetricName, types.LabelMatchers, types.AggregationOverVectors, error) {
	mq := strings.TrimSpace(metricQuery)
	aggregateFunction := types.VecSum
	for _, aggFn := range []types.AggregationOverVectors{types.VecSum, types.VecAvg, types.VecMin, types.VecMax, types.VecCount} {
		prefix := string(aggFn) + "("
		if strings.HasPrefix(mq, prefix) {
			if !strings.HasSuffix(mq, ")") {
				return "", nil, "", fmt.Errorf("unable to parse metric query: %s", metricQuery)
			}
			aggregateFunction = aggFn
			mq = strings.TrimSpace(mq[len(prefix) : len(mq)-1])
			break
		}
	}

	name, labelQuery, hasLabels := strings.Cut(mq, "{")
	name = strings.TrimSpace(name)
	if name == "" || strings.IndexFunc(name, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("{}(),=!~\"'`[]", r)
	}) >= 0 {
		return "", nil, "", fmt.Errorf("unable to parse metric query: %s", metricQuery)
	}
	if !hasLabels {
		return types.MetricName(name), nil, aggregateFunction, nil
	}
	if !strings.HasSuffix(labelQuery, "}") {
		return "", nil, "", fmt.Errorf("unable to parse metric query: %s", metricQuery)
	}
	matchers, err := p.ParseLabels(labelQuery[:len(labelQuery)-1])
	if err != nil {
		return "", nil, "", err
	}
	return types.MetricName(name), matchers, aggregateFunction, nil
}

func (p p) ParseLabels(labelsQuery string) (types.LabelMatchers, error) {
	remaining := strings.TrimSpace(labelsQuery)
	if remaining == "" {
		return nil, fmt.Errorf("unable to parse labels: %s", labelsQuery)
	}

	var matchers types.LabelMatchers
	for remaining != "" {
		var name string
		var err error
		if isQuote(remaining[0]) {
			name, remaining, err = readQuotedLabel(remaining)
			if err != nil {
				return nil, err
			}
		} else {
			end := strings.IndexFunc(remaining, func(r rune) bool {
				return unicode.IsSpace(r) || strings.ContainsRune("=!~<>,{}()\"'`", r)
			})
			if end < 0 {
				return nil, fmt.Errorf("missing label matching operator in %q", remaining)
			}
			name, remaining = remaining[:end], remaining[end:]
		}
		if name == "" {
			return nil, fmt.Errorf("label name must not be empty in %q", labelsQuery)
		}

		remaining = strings.TrimSpace(remaining)
		var operator types.MatchOperator
		for _, op := range []types.MatchOperator{types.MatchNotEqual, types.MatchNotRegexp, types.MatchRegexp, types.MatchEqual} {
			if strings.HasPrefix(remaining, string(op)) {
				operator = op
				remaining = strings.TrimSpace(remaining[len(op):])
				break
			}
		}
		if operator == "" {
			return nil, fmt.Errorf("invalid label matching operator after %q", name)
		}
		if remaining == "" {
			return nil, fmt.Errorf("missing value for label %q", name)
		}

		var value string
		if isQuote(remaining[0]) {
			value, remaining, err = readQuotedLabel(remaining)
			if err != nil {
				return nil, err
			}
		} else {
			// Retain the original unquoted shorthand for simple values.
			var comma bool
			value, remaining, comma = strings.Cut(remaining, ",")
			if comma {
				remaining = "," + remaining
			}
			value = strings.TrimSpace(value)
			if value == "" || strings.IndexFunc(value, func(r rune) bool {
				return unicode.IsSpace(r) || strings.ContainsRune("=!~{}()\"'`", r)
			}) >= 0 {
				return nil, fmt.Errorf("invalid unquoted value for label %q: %q", name, value)
			}
		}
		matcher, err := types.NewLabelMatcher(name, operator, value)
		if err != nil {
			return nil, err
		}
		matchers = append(matchers, matcher)

		remaining = strings.TrimSpace(remaining)
		if remaining == "" {
			break
		}
		if remaining[0] != ',' {
			return nil, fmt.Errorf("expected comma after label %q, got %q", name, remaining)
		}
		remaining = strings.TrimSpace(remaining[1:])
	}
	return matchers, nil
}

func isQuote(c byte) bool {
	return c == '\'' || c == '"' || c == '`'
}

// readQuotedLabel follows PromQL's Go-style escapes for single/double quotes.
// Backtick strings retain all characters, including newlines and backslashes.
func readQuotedLabel(input string) (string, string, error) {
	quote := input[0]
	remaining := input[1:]
	if quote == '`' {
		if end := strings.IndexByte(remaining, quote); end >= 0 {
			return remaining[:end], remaining[end+1:], nil
		}
	} else {
		var value strings.Builder
		for remaining != "" {
			if remaining[0] == quote {
				return value.String(), remaining[1:], nil
			}
			if remaining[0] == '\n' {
				return "", "", fmt.Errorf("unescaped newline in label string %q", input)
			}
			c, multibyte, tail, err := strconv.UnquoteChar(remaining, quote)
			if err != nil {
				return "", "", fmt.Errorf("invalid label string %q: %w", input, err)
			}
			if multibyte {
				value.WriteRune(c)
			} else {
				value.WriteByte(byte(c))
			}
			remaining = tail
		}
	}
	return "", "", fmt.Errorf("unterminated label string %q", input)
}
