package tui_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/tui"
)

func TestModel_PromptDeleteConfirm_TypingNo_Cancels(t *testing.T) {
	m := newTestModel()
	m = m.WithMode(tui.CollectionPromptMode).
		WithPromptMode(tui.PromptDeleteConfirm).
		WithPromptTargetID("col-1")
	m = m.WithPromptInputValue("no")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model, ok := updated.(tui.Model)
	require.True(t, ok)

	assert.Equal(t, tui.NormalMode, model.Mode(), "should return to normal mode")
	assert.NotEmpty(t, model.StatusErr(), "should show error")
	assert.Contains(t, model.StatusErr(), "cancelled")
}

func TestModel_PromptDeleteConfirm_EmptyInput_Cancels(t *testing.T) {
	m := newTestModel()
	m = m.WithMode(tui.CollectionPromptMode).
		WithPromptMode(tui.PromptDeleteConfirm).
		WithPromptTargetID("col-1")
	m = m.WithPromptInputValue("")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model, ok := updated.(tui.Model)
	require.True(t, ok)

	assert.Equal(t, tui.NormalMode, model.Mode(), "should return to normal mode")
	assert.NotEmpty(t, model.StatusErr(), "should show error")
	assert.Contains(t, model.StatusErr(), "cancelled")
}

func TestModel_PromptDeleteConfirm_CaseSensitive_Fails(t *testing.T) {
	m := newTestModel()
	m = m.WithMode(tui.CollectionPromptMode).
		WithPromptMode(tui.PromptDeleteConfirm).
		WithPromptTargetID("col-1")
	m = m.WithPromptInputValue("YES")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model, ok := updated.(tui.Model)
	require.True(t, ok)

	assert.Equal(t, tui.NormalMode, model.Mode(), "should return to normal mode")
	assert.NotEmpty(t, model.StatusErr(), "should show error")
	assert.Contains(t, model.StatusErr(), "cancelled")
}

func TestModel_PromptDeleteConfirm_Esc_Cancels(t *testing.T) {
	m := newTestModel()
	m = m.WithMode(tui.CollectionPromptMode).
		WithPromptMode(tui.PromptDeleteConfirm).
		WithPromptTargetID("col-1")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model, ok := updated.(tui.Model)
	require.True(t, ok)

	assert.Equal(t, tui.NormalMode, model.Mode(), "should return to normal mode")
}
