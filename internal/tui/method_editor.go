package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/crazy-vedic/quark/internal/domain"
)

// methodInputWidth is the cell width of the method input in the badge slot.
const methodInputWidth = 12

// beginMethodEdit turns the method badge into a text input pre-filled with the
// current method. Enter commits it; Esc restores the method it started with.
func (m Model) beginMethodEdit() (Model, tea.Cmd) {
	m.preEditMethod = m.method
	m.methodInput.SetValue(m.method)
	m.methodInput.CursorEnd()
	m.methodSelection.Collapse(0)
	m.urlInput.Blur()
	m.methodInput.Focus()
	m.activeField = methodField
	return m, textinput.Blink
}

// finishMethodEdit commits the typed method, trimmed and upper-cased. A value
// that is not a valid HTTP token is refused and the field stays open.
func (m Model) finishMethodEdit() (Model, tea.Cmd) {
	method := strings.ToUpper(strings.TrimSpace(m.methodInput.Value()))
	if err := domain.ValidMethod(method); err != nil {
		return m.status("error",
			"Method must be an HTTP token: letters, digits and !#$%&'*+-.^_`|~"), nil
	}
	m.method = method
	m.activeField = noneField
	m.methodInput.Blur()
	if m.activeRequest == nil {
		return m, nil
	}
	m.activeRequest.Method = method
	return m, saveRequestCmd(m.ctx, m.writer, m.reader, m.activeRequest)
}
