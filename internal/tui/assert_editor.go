package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/keybindings"
	"github.com/crazy-vedic/quark/internal/tui/textedit"
)

// assertResolverMode is the keybindings mode of the assertion editor.
const assertResolverMode = 11

// assertEditor is the request pane's list of assertions. Each row is one
// assertion in its text form (see domain.ParseAssertionText); the single
// input edits the row under the cursor.
type assertEditor struct {
	rules   []domain.Assertion
	cursor  int
	editing bool
	newRow  bool // the row being edited was just added; cancel removes it
	input   textinput.Model
	sel     textedit.Selection
	err     string
}

func newAssertInput() textinput.Model {
	in := textinput.New()
	in.Placeholder = "status = 200 | time < 500ms | $.id exists | header X = y"
	in.CharLimit = 512
	return in
}

func newAssertEditor(req *domain.Request) (assertEditor, error) {
	rules, err := domain.ParseAssertions(req.Assertions)
	if err != nil {
		return assertEditor{}, err
	}
	return assertEditor{rules: rules, input: newAssertInput()}, nil
}

func (m Model) beginAssertEdit() Model {
	if m.activeRequest == nil {
		return m.status("error", "Select a request first")
	}
	ed, err := newAssertEditor(m.activeRequest)
	if err != nil {
		return m.status(
			"error",
			"This request's stored assertions are not valid JSON; refusing to edit and overwrite them",
		)
	}
	m.assert = ed
	m.styleInputs()
	m.activeField = assertField
	return m.clearStatus()
}

func (m Model) beginAssertRowEdit(newRow bool) Model {
	e := &m.assert
	e.editing = true
	e.newRow = newRow
	e.err = ""
	text := ""
	if !newRow && e.cursor < len(e.rules) {
		text = domain.FormatAssertion(e.rules[e.cursor])
	}
	e.input.SetValue(text)
	e.sel.Collapse(0)
	e.input.Focus()
	return m
}

// finishAssertRowEdit commits the row under the cursor. An invalid row keeps
// the editor open with the error shown.
func (m Model) finishAssertRowEdit() Model {
	e := &m.assert
	rules, err := domain.ParseAssertionText(e.input.Value())
	if err == nil && len(rules) != 1 {
		err = fmt.Errorf("enter exactly one assertion per row")
	}
	if err != nil {
		e.err = err.Error()
		return m
	}
	if e.cursor < len(e.rules) {
		e.rules[e.cursor] = rules[0]
	}
	e.editing = false
	e.newRow = false
	e.err = ""
	e.input.Blur()
	return m
}

// cancelAssertRowEdit leaves the row edit. A row added by the edit is removed.
func (m Model) cancelAssertRowEdit() Model {
	e := &m.assert
	if e.newRow {
		e.rules = removeAssertRow(e.rules, e.cursor)
		if e.cursor >= len(e.rules) && e.cursor > 0 {
			e.cursor--
		}
	}
	e.editing = false
	e.newRow = false
	e.err = ""
	e.input.Blur()
	return m
}

func removeAssertRow(rules []domain.Assertion, i int) []domain.Assertion {
	if i < 0 || i >= len(rules) {
		return rules
	}
	return append(rules[:i], rules[i+1:]...)
}

func (m Model) handleAssertFieldKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.assert.editing {
		return m.handleAssertRowKey(msg)
	}
	action, ok := m.resolver.Resolve(assertResolverMode, 0, msg)
	if !ok {
		return m, nil
	}
	e := &m.assert
	switch action {
	case "save":
		return m.saveAssertions()
	case keybindings.ActionCancel:
		m.activeField = noneField
		m.assert = assertEditor{}
	case keybindings.ActionNavigateDown:
		if e.cursor < len(e.rules)-1 {
			e.cursor++
		}
	case keybindings.ActionNavigateUp:
		if e.cursor > 0 {
			e.cursor--
		}
	case "add_rule":
		e.rules = append(e.rules, domain.Assertion{})
		e.cursor = len(e.rules) - 1
		return m.beginAssertRowEdit(true), textinput.Blink
	case "delete_rule":
		e.rules = removeAssertRow(e.rules, e.cursor)
		if e.cursor >= len(e.rules) && e.cursor > 0 {
			e.cursor--
		}
	case "edit_rule":
		if e.cursor < len(e.rules) {
			return m.beginAssertRowEdit(false), textinput.Blink
		}
	}
	return m, nil
}

