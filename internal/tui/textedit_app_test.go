package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/keybindings"
)

// A config remap of copy and paste must reach the TUI's text key checks, and
// unset keys keep their defaults.
func TestTextKeysFollowConfigRemap(t *testing.T) {
	binds := keybindings.DefaultKeybindings()
	binds.Copy = "ctrl+y"
	binds.Paste = "ctrl+p"
	m := Model{cfg: config.Config{Keybindings: binds}}
	keys := m.textKeys()

	assert.True(t, keys.Copy.Match("ctrl+y"))
	assert.False(t, keys.Copy.Match("ctrl+c"))
	assert.True(t, keys.Paste.Match("ctrl+p"))
	assert.True(t, keys.WordLeft.Match("ctrl+left"))
	assert.True(t, keys.WordLeft.Match("alt+left"))
}

func TestTextKeysDefaultWithEmptyConfig(t *testing.T) {
	m := Model{}
	assert.True(t, m.textKeys().Copy.Match("ctrl+c"))
	assert.True(t, m.textKeys().Paste.Match("ctrl+v"))
}

// Viewer and history keys follow the config too.
func TestViewerAndHistoryKeysFollowConfig(t *testing.T) {
	binds := keybindings.DefaultKeybindings()
	binds.ViewerCopy = "y"
	binds.HistoryNext = "ctrl+n"
	m := Model{cfg: config.Config{Keybindings: binds}}
	assert.True(t, m.textKeys().ViewerCopy.Match("y"))
	assert.False(t, m.textKeys().ViewerCopy.Match("c"))
	assert.True(t, m.textKeys().HistoryNext.Match("ctrl+n"))
	assert.True(t, m.textKeys().HistoryPrev.Match("shift+up"))
}

// A successful copy or cut reports its character count in the status bar.
func TestClipboardWriteSuccessShowsStatus(t *testing.T) {
	m := Model{}
	next, _ := m.Update(clipboardMsg{text: "héllo"})
	got, ok := next.(Model)
	require.True(t, ok)
	assert.Equal(t, "Copied 5 characters", got.statusSuccess)
	assert.Empty(t, got.statusErr)
}
