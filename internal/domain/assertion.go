package domain

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/crazy-vedic/quark/internal/jsonpath"
)

// Assertion operators.
const (
	OpEquals   = "="
	OpContains = "contains"
	OpExists   = "exists"
	OpLess     = "<"
)

// Assertion kinds.
const (
	AssertStatus   = "status"   // status = 200
	AssertJSONPath = "jsonpath" // $.path exists | $.path = value
	AssertHeader   = "header"   // header Name exists | = value | contains value
	AssertTime     = "time"     // time < 500ms (maximum response time)
)

// Assertion checks one property of a response. Target is the JSONPath for
// jsonpath assertions and the header name for header assertions. Op is "=",
// "contains", "exists" or "<". Value is the expected text ("" for exists).
type Assertion struct {
	Kind   string `json:"kind"`
	Target string `json:"target,omitempty"`
	Op     string `json:"op"`
	Value  string `json:"value,omitempty"`
}

// ParseAssertions decodes the JSON array stored in Request.Assertions. An
// empty string means no assertions.
func ParseAssertions(raw string) ([]Assertion, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var rules []Assertion
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return nil, fmt.Errorf("assertions are not valid JSON: %w", err)
	}
	return rules, nil
}

// EncodeAssertions encodes assertions for Request.Assertions. No assertions
// encode as "[]".
func EncodeAssertions(rules []Assertion) string {
	if len(rules) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(rules)
	return string(b)
}

