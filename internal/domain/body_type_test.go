package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

func TestNormalizeBodyType(t *testing.T) {
	for in, want := range map[string]string{
		"":                domain.BodyTypeRaw,
		"\tRAW\n":         domain.BodyTypeRaw,
		"none":            domain.BodyTypeNone,
		"json":            domain.BodyTypeJSON,
		"form-urlencoded": domain.BodyTypeFormURLEncoded,
		"Form-UrlEncoded": domain.BodyTypeFormURLEncoded,
	} {
		got, err := domain.NormalizeBodyType(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	_, err := domain.NormalizeBodyType("xml")
	assert.ErrorContains(t, err, `unsupported body type "xml"`)
}

func TestNextBodyType_CyclesAllTypes(t *testing.T) {
	current := domain.BodyTypeNone
	var seen []string
	for range 5 {
		current = domain.NextBodyType(current)
		seen = append(seen, current)
	}
	assert.Equal(t, []string{"raw", "json", "form-urlencoded", "multipart", "none"}, seen)
	assert.Equal(t, "none", domain.NextBodyType("bogus"))
	assert.Equal(t, "json", domain.NextBodyType(""), "blank is raw, so the next is json")
}

func TestDefaultBodyContentType(t *testing.T) {
	assert.Equal(t, "application/json", domain.DefaultBodyContentType(domain.BodyTypeJSON))
	assert.Equal(t, "application/x-www-form-urlencoded",
		domain.DefaultBodyContentType(domain.BodyTypeFormURLEncoded))
	assert.Empty(t, domain.DefaultBodyContentType(domain.BodyTypeRaw))
	assert.Empty(t, domain.DefaultBodyContentType(domain.BodyTypeNone))
}

func TestParseFormBody_DecodesRows(t *testing.T) {
	fields := domain.ParseFormBody(
		"grant_type=client+credentials&&flag&token={{tok}}&pct=100%25&bad=%zz",
	)
	require.Len(t, fields, 5)
	assert.Equal(t, domain.FormField{Key: "grant_type", Value: "client credentials"}, fields[0])
	assert.Equal(t, domain.FormField{Key: "flag"}, fields[1])
	assert.Equal(t, domain.FormField{Key: "token", Value: "{{tok}}"}, fields[2])
	assert.Equal(t, domain.FormField{Key: "pct", Value: "100%"}, fields[3])
	assert.Equal(
		t,
		domain.FormField{Key: "bad", Value: "%zz"},
		fields[4],
		"undecodable value shown as written",
	)
	assert.Empty(t, domain.ParseFormBody(""))
}

func TestEncodeFormBody_RoundTripAndVariables(t *testing.T) {
	fields := []domain.FormField{
		{Key: "q", Value: "a b&c=d#e+f%"},
		{Key: "k=1", Value: "{{var}}/x"},
		{Key: "", Value: ""}, // blank rows are dropped
	}
	wire := domain.EncodeFormBody(fields)
	assert.Equal(t, "q=a+b%26c%3Dd%23e%2Bf%25&k%3D1={{var}}%2Fx", wire)
	assert.Equal(t, fields[:2], domain.ParseFormBody(wire))
}
