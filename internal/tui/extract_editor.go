package tui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/jsonpath"
	"github.com/crazy-vedic/quark/internal/keybindings"
	"github.com/crazy-vedic/quark/internal/tui/textedit"
)

// extractResolverMode is the keybindings mode of the extraction rule editor.
const extractResolverMode = 10

// extractEditor is the request pane's list of extraction rules. Each row is
// one rule, shown as "variable = $.path" or "variable = header Name". The
// two inputs edit the row under the cursor.
type extractEditor struct {
	rules    []domain.Extraction
	cursor   int
	editing  bool
	newRow   bool // the row being edited was just added; cancel removes it
	varInput textinput.Model
	srcInput textinput.Model
	varSel   textedit.Selection
	srcSel   textedit.Selection
	err      string
}

// newExtractInputs returns the two inputs used to edit one rule row.
func newExtractInputs() (textinput.Model, textinput.Model) {
	varInput := textinput.New()
	varInput.Placeholder = "variable"
	varInput.CharLimit = 128
	srcInput := textinput.New()
	srcInput.Placeholder = "$.path or header Name"
	srcInput.CharLimit = 512
	return varInput, srcInput
}

func newExtractEditor(req *domain.Request) (extractEditor, error) {
	rules, err := domain.ParseExtractions(req.Extractions)
	if err != nil {
		return extractEditor{}, err
	}
	varInput, srcInput := newExtractInputs()
	return extractEditor{rules: rules, varInput: varInput, srcInput: srcInput}, nil
}

// extractRuleText is the editable "$.path" or "header Name" form of a rule.
func extractRuleText(r domain.Extraction) string {
	if r.Source == domain.ExtractFromHeader {
		return "header " + r.Path
	}
	return r.Path
}

// parseExtractRuleRow turns the two inputs into one validated rule.
func parseExtractRuleRow(name, source string) (domain.Extraction, error) {
	rules, err := domain.ParseExtractionText(name + " = " + source)
	if err != nil {
		return domain.Extraction{}, err
	}
	if len(rules) != 1 {
		return domain.Extraction{}, fmt.Errorf("expected one rule")
	}
	return rules[0], nil
}

func (m Model) beginExtractEdit() Model {
	if m.activeRequest == nil {
		return m.status("error", "Select a request first")
	}
	ed, err := newExtractEditor(m.activeRequest)
	if err != nil {
		return m.status(
			"error",
			"This request's stored extraction rules are not valid JSON; refusing to edit and overwrite them",
		)
	}
	m.extract = ed
	m.styleInputs()
	m.activeField = extractField
	return m.clearStatus()
}

func (m Model) beginExtractRuleEdit(newRow bool) Model {
	e := &m.extract
	e.editing = true
	e.newRow = newRow
	e.err = ""
	rule := domain.Extraction{}
	if e.cursor < len(e.rules) {
		rule = e.rules[e.cursor]
	}
	e.varInput.SetValue(rule.Variable)
	e.srcInput.SetValue(extractRuleText(rule))
	e.varSel.Collapse(0)
	e.srcSel.Collapse(0)
	e.varInput.Focus()
	e.srcInput.Blur()
	return m
}

// finishExtractRuleEdit commits the row under the cursor. An invalid row keeps
// the editor open with the error shown.
func (m Model) finishExtractRuleEdit() Model {
	e := &m.extract
	rule, err := parseExtractRuleRow(e.varInput.Value(), e.srcInput.Value())
	if err == nil && rule.Source == domain.ExtractFromBody {
		if _, perr := jsonpath.Parse(rule.Path); perr != nil {
			err = perr
		}
	}
	if err != nil {
		e.err = err.Error()
		return m
	}
	if e.cursor < len(e.rules) {
		e.rules[e.cursor] = rule
	}
	e.editing = false
	e.newRow = false
	e.err = ""
	e.varInput.Blur()
	e.srcInput.Blur()
	return m
}

// cancelExtractRuleEdit leaves the row edit. A row added by the edit is
// removed again.
func (m Model) cancelExtractRuleEdit() Model {
	e := &m.extract
	if e.newRow && e.cursor < len(e.rules) {
		e.rules = append(e.rules[:e.cursor], e.rules[e.cursor+1:]...)
		if e.cursor >= len(e.rules) && e.cursor > 0 {
			e.cursor--
		}
	}
	e.editing = false
	e.newRow = false
	e.err = ""
	e.varInput.Blur()
	e.srcInput.Blur()
	return m
}

