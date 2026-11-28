package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
)

func TestAssertionStatusSummary(t *testing.T) {
	kind, text := assertionStatus(nil)
	assert.Empty(t, kind)
	assert.Empty(t, text)

	kind, text = assertionStatus([]exec.AssertionResult{
		{Rule: "status = 200", Passed: true},
		{Rule: "time < 1s", Passed: true},
	})
	assert.Equal(t, "success", kind)
	assert.Equal(t, "Assertions: 2 of 2 passed", text)

	kind, text = assertionStatus([]exec.AssertionResult{
		{Rule: "status = 200", Passed: true},
		{Rule: "status = 404", Passed: false, Reason: "status is 200"},
		{Rule: "time < 1ms", Passed: false, Reason: "took 5ms, limit 1ms"},
	})
	assert.Equal(t, "error", kind)
	assert.Equal(t, "Assertions: 2 of 3 failed (status = 404: status is 200)", text)
}

func TestNewAssertEditorRejectsInvalidStoredJSON(t *testing.T) {
	_, err := newAssertEditor(&domain.Request{Assertions: "[oops"})
	assert.Error(t, err)

	ed, err := newAssertEditor(
		&domain.Request{Assertions: `[{"kind":"status","op":"=","value":"200"}]`},
	)
	require.NoError(t, err)
	require.Len(t, ed.rules, 1)
	assert.Equal(t, "status = 200", domain.FormatAssertion(ed.rules[0]))
}

func TestRemoveAssertRowBounds(t *testing.T) {
	rules := []domain.Assertion{{Kind: domain.AssertStatus, Op: "=", Value: "200"}}
	assert.Len(t, removeAssertRow(rules, -1), 1)
	assert.Len(t, removeAssertRow(rules, 1), 1)
	assert.Empty(t, removeAssertRow(rules, 0))
}
