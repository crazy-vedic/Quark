package tui_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/tui"
)

func TestHelpScroll_UpMovesCursorBeforeViewport(t *testing.T) {
	m := newTestModel()
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 28})
	m = m.WithMode(tui.HelpMode)

	for i := 0; i < 25; i++ {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	}

	require.Greater(t, m.HelpScrollOffset(), 0, "setup should scroll the help viewport down")
	offsetBefore := m.HelpScrollOffset()
	cursorBefore := m.HelpCursor()

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})

	assert.Equal(t, cursorBefore-1, m.HelpCursor(), "up should move the cursor first")
	assert.Equal(
		t,
		offsetBefore,
		m.HelpScrollOffset(),
		"viewport should stay stable while the cursor is still inside the visible comfort zone",
	)
}

func TestHelpScroll_ViewportEventuallyMovesWhenCursorKeepsGoingUp(t *testing.T) {
	m := newTestModel()
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 28})
	m = m.WithMode(tui.HelpMode)

	for i := 0; i < 25; i++ {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	}

	require.Greater(t, m.HelpScrollOffset(), 0, "setup should scroll the help viewport down")
	offsetBefore := m.HelpScrollOffset()

	for i := 0; i < 20; i++ {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	}

	assert.Less(
		t,
		m.HelpScrollOffset(),
		offsetBefore,
		"viewport should scroll upward once the cursor leaves the upper comfort zone",
	)
}
