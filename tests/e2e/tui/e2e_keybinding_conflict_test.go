//go:build e2e

package tui_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/keybindings"
	"github.com/crazy-vedic/quark/internal/tui"
)

// Rebinding an action to a key another action in the same group already uses is
// refused. The message must name the key and both actions in plain words, not
// print a Go slice.
func TestE2E_HelpEditor_ConflictMessageIsReadable(t *testing.T) {
	m := newE2EModelWithBindings(t, keybindings.DefaultKeybindings())
	m = resize(t, m, 120, 40)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	require.Equal(t, tui.HelpMode, m.Mode())

	// The first two rows are the Global actions "quit" and "help".
	entries := keybindings.ListEntries(keybindings.DefaultKeybindings())
	require.Equal(t, "quit", entries[0].Action)
	require.Equal(t, "help", entries[1].Action)
	helpKey := entries[1].Key

	// Start recording a new key for "quit", then press the key "help" already uses.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(helpKey)})

	want := helpKey + " is bound to help and quit"
	assertViewContains(t, m, "conflict: "+want)
	assertViewNotContains(t, m, "[")
}