func (m Model) handleExtractFieldKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.extract.editing {
		return m.handleExtractRuleKey(msg)
	}
	action, ok := m.resolver.Resolve(extractResolverMode, 0, msg)
	if !ok {
		return m, nil
	}
	e := &m.extract
	switch action {
	case "save":
		return m.saveExtractions()
	case keybindings.ActionCancel:
		m.activeField = noneField
		m.extract = extractEditor{}
		return m, nil
	case keybindings.ActionNavigateDown:
		if e.cursor < len(e.rules)-1 {
			e.cursor++
		}
	case keybindings.ActionNavigateUp:
		if e.cursor > 0 {
			e.cursor--
		}
	case "add_rule":
		e.rules = append(e.rules, domain.Extraction{})
		e.cursor = len(e.rules) - 1
		return m.beginExtractRuleEdit(true), textinput.Blink
	case "delete_rule":
		if e.cursor < len(e.rules) {
			e.rules = append(e.rules[:e.cursor], e.rules[e.cursor+1:]...)
			if e.cursor >= len(e.rules) && e.cursor > 0 {
				e.cursor--
			}
		}
	case "edit_rule":
		if e.cursor < len(e.rules) {
			return m.beginExtractRuleEdit(false), textinput.Blink
		}
	}
	return m, nil
}

// handleExtractRuleKey routes keys while one row is being edited. Only save,
// cancel and switch_field are intercepted; every other key is text.
func (m Model) handleExtractRuleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	sel, input := &m.extract.srcSel, &m.extract.srcInput
	if m.extract.varInput.Focused() {
		sel, input = &m.extract.varSel, &m.extract.varInput
	}
	if action, ok := m.resolver.Resolve(extractResolverMode, 0, msg); ok {
		switch action {
		case "save":
			return m.finishExtractRuleEdit(), nil
		case keybindings.ActionCancel:
			return m.cancelExtractRuleEdit(), nil
		case "switch_field":
			if m.extract.varInput.Focused() {
				m.extract.varInput.Blur()
				m.extract.srcInput.Focus()
			} else {
				m.extract.srcInput.Blur()
				m.extract.varInput.Focus()
			}
			return m, nil
		}
	}
	ed := m.newTextEditor()
	b := textedit.Input(input)
	if cmd, handled := ed.HandleKey(sel, b, msg); handled {
		return m, cmd
	}
	if m.textKeys().Copy.Match(msg.String()) {
		return m.ctrlCNoSelection(), nil
	}
	var cmd tea.Cmd
	*input, cmd = input.Update(msg)
	ed.Sync(sel, b)
	return m, cmd
}

// saveExtractions persists the rule list to the active request.
func (m Model) saveExtractions() (Model, tea.Cmd) {
	if m.activeRequest == nil {
		return m, nil
	}
	m.activeRequest.Extractions = domain.EncodeExtractions(m.extract.rules)
	m.activeField = noneField
	m.extract = extractEditor{}
	m = m.status("success", "Extraction rules updated")
	return m, saveRequestCmd(m.ctx, m.writer, m.reader, m.activeRequest)
}

// viewExtractEditor renders the rule list, keeping the cursor row visible.
func (m Model) viewExtractEditor(maxRows, width int) string {
	e := m.extract
	var sb strings.Builder
	if e.editing {
		sb.WriteString("Variable:\n")
		sb.WriteString(textedit.RenderInput(&e.varInput, e.varSel, m.styles.selectionStyle))
		sb.WriteString("\n\nSource ($.path or header Name):\n")
		sb.WriteString(textedit.RenderInput(&e.srcInput, e.srcSel, m.styles.selectionStyle))
		sb.WriteString("\n")
		if e.err != "" {
			sb.WriteString(m.styles.errorStyle.Render("  " + e.err))
			sb.WriteString("\n")
		}
		return sb.String()
	}
	if len(e.rules) == 0 {
		sb.WriteString(m.styles.mutedStyle.Render(
			"  No extraction rules. Press " +
				m.renderHintKeys([]string{"extract_add"}, false) + " to add.\n"))
		return sb.String()
	}
	start := 0
	if e.cursor >= maxRows-1 {
		start = e.cursor - maxRows + 2
	}
	end := min(len(e.rules), start+max(1, maxRows-1))
	for i := start; i < end; i++ {
		r := e.rules[i]
		cursor := "  "
		if i == e.cursor {
			cursor = "▸ "
		}
		line := truncate(cursor+r.Variable+" = "+extractRuleText(r), width-2)
		if i == e.cursor {
			line = lipgloss.NewStyle().Bold(true).Render(line)
		}
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	return sb.String()
}

// extractionStatus summarizes the extraction outcomes of one response for the
// status line. It returns an empty text when the request had no rules.
func extractionStatus(outcomes []exec.ExtractionOutcome) (kind, text string) {
	if len(outcomes) == 0 {
		return "", ""
	}
	written := 0
	var failure error
	for _, o := range outcomes {
		switch {
		case o.Err == nil:
			written++
		case failure == nil && !errors.Is(o.Err, exec.ErrExtractionSkipped):
			failure = fmt.Errorf("%s: %w", o.Variable, o.Err)
		}
	}
	if failure != nil {
		return "error", "Extraction failed: " + failure.Error()
	}
	if written == 0 {
		return "warn", "Extraction skipped: response was not 2xx"
	}
	return "success", fmt.Sprintf("Extracted %d variable(s)", written)
}
