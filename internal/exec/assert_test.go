package exec

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

func assertionResponse() *ExecuteResult {
	return &ExecuteResult{
		StatusCode: 200,
		Status:     "200 OK",
		Headers: http.Header{
			"Content-Type": {"application/json; charset=utf-8"},
			"X-Env":        {"prod"},
		},
		Body:     []byte(`{"data":{"id":7,"name":"Ada","tags":null}}`),
		Duration: 120 * time.Millisecond,
	}
}

func TestEvaluateAssertionsPassesAndFails(t *testing.T) {
	rules, err := domain.ParseAssertionText(`
status = 200
status = 404
time < 500ms
time < 100ms
$.data.id exists
$.data.missing exists
$.data.name = "Ada"
$.data.id = 8
$.data.tags = null
header Content-Type contains json
header X-Env = prod
header X-Env = dev
header X-Missing exists
`)
	require.NoError(t, err)
	res := assertionResponse()
	got := EvaluateAssertions(rules, res)
	require.Len(t, got, len(rules))

	want := []bool{
		true,
		false,
		true,
		false,
		true,
		false,
		true,
		false,
		true,
		true,
		true,
		false,
		false,
	}
	for i, w := range want {
		assert.Equal(t, w, got[i].Passed, got[i].Rule)
		if w {
			assert.Empty(t, got[i].Reason, got[i].Rule)
		} else {
			assert.NotEmpty(t, got[i].Reason, got[i].Rule)
		}
	}
	assert.Equal(t, "status = 404", got[1].Rule)
	assert.Equal(t, "status is 200", got[1].Reason)
	assert.Equal(t, "header X-Missing is missing", got[12].Reason)
}

func TestEvaluateAssertionsReasonsDoNotEchoValues(t *testing.T) {
	rules := []domain.Assertion{
		{Kind: domain.AssertJSONPath, Target: "$.data.name", Op: "=", Value: "wrong"},
		{Kind: domain.AssertHeader, Target: "X-Env", Op: "=", Value: "dev"},
	}
	got := EvaluateAssertions(rules, assertionResponse())
	for _, r := range got {
		assert.False(t, r.Passed)
		assert.NotContains(t, r.Reason, "Ada")
		assert.NotContains(t, r.Reason, "prod")
	}
}

func TestEvaluateAssertionsJSONBodyRequired(t *testing.T) {
	res := assertionResponse()
	res.Body = []byte("plain text")
	got := EvaluateAssertions([]domain.Assertion{
		{Kind: domain.AssertJSONPath, Target: "$.a", Op: "exists"},
	}, res)
	require.Len(t, got, 1)
	assert.False(t, got[0].Passed)
	assert.Equal(t, "response body is not JSON", got[0].Reason)
}

func TestEvaluateAssertionsRejectsInvalidRule(t *testing.T) {
	got := EvaluateAssertions([]domain.Assertion{
		{Kind: domain.AssertStatus, Op: "=", Value: "abc"},
	}, assertionResponse())
	require.Len(t, got, 1)
	assert.False(t, got[0].Passed)
	assert.Contains(t, got[0].Reason, "not an HTTP status code")
}

func TestAssertionHookAppendsResultsAndReportsBadStoredRules(t *testing.T) {
	hook := NewAssertionHook()
	res := assertionResponse()

	req := &domain.Request{Assertions: `[{"kind":"status","op":"=","value":"200"}]`}
	require.NoError(t, hook.AfterResponse(context.Background(), req, res))
	require.Len(t, res.Assertions, 1)
	assert.True(t, res.Assertions[0].Passed)

	bad := assertionResponse()
	require.NoError(
		t,
		hook.AfterResponse(context.Background(), &domain.Request{Assertions: "{"}, bad),
	)
	require.Len(t, bad.Assertions, 1)
	assert.False(t, bad.Assertions[0].Passed)

	none := assertionResponse()
	require.NoError(t, hook.AfterResponse(context.Background(), &domain.Request{}, none))
	assert.Empty(t, none.Assertions)
}
