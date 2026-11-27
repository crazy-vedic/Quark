package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
)

// finishConfigSave runs a pending config write and feeds its result back into
// Update, as the Bubble Tea runtime would. It returns the model and any
// follow-up command.
func finishConfigSave(t *testing.T, m Model, cmd tea.Cmd) (Model, tea.Cmd) {
	t.Helper()
	require.NotNil(t, cmd, "a config write must be started")
	updated, next := m.Update(cmd())
	return requireModel(t, updated), next
}

func TestConfigWriteGateBlocksSecondWriteWhileSaving(t *testing.T) {
	dir := t.TempDir()
	m := New(Deps{Config: config.Default(dir), ConfigDir: dir})
	m.helpEditState = helpConfirmResetAll

	updated, cmd := m.handleHelpKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = requireModel(t, updated)
	require.NotNil(t, cmd)
	require.True(t, m.configSaving)
	require.Equal(t, helpConfirmResetAll, m.helpEditState, "state changes only after the write")

	updated, second := m.handleHelpKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = requireModel(t, updated)
	require.Nil(t, second, "no second write may start while one is pending")
	require.Contains(t, m.statusErr, "Still saving config.toml")

	m, _ = finishConfigSave(t, m, cmd)
	require.False(t, m.configSaving)
	require.Equal(t, helpViewing, m.helpEditState)
	require.Equal(t, "Reset all keybindings to defaults", m.statusSuccess)
}
