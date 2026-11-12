package metric

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/kedify/otel-add-on/types"
)

type valueType uint8

const (
	scalarType valueType = iota
	vectorType
)

type expression struct {
	op       string
	kind     valueType
	number   float64
	name     types.MetricName
	matchers types.LabelMatchers
	args     []*expression
}

type compiledQuery struct {
	root  *expression
	names []types.MetricName
}

type expressionParser struct {
	input string
	pos   int
	names []types.MetricName
}

// Bound both recursive parsing and evaluation without tracking expression depth.
const maxQueryLength = 4096

func (p parser) Parse(input string) (types.Query, error) {
	if len(input) > maxQueryLength {
		return nil, fmt.Errorf("metric query exceeds maximum length of %d bytes", maxQueryLength)
	}
	parser := expressionParser{input: input}
	root, err := parser.parseBinary(1)
	if err != nil {
		return nil, err
	}
	parser.space()
	if parser.pos != len(input) {
		return nil, parser.unexpected()
	}
	return &compiledQuery{root: root, names: parser.names}, nil
}

func (p *expressionParser) space() {
	remaining := p.input[p.pos:]
	p.pos += len(remaining) - len(strings.TrimLeftFunc(remaining, unicode.IsSpace))
}

func (p *expressionParser) consume(c byte) bool {
	p.space()
	if p.pos < len(p.input) && p.input[p.pos] == c {
		p.pos++
		return true
	}
	return false
}

func (p *expressionParser) unexpected() error {
	tail := p.input[p.pos:]
	switch {
	case tail == "":
		return fmt.Errorf("invalid metric query: expected expression at end of input")
	case strings.HasPrefix(tail, "["):
		return fmt.Errorf("range selectors and subqueries are unsupported (position %d)", p.pos)
	case strings.ContainsAny(tail[:1], "<>=!"):
		return fmt.Errorf("comparisons are unsupported (position %d)", p.pos)
	case strings.HasPrefix(tail, "offset"), strings.HasPrefix(tail, "@"):
		return fmt.Errorf("offsets and @ modifiers are unsupported (position %d)", p.pos)
	case strings.HasPrefix(tail, "by"), strings.HasPrefix(tail, "without"),
		strings.HasPrefix(tail, "on"), strings.HasPrefix(tail, "ignoring"), strings.HasPrefix(tail, "group_"):
		return fmt.Errorf("grouping and vector matching modifiers are unsupported (position %d)", p.pos)
	default:
		return fmt.Errorf("invalid metric query: unexpected input %q at position %d", tail, p.pos)
	}
}

func precedence(op string) int {
	switch op {
	case "or":
		return 1
	case "+", "-":
		return 2
	case "*", "/", "%":
		return 3
	case "^":
		return 4
	default:
		return 0
	}
}

func (p *expressionParser) binaryOperator() string {
	tail := p.input[p.pos:]
	if tail == "" {
		return ""
	}
	if strings.HasPrefix(tail, "or") && (len(tail) == 2 || !isIdentifierPart(tail[2])) {
		return "or"
	}
	return tail[:1]
}

func (p *expressionParser) parseBinary(minPrecedence int) (*expression, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		p.space()
		op := p.binaryOperator()
		if precedence(op) < minPrecedence {
			return left, nil
		}
		p.pos += len(op)
		next := precedence(op) + 1
		if op == "^" { // Exponentiation is right associative.
			next--
		}
		right, err := p.parseBinary(next)
		if err != nil {
			return nil, err
		}
		if op == "or" && (left.kind != vectorType || right.kind != vectorType) {
			return nil, fmt.Errorf("or operands must be vectors")
		}
		kind := scalarType
		if left.kind == vectorType || right.kind == vectorType {
			kind = vectorType
		}
		left = &expression{op: op, kind: kind, args: []*expression{left, right}}
	}
}

func (p *expressionParser) parseUnary() (*expression, error) {
	p.space()
	if p.pos < len(p.input) && (p.input[p.pos] == '+' || p.input[p.pos] == '-') {
		op := p.input[p.pos]
		p.pos++
		// PromQL binds exponentiation more tightly than unary signs.
		arg, err := p.parseBinary(4)
		if err != nil {
			return nil, err
		}
		return &expression{op: "unary" + string(op), kind: arg.kind, args: []*expression{arg}}, nil
	}
	return p.parsePrimary()
}

