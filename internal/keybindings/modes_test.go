package keybindings

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestModeConstants_KeepResolverOrder(t *testing.T) {
	// The numeric values are part of the resolver's contract; pin them.
	assert.Equal(t, Mode(0), ModeNormal)
	assert.Equal(t, Mode(1), ModeSearch)
	assert.Equal(t, Mode(2), ModeHelp)
	assert.Equal(t, Mode(3), ModeImport)
	assert.Equal(t, Mode(4), ModeBody)
	assert.Equal(t, Mode(5), ModeHeader)
	assert.Equal(t, Mode(6), ModeEnv)
	assert.Equal(t, Mode(7), ModeCollectionPrompt)
	assert.Equal(t, Mode(8), ModeAuth)
	assert.Equal(t, Mode(9), ModeCertificate)
	assert.Equal(t, Pane(0), PaneSidebar)
	assert.Equal(t, Pane(1), PaneRequest)
	assert.Equal(t, Pane(2), PaneResponse)
}

func TestResolve_UnknownModeDoesNotResolve(t *testing.T) {
	r := NewResolver(DefaultKeybindings())
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}
	_, ok := r.Resolve(ModeCertificate+1, PaneSidebar, msg)
	assert.False(t, ok)
	_, ok = r.Resolve(Mode(-1), PaneSidebar, msg)
	assert.False(t, ok)
}
