package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAssertionText(t *testing.T) {
	text := `
# comments and blank lines are skipped

status = 201
time < 500ms
$.data.id exists
$.data.name = "Ada Lovelace"
$.count = 3
header Content-Type contains json
header X-Env = prod
`
	rules, err := ParseAssertionText(text)
	require.NoError(t, err)
	assert.Equal(t, []Assertion{
		{Kind: AssertStatus, Op: "=", Value: "201"},
		{Kind: AssertTime, Op: "<", Value: "500ms"},
		{Kind: AssertJSONPath, Target: "$.data.id", Op: "exists"},
		{Kind: AssertJSONPath, Target: "$.data.name", Op: "=", Value: "Ada Lovelace"},
		{Kind: AssertJSONPath, Target: "$.count", Op: "=", Value: "3"},
		{Kind: AssertHeader, Target: "Content-Type", Op: "contains", Value: "json"},
		{Kind: AssertHeader, Target: "X-Env", Op: "=", Value: "prod"},
	}, rules)
}

func TestParseAssertionTextRejectsInvalidLines(t *testing.T) {
	for _, line := range []string{
		"status 200",         // missing '='
		"status = 99",        // not a status code
		"status < 200",       // status only supports '='
		"time < soon",        // not a duration
		"time = 500ms",       // time only supports '<'
		"$.a",                // no operator
		"$.a ~ 1",            // unknown operator
		"$[ exists",          // invalid JSONPath
		"header exists",      // missing header name is parsed as name "exists"; no operator
		"header X contains ", // contains needs a value
		"nonsense",           // unknown kind
	} {
		_, err := ParseAssertionText(line)
		assert.Error(t, err, line)
	}
}

func TestFormatAssertionTextRoundTrips(t *testing.T) {
	rules := []Assertion{
		{Kind: AssertStatus, Op: "=", Value: "200"},
		{Kind: AssertTime, Op: "<", Value: "1s"},
		{Kind: AssertJSONPath, Target: "$.a", Op: "exists"},
		{Kind: AssertJSONPath, Target: "$.name", Op: "=", Value: " padded "},
		{Kind: AssertJSONPath, Target: "$.empty", Op: "=", Value: ""},
		{Kind: AssertHeader, Target: "X-Env", Op: "contains", Value: "prod"},
	}
	parsed, err := ParseAssertionText(FormatAssertionText(rules))
	require.NoError(t, err)
	assert.Equal(t, rules, parsed)
}

func TestEncodeParseAssertions(t *testing.T) {
	assert.Equal(t, "[]", EncodeAssertions(nil))
	rules, err := ParseAssertions("")
	require.NoError(t, err)
	assert.Empty(t, rules)

	_, err = ParseAssertions("{")
	assert.Error(t, err)

	in := []Assertion{{Kind: AssertStatus, Op: "=", Value: "200"}}
	out, err := ParseAssertions(EncodeAssertions(in))
	require.NoError(t, err)
	assert.Equal(t, in, out)
}
