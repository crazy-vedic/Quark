package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

func TestParseURLQuery(t *testing.T) {
	params := domain.ParseURLQuery(
		"https://api.test/items?q=a%20b&&flag&token={{tok}}&plus=1+2#frag",
	)
	require.Len(t, params, 4)
	assert.Equal(t, "q", params[0].Key)
	assert.Equal(t, "a b", params[0].Value)
	assert.Equal(t, "flag", params[1].Key)
	assert.Empty(t, params[1].Value)
	assert.Equal(t, "{{tok}}", params[2].Value)
	assert.Equal(t, "1 2", params[3].Value)

	assert.Empty(t, domain.ParseURLQuery("https://api.test/items"))
	assert.Empty(t, domain.ParseURLQuery("https://api.test/items?#frag"))
}

func TestParseURLQuery_KeepsUndecodableValue(t *testing.T) {
	params := domain.ParseURLQuery("https://api.test/?pct=100%")
	require.Len(t, params, 1)
	assert.Equal(t, "100%", params[0].Value)
}

func TestSetURLQuery_RoundTripKeepsUntouchedBytes(t *testing.T) {
	raw := "https://api.test/items?q=a%20b&flag&plus=1+2&token={{tok}}#frag"
	assert.Equal(t, raw, domain.SetURLQuery(raw, domain.ParseURLQuery(raw)))
}

func TestSetURLQuery_EncodesEditedAndSkipsDisabled(t *testing.T) {
	params := domain.ParseURLQuery("https://api.test/items?q=old&keep=1")
	params[0].Value = "a b&c=d#e+f%"
	params[1].Disabled = true
	params = append(params,
		domain.QueryParam{Key: "k=1", Value: "{{var}}/x"},
		domain.QueryParam{}, // blank rows are dropped
	)
	got := domain.SetURLQuery("https://api.test/items?q=old&keep=1#top", params)
	assert.Equal(t, "https://api.test/items?q=a%20b%26c=d%23e%2Bf%25&k%3D1={{var}}/x#top", got)

	decoded := domain.ParseURLQuery(got)
	require.Len(t, decoded, 2)
	assert.Equal(t, "a b&c=d#e+f%", decoded[0].Value)
	assert.Equal(t, "k=1", decoded[1].Key)
}

func TestSetURLQuery_NoParamsRemovesQuestionMark(t *testing.T) {
	assert.Equal(
		t,
		"https://api.test/items#f",
		domain.SetURLQuery("https://api.test/items?a=1#f", nil),
	)
	assert.Equal(t, "https://api.test/?x=1", domain.SetURLQuery("https://api.test/",
		[]domain.QueryParam{{Key: "x", Value: "1"}}))
}

func TestRequestParams_RoundTrip(t *testing.T) {
	empty, err := domain.ParseRequestParams("")
	require.NoError(t, err)
	assert.Equal(t, "{}", empty.String())

	p := domain.RequestParams{DisabledQuery: []domain.QueryParam{{Key: "debug", Value: "1"}}}
	parsed, err := domain.ParseRequestParams(p.String())
	require.NoError(t, err)
	require.Len(t, parsed.DisabledQuery, 1)
	assert.Equal(t, "debug", parsed.DisabledQuery[0].Key)
	assert.True(t, parsed.DisabledQuery[0].Disabled)

	_, err = domain.ParseRequestParams("{not json")
	assert.Error(t, err)
}
