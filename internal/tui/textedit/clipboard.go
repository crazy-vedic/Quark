package textedit

import tea "github.com/charmbracelet/bubbletea"

// Clipboard is how the editor reads and writes the system clipboard. The
// application supplies the implementation so that copy messages keep their
// existing status-bar behavior, and tests can record writes.
type Clipboard interface {
	// ReadCmd returns a command that reads the clipboard for paste. It runs
	// off the UI goroutine, so a slow clipboard helper cannot block input.
	ReadCmd() tea.Cmd
	// WriteCmd returns a command that writes text to the clipboard.
	WriteCmd(text string) tea.Cmd
}

// PasteCmd runs read off the UI goroutine and delivers the text as a
// bracketed paste key. The key then goes through the same routing as a
// terminal paste, so the focused field inserts it and multi-line handling
// applies. An empty or failed read delivers nothing.
func PasteCmd(read func() (string, error)) tea.Cmd {
	return func() tea.Msg {
		text, err := read()
		if err != nil || text == "" {
			return nil
		}
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text), Paste: true}
	}
}