// handleAssertRowKey routes keys while one row is being edited. Only save and
// cancel are intercepted; every other key is text.
func (m Model) handleAssertRowKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if action, ok := m.resolver.Resolve(assertResolverMode, 0, msg); ok {
		switch action {
		case "save":
			return m.finishAssertRowEdit(), nil
		case keybindings.ActionCancel:
			return m.cancelAssertRowEdit(), nil
		}
	}
	ed := m.newTextEditor()
	b := textedit.Input(&m.assert.input)
	if cmd, handled := ed.HandleKey(&m.assert.sel, b, msg); handled {
		return m, cmd
	}
	if m.textKeys().Copy.Match(msg.String()) {
		return m.ctrlCNoSelection(), nil
	}
	var cmd tea.Cmd
	m.assert.input, cmd = m.assert.input.Update(msg)
	ed.Sync(&m.assert.sel, b)
	return m, cmd
}

// saveAssertions persists the assertion list to the active request.
func (m Model) saveAssertions() (Model, tea.Cmd) {
	if m.activeRequest == nil {
		return m, nil
	}
	m.activeRequest.Assertions = domain.EncodeAssertions(m.assert.rules)
	m.activeField = noneField
	m.assert = assertEditor{}
	m = m.status("success", "Assertions updated")
	return m, saveRequestCmd(m.ctx, m.writer, m.reader, m.activeRequest)
}

// viewAssertEditor renders the assertion list, keeping the cursor row visible.
func (m Model) viewAssertEditor(maxRows, width int) string {
	e := m.assert
	var sb strings.Builder
	if e.editing {
		sb.WriteString(
			"Assertion (status = N | time < D | $.path exists | $.path = v | header N = v | header N contains v):\n",
		)
		sb.WriteString(textedit.RenderInput(&e.input, e.sel, m.styles.selectionStyle))
		sb.WriteString("\n")
		if e.err != "" {
			sb.WriteString(m.styles.errorStyle.Render("  " + e.err))
			sb.WriteString("\n")
		}
		return sb.String()
	}
	if len(e.rules) == 0 {
		sb.WriteString(m.styles.mutedStyle.Render(
			"  No assertions. Press " +
				m.renderHintKeys([]string{"assert_add"}, false) + " to add.\n"))
		return sb.String()
	}
	start := 0
	if e.cursor >= maxRows-1 {
		start = e.cursor - maxRows + 2
	}
	end := min(len(e.rules), start+max(1, maxRows-1))
	for i := start; i < end; i++ {
		cursor := "  "
		if i == e.cursor {
			cursor = "▸ "
		}
		line := truncate(cursor+domain.FormatAssertion(e.rules[i]), width-2)
		if i == e.cursor {
			line = lipgloss.NewStyle().Bold(true).Render(line)
		}
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	return sb.String()
}

// assertionStatus summarizes assertion results for the status line. It
// returns an empty text when the request has no assertions.
func assertionStatus(results []exec.AssertionResult) (kind, text string) {
	if len(results) == 0 {
		return "", ""
	}
	passed := 0
	firstFailure := ""
	for _, r := range results {
		if r.Passed {
			passed++
			continue
		}
		if firstFailure == "" {
			firstFailure = r.Rule + ": " + r.Reason
		}
	}
	if firstFailure != "" {
		return "error", fmt.Sprintf("Assertions: %d of %d failed (%s)",
			len(results)-passed, len(results), firstFailure)
	}
	return "success", fmt.Sprintf("Assertions: %d of %d passed", passed, len(results))
}
