package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseExtractionText(t *testing.T) {
	rules, err := ParseExtractionText(`
# comment
token = $.data.token
request_id = header X-Request-Id
headerless = $.header
`)
	require.NoError(t, err)
	assert.Equal(t, []Extraction{
		{Variable: "token", Source: ExtractFromBody, Path: "$.data.token"},
		{Variable: "request_id", Source: ExtractFromHeader, Path: "X-Request-Id"},
		{Variable: "headerless", Source: ExtractFromBody, Path: "$.header"},
	}, rules)

	again, err := ParseExtractionText(FormatExtractionText(rules))
	require.NoError(t, err)
	assert.Equal(t, rules, again)
}

func TestParseExtractionTextErrors(t *testing.T) {
	for _, text := range []string{"token", "= $.a", "token =", "my var = $.a", "x = header"} {
		_, err := ParseExtractionText(text)
		require.Error(t, err, text)
		assert.Contains(t, err.Error(), "line 1")
	}
}

func TestEncodeParseExtractions(t *testing.T) {
	assert.Equal(t, "[]", EncodeExtractions(nil))
	rules := []Extraction{{Variable: "a", Source: ExtractFromBody, Path: "$.a"}}
	got, err := ParseExtractions(EncodeExtractions(rules))
	require.NoError(t, err)
	assert.Equal(t, rules, got)

	none, err := ParseExtractions("")
	require.NoError(t, err)
	assert.Nil(t, none)

	_, err = ParseExtractions("{")
	require.Error(t, err)
}
