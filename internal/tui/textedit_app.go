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

// newTextEditor returns the shared editor for single-line fields. It holds no
// per-field state, so it is built on each use.
func newTextEditor() textedit.Editor { return textedit.New(appClipboard{}) }
