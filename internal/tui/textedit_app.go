package tui

import (
	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/crazy-vedic/quark/internal/tui/textedit"
)

// appClipboard connects the textedit editor to the system clipboard. Copy uses
// copyToClipboardCmd so status-bar messages are unchanged.
type appClipboard struct{}

var _ textedit.Clipboard = appClipboard{}

// readClipboardText reads the system clipboard. It is a variable so tests can
// supply clipboard text without touching the real clipboard.
var readClipboardText = clipboard.ReadAll

func (appClipboard) Read() (string, error)        { return readClipboardText() }
func (appClipboard) WriteCmd(text string) tea.Cmd { return copyToClipboardCmd(text) }

// newTextEditor returns the editor for text fields, with key bindings taken
// from the config. It holds no per-field state, so it is built on each use.
func (m Model) newTextEditor() textedit.Editor {
	return textedit.Editor{Keys: m.textKeys(), Clip: appClipboard{}}
}

// textKeys returns the text-editing keymap built from the config. Every text
// key check in the TUI reads its binding from here, so remapping applies
// everywhere.
func (m Model) textKeys() textedit.Keymap {
	return textedit.KeymapFromConfig(m.cfg.Keybindings)
}
