//go:build e2e

package tui_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/keybindings"
	"github.com/crazy-vedic/quark/internal/tui"
	"github.com/crazy-vedic/quark/internal/tuitest"
)

func helpSaveJourneyModel(t *testing.T, dir string) tui.Model {
	t.Helper()
	st := setupStore(t)
	cfg := config.Default(dir)
	m := tuitest.NewModelWithConfigDir(t, st, &mockExecutor{}, cfg, dir)
	m = resize(t, m, 120, 40)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	require.Equal(t, tui.HelpMode, m.Mode())
	return m
}

// Recording a key in the help overlay writes config.toml off the Update loop.
// While that write is pending, other help keys must wait with a warning, and
// the saved binding must be visible in the status line and on disk.
func TestE2E_HelpEditor_RecordedBindingSavesAndBlocksWhilePending(t *testing.T) {
	dir := t.TempDir()
	m := helpSaveJourneyModel(t, dir)

	// Cursor starts on the "quit" row. Enter starts recording, Q is the new key.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m, saveCmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Q'}})
	require.NotNil(t, saveCmd, "recording a key must return the config write")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	assert.Equal(t, "Still saving config.toml, try again", m.StatusErr())

	m = callUpdate(t, m, saveCmd())
	assert.Equal(t, "Bound quit -> Q", m.StatusSuccess())

	saved, err := config.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "Q", saved.Keybindings.Quit, "the recorded key must be written to config.toml")
}

// Resetting one binding and resetting every binding both persist the defaults.
func TestE2E_HelpEditor_ResetOneAndResetAllPersistDefaults(t *testing.T) {
	dir := t.TempDir()
	m := helpSaveJourneyModel(t, dir)
	defaults := keybindings.DefaultKeybindings()

	// Record a non-default key for quit, then reset that single row with r.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m, saveCmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Q'}})
	m = callUpdate(t, m, saveCmd())
	require.Equal(t, "Bound quit -> Q", m.StatusSuccess())

	m, resetCmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	require.NotNil(t, resetCmd)
	m = callUpdate(t, m, resetCmd())
	assert.Equal(t, "Reset quit to q", m.StatusSuccess())
	saved, err := config.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, defaults.Quit, saved.Keybindings.Quit)

	// Make another change, then reset all: R asks for confirmation, Enter writes.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m, saveCmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Q'}})
	m = callUpdate(t, m, saveCmd())
	require.Equal(t, "Bound quit -> Q", m.StatusSuccess())

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	assertViewContains(t, m, "Reset all")
	m, resetAllCmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, resetAllCmd)
	m = callUpdate(t, m, resetAllCmd())
	assert.Equal(t, "Reset all keybindings to defaults", m.StatusSuccess())

	saved, err = config.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, defaults, saved.Keybindings)
}
