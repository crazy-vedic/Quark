package tui_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"

	"github.com/crazy-vedic/quark/internal/tui"
)

// TestURLFieldKeyboardSelection verifies selection behavior in the URL field.
func TestURLFieldKeyboardSelection(t *testing.T) {
	m := newModel(defaultConfig()).
		WithFocus(tui.RequestPane).
		WithActiveField(tui.URLField).
		WithURLValue("https://example.com")

	// Ctrl+A selects all
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlA})
	sel := m.URLSelection()
	start, end := sel.Range()
	assert.Equal(t, 0, start)
	assert.Equal(t, 19, end) // "https://example.com" = 19 runes
	assert.True(t, sel.Active())

	// Shift+Left shrinks selection
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyShiftLeft})
	sel = m.URLSelection()
	start, end = sel.Range()
	assert.Equal(t, 0, start)
	assert.Equal(t, 18, end) // Shrink from 19 to 18

	// Regular character replaces selection
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	sel = m.URLSelection()
	assert.False(t, sel.Active())
	assert.True(t, len(m.URLValue()) < 20)
}

// TestURLFieldSelectionMultiplied verifies multiple selection operations.
func TestURLFieldSelectionMultiplied(t *testing.T) {
	m := newModel(defaultConfig()).
		WithFocus(tui.RequestPane).
		WithActiveField(tui.URLField).
		WithURLValue("https://example.com")

	// Ctrl+A selects all
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlA})
	sel := m.URLSelection()
	start, end := sel.Range()
	assert.Equal(t, 0, start)
	assert.Equal(t, 19, end)

	// Press right arrow to deselect and move cursor
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight})
	sel = m.URLSelection()
	assert.False(t, sel.Active()) // Selection cleared

	// Now Ctrl+A again
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlA})
	sel = m.URLSelection()
	start, end = sel.Range()
	assert.Equal(t, 0, start)
	assert.Equal(t, 19, end)
}

// TestURLFieldSelectionClears verifies selection clears when changing fields.
func TestURLFieldSelectionClears(t *testing.T) {
	m := newModel(defaultConfig()).
		WithFocus(tui.RequestPane).
		WithActiveField(tui.URLField).
		WithURLValue("https://example.com").
		WithBodyValue("test body")

	// Select text in URL field
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlA})
	sel := m.URLSelection()
	assert.True(t, sel.Active())

	// Press Escape to close field - selection should clear
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	sel = m.URLSelection()
	assert.False(t, sel.Active())
}

// TestEnvironmentSwitchGating verifies Shift+arrows only switch envs when no field is focused.
func TestEnvironmentSwitchGating(t *testing.T) {
	t.Skip("Requires resolver and environment switching implementation")

	// With URL field focused, Shift+arrows should not trigger env switching
	// With no field focused, Shift+arrows should trigger env switching
	// This test needs ActionEnvPrev/ActionEnvNext routing which requires resolver
}

// TestBodyFieldKeyboardSelection verifies selection in body textarea.
func TestBodyFieldKeyboardSelection(t *testing.T) {
	t.Skip("Textarea selection integration requires view.go rendering implementation (Phase 3)")
}

// TestHeaderFieldKeyboardSelection verifies selection in header fields.
func TestHeaderFieldKeyboardSelection(t *testing.T) {
	t.Skip("Header field selection requires key handler integration (Phase 2 for headers)")
}
