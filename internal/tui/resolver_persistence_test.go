package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/keybindings"
	"github.com/crazy-vedic/quark/internal/search"
)

func callUpdate(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	updated, _ := m.Update(msg)
	model, ok := updated.(Model)
	require.True(t, ok, "Update must return tui.Model")
	return model
}

// TestResolverRemapHelpAndQuit verifies that when help is remapped to T and
// quit to Q, the old keys (? and q) become no-ops and the new keys work.
func TestResolverRemapHelpAndQuit(t *testing.T) {
	custom := keybindings.DefaultKeybindings()
	custom.Help = "T"
	custom.Quit = "Q"

	m := New(Deps{
		Searcher: &search.Searcher{},
		Importer: curl.NewImporter(),
		Config:   config.Default(""),
		Ctx:      context.Background(),
		Resolver: keybindings.NewResolver(custom),
	})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	// T should open help
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	assert.Equal(t, HelpMode, m.Mode(), "T must open help mode")

	// ? should NOT open help (it's unmapped)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc}) // close help
	require.Equal(t, NormalMode, m.Mode())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	assert.Equal(t, NormalMode, m.Mode(), "? must NOT open help mode when help is T")

	// In help mode, Q should quit
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	require.Equal(t, HelpMode, m.Mode())
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Q'}})
	require.NotNil(t, cmd, "Q must quit from help mode")

	// q should NOT quit from help mode
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	assert.Equal(t, HelpMode, m.Mode(), "q must NOT quit help mode when quit is Q")
}

// TestResolverSingleKeybindingHelp verifies that when help is set to !,
// both ? and T are dead, and only ! works.
func TestResolverSingleKeybindingHelp(t *testing.T) {
	custom := keybindings.DefaultKeybindings()
	custom.Help = "!"
	custom.Quit = "Q"

	m := New(Deps{
		Searcher: &search.Searcher{},
		Importer: curl.NewImporter(),
		Config:   config.Default(""),
		Ctx:      context.Background(),
		Resolver: keybindings.NewResolver(custom),
	})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	// ? should NOT work
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	assert.Equal(t, NormalMode, m.Mode(), "? must be dead when help is !")

	// ! should open help
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'!'}})
	assert.Equal(t, HelpMode, m.Mode(), "! must open help mode")
}
