package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/crazy-vedic/quark/internal/tui/textedit"
)

// Paste routing. Ctrl+V reads the clipboard asynchronously, so the text arrives
// after the keypress. The text must land in the input that had focus when Ctrl+V
// was pressed, and only for the most recent request. Each request takes a
// ticket and records the input target. The result is applied only if both still
// match, and it is re-dispatched as a bracketed paste key through the normal
// routing. It never reaches the global shortcut path.

// pasteTarget identifies the input that receives keys. It is computed from the
// state the widgets already hold, so every focus move changes it: header key
// versus value, auth field, and env key versus value included. It is comparable,
// so it needs no formatting.
type pasteTarget struct {
	mode       modeID
	pane       paneID
	field      requestField
	header     int // -1 when not editing; else 2*pair + 1 if the value input has focus
	auth       int // -1 when not editing; else the auth field cursor
	env        int // -1 when not editing; else 2*var + 1 if the value input has focus
	viewerFind bool
	help       bool
	prompt     promptType
	clientCert bool
	widgets    uint32 // one bit per text widget that has focus; see focusedWidgets
}

// focusedWidgets returns a bit for each text widget that has focus. Every text
// widget in the model must appear here, or focus moving into it would not
// change the paste target. The coverage test in paste_coverage_test.go fails
// when a widget is missing.
func focusedWidgets(m Model) uint32 {
	focused := [...]bool{
		m.urlInput.Focused(),
		m.viewerFind.Focused(),
		m.searchInput.Focused(),
		m.importInput.Focused(),
		m.importName.Focused(),
		m.postmanPath.Focused(),
		m.promptInput.Focused(),
		m.scheduleInput.Focused(),
		m.clientCertHost.Focused(),
		m.clientCertFile.Focused(),
		m.clientCertType.Focused(),
		m.clientCertKeyFile.Focused(),
		m.clientCertCAFile.Focused(),
		m.clientCertPassword.Focused(),
		m.bodyTextarea.Focused(),
		m.headerKeyInput.Focused(),
		m.headerValueInput.Focused(),
		m.authEditor.tokenInput.Focused(),
		m.authEditor.usernameInput.Focused(),
		m.authEditor.passwordInput.Focused(),
		m.authEditor.apiKeyNameInput.Focused(),
		m.authEditor.apiKeyValueInput.Focused(),
		m.envEditor.editKey.Focused(),
		m.envEditor.editVal.Focused(),
	}
	var bits uint32
	for i, f := range focused {
		if f {
			bits |= 1 << i
		}
	}
	return bits
}

// pasteTargetOf returns the input that currently receives keys.
func (m Model) pasteTargetOf() pasteTarget {
	t := pasteTarget{
		mode:       m.mode,
		pane:       m.focus,
		field:      m.activeField,
		header:     -1,
		auth:       -1,
		env:        -1,
		viewerFind: m.viewerFindOpen,
		help:       m.helpSearch,
		prompt:     m.promptMode,
		clientCert: m.clientCertEditing,
		widgets:    focusedWidgets(m),
	}
	if m.headerEditing {
		t.header = 2 * m.headerCursor
		if m.headerValueInput.Focused() {
			t.header++
		}
	}
	if m.authEditor.editing {
		t.auth = m.authEditor.cursor
	}
	if m.envEditor.editing {
		t.env = 2 * m.envEditor.varCursor
		if m.envEditor.editVal.Focused() {
			t.env++
		}
	}
	return t
}

// tagPasteCmd wraps cmd so that any paste result it produces carries ticket and
// target. Batched results are tagged one by one.
func tagPasteCmd(cmd tea.Cmd, ticket uint64, target pasteTarget) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		return tagPasteMsg(cmd(), ticket, target)
	}
}

func tagPasteMsg(msg tea.Msg, ticket uint64, target pasteTarget) tea.Msg {
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

// handlePasteResult applies a clipboard read. A result is dropped when a newer
// paste request exists, or when the input that receives keys has changed. Read
// errors are reported.
func (m Model) handlePasteResult(msg textedit.PasteResultMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		return m.status("error", "Clipboard read failed: "+msg.Err.Error()), nil
	}
	if msg.Ticket == 0 || msg.Ticket != m.pasteTicket {
		return m.status("warn", "Paste discarded: superseded by a newer paste"), nil
	}
	if target, ok := msg.Target.(pasteTarget); !ok || target != m.pasteTargetOf() {
		return m.status("warn", "Paste discarded: focus changed"), nil
	}
	return m.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(msg.Text), Paste: true})
}