func (p *expressionParser) parsePrimary() (*expression, error) {
	if p.consume('(') {
		inner, err := p.parseBinary(1)
		if err != nil {
			return nil, err
		}
		if !p.consume(')') {
			return nil, fmt.Errorf("expected closing parenthesis at position %d: %w", p.pos, p.unexpected())
		}
		return inner, nil
	}
	p.space()
	if p.pos == len(p.input) {
		return nil, p.unexpected()
	}
	c := p.input[p.pos]
	if isDigit(c) || c == '.' {
		return p.parseNumber()
	}
	if !isIdentifierStart(c) {
		return nil, p.unexpected()
	}
	start := p.pos
	for p.pos < len(p.input) && isIdentifierPart(p.input[p.pos]) {
		p.pos++
	}
	name := p.input[start:p.pos]
	if p.consume('(') {
		return p.parseCall(name)
	}
	var matchers types.LabelMatchers
	if p.consume('{') {
		start := p.pos
		for p.pos < len(p.input) && p.input[p.pos] != '}' {
			if isQuote(p.input[p.pos]) {
				_, remaining, err := readQuotedLabel(p.input[p.pos:])
				if err != nil {
					return nil, err
				}
				p.pos = len(p.input) - len(remaining)
			} else {
				p.pos++
			}
		}
		if p.pos == len(p.input) {
			return nil, fmt.Errorf("unterminated label selector for %q", name)
		}
		var err error
		matchers, err = (parser{}).ParseLabels(p.input[start:p.pos])
		if err != nil {
			return nil, err
		}
		p.pos++
	}
	metricName := escapeName(types.MetricName(name))
	if !slices.Contains(p.names, metricName) {
		p.names = append(p.names, metricName)
	}
	return &expression{op: "selector", kind: vectorType, name: metricName, matchers: matchers}, nil
}

func (p *expressionParser) parseNumber() (*expression, error) {
	start := p.pos
	for p.pos < len(p.input) && isDigit(p.input[p.pos]) {
		p.pos++
	}
	if p.pos < len(p.input) && p.input[p.pos] == '.' {
		p.pos++
		for p.pos < len(p.input) && isDigit(p.input[p.pos]) {
			p.pos++
		}
	}
	if p.pos < len(p.input) && (p.input[p.pos] == 'e' || p.input[p.pos] == 'E') {
		p.pos++
		if p.pos < len(p.input) && (p.input[p.pos] == '+' || p.input[p.pos] == '-') {
			p.pos++
		}
		for p.pos < len(p.input) && isDigit(p.input[p.pos]) {
			p.pos++
		}
	}
	number, err := strconv.ParseFloat(p.input[start:p.pos], 64)
	if err != nil {
		return nil, fmt.Errorf("invalid numeric literal at position %d: %w", start, err)
	}
	return &expression{op: "number", kind: scalarType, number: number}, nil
}

func (p *expressionParser) parseCall(name string) (*expression, error) {
	var arity int
	switch name {
	case "ln", "log2", "scalar", "vector", "sum", "avg", "min", "max", "count":
		arity = 1
	case "clamp_min", "clamp_max":
		arity = 2
	default:
		return nil, fmt.Errorf("unsupported function %q", name)
	}
	var args []*expression
	if !p.consume(')') {
		for {
			arg, err := p.parseBinary(1)
			if err != nil {
				return nil, err
			}
			args = append(args, arg)
			if p.consume(')') {
				break
			}
			if !p.consume(',') {
				return nil, fmt.Errorf("expected comma or closing parenthesis in %s: %w", name, p.unexpected())
			}
		}
	}
	if len(args) != arity {
		return nil, fmt.Errorf("%s expects %d argument(s), got %d", name, arity, len(args))
	}
	if name == "vector" {
		if args[0].kind != scalarType {
			return nil, fmt.Errorf("%s argument 1 must be a scalar", name)
		}
	} else if args[0].kind != vectorType {
		return nil, fmt.Errorf("%s argument 1 must be a vector", name)
	}
	if arity == 2 && args[1].kind != scalarType {
		return nil, fmt.Errorf("%s argument 2 must be a scalar", name)
	}
	kind := vectorType
	if name == "scalar" {
		kind = scalarType
	}
	return &expression{op: name, kind: kind, args: args}, nil
}

func isIdentifierStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c == ':'
}

func isIdentifierPart(c byte) bool { return isIdentifierStart(c) || isDigit(c) || c == '.' }

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
