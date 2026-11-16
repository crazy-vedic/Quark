package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/crazy-vedic/quark/internal/tui/textedit"
)

// Paste routing. Ctrl+V reads the clipboard asynchronously, so the text arrives
// after the keypress. The text must land in the field that had focus when Ctrl+V
// was pressed, and only for the most recent request. Each request takes a
// ticket and records the input target. The result is delivered only if both
// still match, and it is re-dispatched as a bracketed paste key through the
// normal routing. It never reaches the global shortcut path.

// pasteTarget describes where keys currently go. Any change to these fields
// means a paste that was requested earlier no longer has the same target.
func (m Model) pasteTarget() string {
	return fmt.Sprint(
		m.mode, "|", m.focus, "|", m.activeField, "|",
		m.authEditor.editing, "|", m.envEditor.editing, "|",
		m.headerEditing, "|", m.clientCertEditing, "|",
		m.viewerFindOpen, "|", m.helpSearch, "|", m.promptMode,
	)
}

// tagPasteCmd wraps cmd so that any paste result it produces carries ticket and
// target. Batched results are tagged one by one.
func tagPasteCmd(cmd tea.Cmd, ticket uint64, target string) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		return tagPasteMsg(cmd(), ticket, target)
	}
}

func tagPasteMsg(msg tea.Msg, ticket uint64, target string) tea.Msg {
	switch v := msg.(type) {
	case textedit.PasteResultMsg:
		v.Ticket = ticket
		v.Target = target
		return v
	case tea.BatchMsg:
		out := make(tea.BatchMsg, len(v))
		for i, c := range v {
			out[i] = tagPasteCmd(c, ticket, target)
		}
		return out
	}
	return msg
}

// handlePasteResult applies a clipboard read. Stale results are dropped: a
// newer paste request exists, or focus has moved. Errors are reported.
func (m Model) handlePasteResult(msg textedit.PasteResultMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		return m.status("error", "Clipboard read failed: "+msg.Err.Error()), nil
	}
	if msg.Ticket == 0 || msg.Ticket != m.pasteTicket || msg.Target != m.pasteTarget() {
		return m.status("warn", "Paste discarded: focus changed"), nil
	}
	return m.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(msg.Text), Paste: true})
}
