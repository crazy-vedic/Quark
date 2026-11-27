package exec

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/jsonpath"
)

// AssertionResult reports whether one assertion of a request held.
type AssertionResult struct {
	Rule   string // the assertion in text form, see domain.FormatAssertion
	Passed bool
	Reason string // why it failed; empty when it passed
}

// AssertionHook is a PostResponseHook that evaluates a request's assertions
// against the response and appends the results to ExecuteResult.Assertions.
// Assertions never fail the request itself; callers decide what a failure
// means (quark run exits non-zero, the TUI shows the result).
type AssertionHook struct{}

// NewAssertionHook returns the assertion hook.
func NewAssertionHook() *AssertionHook { return &AssertionHook{} }

var _ PostResponseHook = (*AssertionHook)(nil)

// AfterResponse implements PostResponseHook.
func (*AssertionHook) AfterResponse(
	_ context.Context,
	req *domain.Request,
	res *ExecuteResult,
) error {
	if req == nil || res == nil {
		return nil
	}
	rules, err := domain.ParseAssertions(req.Assertions)
	if err != nil {
		res.Assertions = append(res.Assertions, AssertionResult{
			Rule:   "assertions",
			Reason: err.Error(),
		})
		return nil
	}
	res.Assertions = append(res.Assertions, EvaluateAssertions(rules, res)...)
	return nil
}

// EvaluateAssertions checks each rule against res, in order. The body is
// decoded at most once, and only when a JSONPath assertion needs it. Failure
// reasons never echo response values, because they are often credentials.
func EvaluateAssertions(rules []domain.Assertion, res *ExecuteResult) []AssertionResult {
	out := make([]AssertionResult, 0, len(rules))
	var doc any
	var docErr error
	docLoaded := false
	for _, rule := range rules {
		r := AssertionResult{Rule: domain.FormatAssertion(rule)}
		if err := domain.ValidateAssertion(rule); err != nil {
			r.Reason = err.Error()
			out = append(out, r)
			continue
		}
		switch rule.Kind {
		case domain.AssertStatus:
			r.Passed, r.Reason = checkStatus(rule, res)
		case domain.AssertTime:
			r.Passed, r.Reason = checkTime(rule, res)
		case domain.AssertHeader:
			r.Passed, r.Reason = checkHeader(rule, res)
		case domain.AssertJSONPath:
			if !docLoaded {
				docLoaded = true
				doc, docErr = decodeResultBody(res)
			}
			if docErr != nil {
				r.Reason = "response body is not JSON"
				break
			}
			r.Passed, r.Reason = checkJSONPath(rule, doc)
		}
		out = append(out, r)
	}
	return out
}

func checkStatus(rule domain.Assertion, res *ExecuteResult) (bool, string) {
	var want int
	_, _ = fmt.Sscanf(rule.Value, "%d", &want)
	if res.StatusCode == want {
		return true, ""
	}
	return false, fmt.Sprintf("status is %d", res.StatusCode)
}

func checkTime(rule domain.Assertion, res *ExecuteResult) (bool, string) {
	limit, _ := time.ParseDuration(rule.Value)
	if res.Duration < limit {
		return true, ""
	}
	return false, fmt.Sprintf("took %v, limit %v", res.Duration.Round(time.Millisecond), limit)
}

func checkHeader(rule domain.Assertion, res *ExecuteResult) (bool, string) {
	vals := http.Header(res.Headers).Values(rule.Target)
	if len(vals) == 0 {
		return false, fmt.Sprintf("header %s is missing", rule.Target)
	}
	joined := strings.Join(vals, ", ")
	switch rule.Op {
	case "exists":
		return true, ""
	case "=":
		if joined == rule.Value {
			return true, ""
		}
		return false, fmt.Sprintf("header %s has a different value", rule.Target)
	default: // contains
		if strings.Contains(joined, rule.Value) {
			return true, ""
		}
		return false, fmt.Sprintf("header %s does not contain the expected text", rule.Target)
	}
}

func checkJSONPath(rule domain.Assertion, doc any) (bool, string) {
	path, err := jsonpath.Parse(rule.Target)
	if err != nil {
		return false, err.Error()
	}
	v, err := path.Lookup(doc)
	if err != nil {
		return false, fmt.Sprintf("no value at %s", rule.Target)
	}
	if rule.Op == "exists" {
		return true, ""
	}
	if jsonpath.Text(v) == rule.Value {
		return true, ""
	}
	return false, fmt.Sprintf("%s has a different value", rule.Target)
}
