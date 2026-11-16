package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/crazy-vedic/quark/internal/tui/textedit"
)

// Paste routing. Ctrl+V reads the clipboard asynchronously, so the text arrives
// after the keypress. The text must land in the input that had focus when Ctrl+V
// was pressed. Each request takes a ticket and records the input target. The
// result is applied only if the input target is still the same, and it is
// re-dispatched as a bracketed paste key through the normal routing. Results
// from repeated presses in the same input apply in request order. It never
// reaches the global shortcut path.

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

// TextWidget is the focus surface of a text input or text area in Model.
type TextWidget interface {
	Focus() tea.Cmd
	Blur()
	Focused() bool
}

// namedWidget pairs a widget's path in Model with a getter for it.
type namedWidget struct {
	name string
	get  func(*Model) TextWidget
}

// textWidgets lists every text widget in Model, including those inside
// authEditor and envEditor. It is the single list the paste target is built
// from. A widget missing here gets no focus bit, so focus moving into it would
// not change the paste target. The guard tests in paste_routing_test.go check
// this list against Model by reflection. The array length is bounded to 32
// below, because each widget uses one bit of the uint32 mask.
var textWidgets = [...]namedWidget{
	{"urlInput", func(m *Model) TextWidget { return &m.urlInput }},
	{"viewerFind", func(m *Model) TextWidget { return &m.viewerFind }},
	{"searchInput", func(m *Model) TextWidget { return &m.searchInput }},
	{"importInput", func(m *Model) TextWidget { return &m.importInput }},
	{"importName", func(m *Model) TextWidget { return &m.importName }},
	{"postmanPath", func(m *Model) TextWidget { return &m.postmanPath }},
	{"promptInput", func(m *Model) TextWidget { return &m.promptInput }},
	{"scheduleInput", func(m *Model) TextWidget { return &m.scheduleInput }},
	{"clientCertHost", func(m *Model) TextWidget { return &m.clientCertHost }},
	{"clientCertFile", func(m *Model) TextWidget { return &m.clientCertFile }},
	{"clientCertType", func(m *Model) TextWidget { return &m.clientCertType }},
	{"clientCertKeyFile", func(m *Model) TextWidget { return &m.clientCertKeyFile }},
	{"clientCertCAFile", func(m *Model) TextWidget { return &m.clientCertCAFile }},
	{"clientCertPassword", func(m *Model) TextWidget { return &m.clientCertPassword }},
	{"bodyTextarea", func(m *Model) TextWidget { return &m.bodyTextarea }},
	{"headerKeyInput", func(m *Model) TextWidget { return &m.headerKeyInput }},
	{"headerValueInput", func(m *Model) TextWidget { return &m.headerValueInput }},
	{"authEditor.tokenInput", func(m *Model) TextWidget { return &m.authEditor.tokenInput }},
	{"authEditor.usernameInput", func(m *Model) TextWidget { return &m.authEditor.usernameInput }},
	{"authEditor.passwordInput", func(m *Model) TextWidget { return &m.authEditor.passwordInput }},
	{"authEditor.apiKeyNameInput", func(m *Model) TextWidget {
		return &m.authEditor.apiKeyNameInput
	}},
	{"authEditor.apiKeyValueInput", func(m *Model) TextWidget {
		return &m.authEditor.apiKeyValueInput
	}},
	{"envEditor.editKey", func(m *Model) TextWidget { return &m.envEditor.editKey }},
	{"envEditor.editVal", func(m *Model) TextWidget { return &m.envEditor.editVal }},
}

// The mask has 32 bits; a longer list would silently lose widgets.
var _ [32 - len(textWidgets)]struct{}

// focusedWidgets returns a bit for each text widget in textWidgets that has
// focus. Bit i is set when textWidgets[i] is focused.
func focusedWidgets(m Model) uint32 {
	var bits uint32
	for i, w := range textWidgets {
		if w.get(&m).Focused() {
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

// handlePasteResult applies clipboard reads in the order Ctrl+V was pressed.
// Bubble Tea runs commands concurrently, so results can arrive out of order;
// a result is held until every earlier ticket has been applied. A result is
// dropped only when the input that receives keys has changed. Read errors are
// reported.
func (m Model) handlePasteResult(msg textedit.PasteResultMsg) (tea.Model, tea.Cmd) {
	if msg.Ticket == 0 {
		return m.applyPaste(msg)
	}
	if msg.Ticket <= m.pasteApplied {
		return m, nil
	}
	held := make(map[uint64]textedit.PasteResultMsg, len(m.pasteHeld)+1)
	for k, v := range m.pasteHeld {
		held[k] = v
	}
	held[msg.Ticket] = msg
	m.pasteHeld = held

	var cmds []tea.Cmd
	for {
		next, ok := m.pasteHeld[m.pasteApplied+1]
		if !ok {
			break
		}
		delete(m.pasteHeld, m.pasteApplied+1)
		m.pasteApplied++
		model, cmd := m.applyPaste(next)
		m = model.(Model) //nolint:forcetypeassert // applyPaste always returns Model
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m Model) applyPaste(msg textedit.PasteResultMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		if m.mode == importMode {
			m.importError = "Clipboard read failed: " + msg.Err.Error()
			return m, nil
		}
		return m.status("error", "Clipboard read failed: "+msg.Err.Error()), nil
	}
	if msg.Ticket == 0 || msg.Text == "" {
		return m, nil
	}
	if target, ok := msg.Target.(pasteTarget); !ok || target != m.pasteTargetOf() {
		return m.status("warn", "Paste discarded: focus changed"), nil
	}
	return m.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(msg.Text), Paste: true})
}
