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

// PasteResultMsg is the result of a clipboard read for paste. It is not a key
// message: the application decides where the text goes, based on the focus
// that was active when the paste was requested. Ticket and Target are set by
// the application when it receives the paste request; the editor leaves them
// zero. Target is opaque to the editor and compared by the application.
type PasteResultMsg struct {
	Text   string
	Err    error
	Ticket uint64
	Target any
}

// PasteCmd runs read off the UI goroutine and delivers a PasteResultMsg. An
// empty clipboard delivers nothing, so pressing paste on an empty clipboard
// stays silent. A failed read is delivered as Err so it can be reported.
func PasteCmd(read func() (string, error)) tea.Cmd {
	return func() tea.Msg {
		text, err := read()
		if err == nil && text == "" {
			return nil
		}
		return PasteResultMsg{Text: text, Err: err}
	}
}