// ParseAssertionText parses the line format used by the TUI editor and the
// request file. Each non-blank line that does not start with '#' is one rule:
//
//	status = 200
//	time < 500ms
//	$.data.id exists
//	$.data.name = "Ada"
//	header Content-Type contains json
//	header X-Env = prod
//
// Quoted values are unquoted, so the value of "$.name = \"Ada\"" is Ada.
func ParseAssertionText(text string) ([]Assertion, error) {
	var rules []Assertion
	for n, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rule, err := parseAssertionLine(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n+1, err)
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// FormatAssertionText renders one assertion in the format read by
// ParseAssertionText.
func FormatAssertionText(rules []Assertion) string {
	var b strings.Builder
	for _, r := range rules {
		b.WriteString(FormatAssertion(r))
		b.WriteString("\n")
	}
	return b.String()
}

// FormatAssertion renders one assertion as a single line.
func FormatAssertion(r Assertion) string {
	switch r.Kind {
	case AssertStatus:
		return "status = " + r.Value
	case AssertTime:
		return "time < " + r.Value
	case AssertHeader:
		return "header " + r.Target + " " + formatAssertionOp(r)
	default:
		return r.Target + " " + formatAssertionOp(r)
	}
}

func formatAssertionOp(r Assertion) string {
	if r.Op == OpExists {
		return OpExists
	}
	return r.Op + " " + quoteAssertionValue(r.Value)
}

// quoteAssertionValue quotes a value that would not survive a round trip
// through the text format unchanged.
func quoteAssertionValue(v string) string {
	if v == "" || strings.ContainsAny(v, "\"") || v != strings.TrimSpace(v) {
		return strconv.Quote(v)
	}
	return v
}

// ValidateAssertion reports whether r is a complete, supported assertion.
func ValidateAssertion(r Assertion) error {
	switch r.Kind {
	case AssertStatus:
		if r.Op != OpEquals {
			return fmt.Errorf("status supports only '='")
		}
		code, err := strconv.Atoi(r.Value)
		if err != nil || code < 100 || code > 599 {
			return fmt.Errorf("status %q is not an HTTP status code", r.Value)
		}
	case AssertTime:
		if r.Op != OpLess {
			return fmt.Errorf("time supports only '<'")
		}
		d, err := time.ParseDuration(r.Value)
		if err != nil || d <= 0 {
			return fmt.Errorf("time %q is not a positive duration such as 500ms", r.Value)
		}
	case AssertJSONPath, AssertHeader:
		if r.Target == "" {
			return fmt.Errorf("%s assertion has no target", r.Kind)
		}
		if r.Kind == AssertJSONPath {
			if _, err := jsonpath.Parse(r.Target); err != nil {
				return err
			}
		}
		switch r.Op {
		case OpExists:
		case OpEquals, OpContains:
			if r.Kind == AssertHeader && r.Op == OpContains && r.Value == "" {
				return fmt.Errorf("header contains needs a value")
			}
		default:
			return fmt.Errorf("unsupported operator %q", r.Op)
		}
	default:
		return fmt.Errorf("unknown assertion kind %q", r.Kind)
	}
	return nil
}

// parseAssertionLine parses one non-empty, non-comment line.
func parseAssertionLine(line string) (Assertion, error) {
	switch {
	case strings.HasPrefix(line, "status "):
		rest, ok := strings.CutPrefix(strings.TrimSpace(line[len("status"):]), "=")
		if !ok {
			return Assertion{}, fmt.Errorf("expected 'status = <code>'")
		}
		return checkedAssertion(
			Assertion{Kind: AssertStatus, Op: OpEquals, Value: strings.TrimSpace(rest)},
		)
	case strings.HasPrefix(line, "time "):
		rest, ok := strings.CutPrefix(strings.TrimSpace(line[len("time"):]), "<")
		if !ok {
			return Assertion{}, fmt.Errorf("expected 'time < <duration>'")
		}
		return checkedAssertion(
			Assertion{Kind: AssertTime, Op: OpLess, Value: strings.TrimSpace(rest)},
		)
	case strings.HasPrefix(line, "header "):
		name, rest, _ := strings.Cut(strings.TrimSpace(line[len("header"):]), " ")
		op, value, err := parseAssertionOp(strings.TrimSpace(rest))
		if err != nil {
			return Assertion{}, err
		}
		return checkedAssertion(Assertion{Kind: AssertHeader, Target: name, Op: op, Value: value})
	case strings.HasPrefix(line, "$"):
		target, rest, found := splitJSONPathTarget(line)
		if !found {
			return Assertion{}, fmt.Errorf("expected '$.path exists' or '$.path = value'")
		}
		op, value, err := parseAssertionOp(rest)
		if err != nil {
			return Assertion{}, err
		}
		return checkedAssertion(
			Assertion{Kind: AssertJSONPath, Target: target, Op: op, Value: value},
		)
	default:
		return Assertion{}, fmt.Errorf("unknown assertion %q", line)
	}
}

// checkedAssertion validates a parsed rule so the editor and the store never
// hold a rule that cannot be evaluated.
func checkedAssertion(r Assertion) (Assertion, error) {
	if err := ValidateAssertion(r); err != nil {
		return Assertion{}, err
	}
	return r, nil
}

// parseAssertionOp parses "exists", "= value" or "contains value".
func parseAssertionOp(s string) (op, value string, err error) {
	switch {
	case s == OpExists:
		return OpExists, "", nil
	case strings.HasPrefix(s, "="):
		return "=", unquoteAssertionValue(strings.TrimSpace(s[1:])), nil
	case strings.HasPrefix(s, "contains "):
		return OpContains, unquoteAssertionValue(strings.TrimSpace(s[len(OpContains):])), nil
	default:
		return "", "", fmt.Errorf("expected 'exists', '= value' or 'contains value', got %q", s)
	}
}

// splitJSONPathTarget splits "$.path rest" at the first space outside quotes.
func splitJSONPathTarget(line string) (target, rest string, ok bool) {
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == ' ' || c == '\t':
			return line[:i], strings.TrimSpace(line[i:]), true
		}
	}
	return "", "", false
}

func unquoteAssertionValue(v string) string {
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		if s, err := strconv.Unquote(v); err == nil {
			return s
		}
	}
	return v
}
