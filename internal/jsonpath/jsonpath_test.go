package jsonpath

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const doc = `{
  "data": {"token": "abc", "count": 3, "ok": true, "none": null,
           "items": [{"id": 1}, {"id": 2}], "odd key": "x", "with-dash": "d"},
  "big": 12345678901234567890
}`

func TestEval(t *testing.T) {
	cases := map[string]string{
		"$.data.token":          "abc",
		"data.token":            "abc",
		"$.data.count":          "3",
		"$.data.ok":             "true",
		"$.data.none":           "null",
		"$.data.items[1].id":    "2",
		"$.data.items[-1].id":   "2",
		"$.data['odd key']":     "x",
		`$["data"]["odd key"]`:  "x",
		"$.data.with-dash":      "d",
		"$.data.items[0]":       `{"id":1}`,
		"$.big":                 "12345678901234567890",
		`$.data['it\'s']`:       "",
		"$":                     "",
		" $.data.token ":        "abc",
		"$.data.items[ 0 ].id":  "1",
		"$.data.items[-2].id":   "1",
		"$['data'].items[0].id": "1",
	}
	for expr, want := range cases {
		t.Run(expr, func(t *testing.T) {
			v, err := Eval([]byte(doc), expr)
			if expr == `$.data['it\'s']` {
				require.ErrorIs(t, err, ErrNotFound)
				return
			}
			require.NoError(t, err)
			if expr == "$" {
				_, isMap := v.(map[string]any)
				assert.True(t, isMap)
				return
			}
			assert.Equal(t, want, Text(v))
		})
	}
}

func TestEvalNotFound(t *testing.T) {
	for _, expr := range []string{
		"$.missing", "$.data.token.deeper", "$.data.items[5]", "$.data.items[-3]",
		"$.data[0]", "$.data.items.id",
	} {
		_, err := Eval([]byte(doc), expr)
		require.ErrorIs(t, err, ErrNotFound, expr)
	}
}

func TestParseSyntaxErrors(t *testing.T) {
	for _, expr := range []string{
		"", "$.", "$[", "$[*]", "$..a", "$.a[?(@.x)]", "$['a'", "$['a'x", "$.a b", "$[1",
	} {
		_, err := Parse(expr)
		require.ErrorIs(t, err, ErrSyntax, expr)
	}
}

func TestEvalInvalidJSON(t *testing.T) {
	_, err := Eval([]byte("not json"), "$.a")
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrNotFound))
	_, err = Eval([]byte("{}"), "$[")
	require.ErrorIs(t, err, ErrSyntax)
}

func TestPathString(t *testing.T) {
	p, err := Parse("$.a")
	require.NoError(t, err)
	assert.Equal(t, "$.a", p.String())
}

func TestText(t *testing.T) {
	assert.Equal(t, "1.5", Text(1.5))
	assert.Equal(t, "false", Text(false))
	assert.Equal(t, "[1,2]", Text([]any{1, 2}))
	assert.Equal(t, "x", Text("x"))
}
