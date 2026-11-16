package tui

import (
	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/crazy-vedic/quark/internal/tui/textedit"
)

// appClipboard connects the textedit editor to the system clipboard. Copy uses
// copyToClipboardCmd, which reports success ("Copied N characters") or failure
// through the status bar when its result is handled in update.
type appClipboard struct{}

var _ textedit.Clipboard = appClipboard{}

// readClipboardText reads the system clipboard. It is a variable so tests can
// supply clipboard text without touching the real clipboard.
var readClipboardText = clipboard.ReadAll

func (appClipboard) ReadCmd() tea.Cmd             { return textedit.PasteCmd(readClipboardText) }
func (appClipboard) WriteCmd(text string) tea.Cmd { return copyToClipboardCmd(text) }

// newTextEditor returns the editor for text fields, with key bindings taken
// from the config. It holds no per-field state, so it is built on each use.
func (m Model) newTextEditor() textedit.Editor {
	return textedit.Editor{Keys: m.textKeys(), Clip: appClipboard{}}
}

// cacheTextKeys builds the text-editing keymap from the config once. It runs
// at construction and whenever the config changes, so keystrokes reuse it.
func (m *Model) cacheTextKeys() {
	k := textedit.KeymapFromConfig(m.cfg.Keybindings)
	m.editKeys = &k
}

// textKeys returns the text-editing keymap built from the config. Every text
// key check in the TUI reads its binding from here, so remapping applies
// everywhere. Models built without cacheTextKeys still get a correct keymap.
func (m Model) textKeys() textedit.Keymap {
	if m.editKeys != nil {
		return *m.editKeys
	}
	return textedit.KeymapFromConfig(m.cfg.Keybindings)
}
