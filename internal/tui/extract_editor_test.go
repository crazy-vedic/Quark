package tui

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
)

func TestParseExtractRuleRow(t *testing.T) {
	rule, err := parseExtractRuleRow("token", "$.session.token")
	require.NoError(t, err)
	assert.Equal(
		t,
		domain.Extraction{
			Variable: "token",
			Source:   domain.ExtractFromBody,
			Path:     "$.session.token",
		},
		rule,
	)

	rule, err = parseExtractRuleRow("rid", "header X-Req-Id")
	require.NoError(t, err)
	assert.Equal(
		t,
		domain.Extraction{Variable: "rid", Source: domain.ExtractFromHeader, Path: "X-Req-Id"},
		rule,
	)

	_, err = parseExtractRuleRow("bad name", "$.x")
	assert.Error(t, err)
	_, err = parseExtractRuleRow("token", "")
	assert.Error(t, err)
}

func TestNewExtractEditorRejectsInvalidStoredJSON(t *testing.T) {
	_, err := newExtractEditor(&domain.Request{Extractions: "{not json"})
	assert.Error(t, err)

	ed, err := newExtractEditor(&domain.Request{})
	require.NoError(t, err)
	assert.Empty(t, ed.rules)
}

func TestExtractionStatus(t *testing.T) {
	kind, text := extractionStatus(nil)
	assert.Empty(t, kind)
	assert.Empty(t, text)

	kind, text = extractionStatus([]exec.ExtractionOutcome{
		{Variable: "a", Environment: "dev"},
		{Variable: "b", Environment: "dev"},
	})
	assert.Equal(t, "success", kind)
	assert.Equal(t, "Extracted 2 variable(s)", text)

	kind, text = extractionStatus([]exec.ExtractionOutcome{
		{
			Variable: "a",
			Err:      fmt.Errorf("%w: HTTP 500 is not a 2xx response", exec.ErrExtractionSkipped),
		},
	})
	assert.Equal(t, "warn", kind)
	assert.Contains(t, text, "skipped")

	kind, text = extractionStatus([]exec.ExtractionOutcome{
		{Variable: "a", Environment: "dev"},
		{Variable: "tok", Err: errors.New("response has no \"X\" header")},
	})
	assert.Equal(t, "error", kind)
	assert.Equal(t, `Extraction failed: tok: response has no "X" header`, text)
}
