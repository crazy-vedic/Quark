package textedit

import tea "github.com/charmbracelet/bubbletea"

// Clipboard is how the editor reads and writes the system clipboard. The
// application supplies the implementation so that copy messages keep their
// existing status-bar behavior, and tests can record writes.
type Clipboard interface {
	// Read returns the clipboard text for paste.
	Read() (string, error)
	// WriteCmd returns a command that writes text to the clipboard.
	WriteCmd(text string) tea.Cmd
}
