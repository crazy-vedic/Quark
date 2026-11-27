package tui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/keybindings"
	"github.com/crazy-vedic/quark/internal/tui/textedit"
)

// optionRowID identifies one row of the request options editor.
type optionRowID int

const (
	optionRowTimeout optionRowID = iota
	optionRowFollow
	optionRowMaxRedirects
	optionRowProxy
	optionRowSkipTLS
)

var optionRows = []optionRowID{
	optionRowTimeout,
	optionRowFollow,
	optionRowMaxRedirects,
	optionRowProxy,
	optionRowSkipTLS,
}

// followChoice is the three-way redirect setting: keep Quark's configured
// policy, follow redirects, or do not follow them.
type followChoice int

const (
	followDefault followChoice = iota
	followOn
	followOff
)

// optionsEditor edits a request's per-request options. Empty text rows keep
// the configured defaults.
type optionsEditor struct {
	cursor            int
	editing           bool
	follow            followChoice
	skipTLS           bool
	timeoutInput      textinput.Model
	maxRedirectsInput textinput.Model
	proxyInput        textinput.Model
	timeoutSel        textedit.Selection
	maxRedirectsSel   textedit.Selection
	proxySel          textedit.Selection
	err               string
}

func newOptionsEditor(req *domain.Request) optionsEditor {
	ed := optionsEditor{
		timeoutInput:      textinput.New(),
		maxRedirectsInput: textinput.New(),
		proxyInput:        textinput.New(),
	}
	opts := req.Options
	if opts.TimeoutMS > 0 {
		ed.timeoutInput.SetValue(opts.Timeout().String())
	}
	if opts.FollowRedirects != nil {
		ed.follow = followOff
		if *opts.FollowRedirects {
			ed.follow = followOn
		}
	}
	if opts.MaxRedirects != nil {
		ed.maxRedirectsInput.SetValue(strconv.Itoa(*opts.MaxRedirects))
	}
	ed.proxyInput.SetValue(opts.Proxy)
	ed.skipTLS = opts.SkipTLSVerify
	return ed
}

func (e *optionsEditor) clampCursor() {
	e.cursor = min(max(0, e.cursor), len(optionRows)-1)
}

func (e optionsEditor) currentRow() optionRowID {
	e.clampCursor()
	return optionRows[e.cursor]
}

// beginEdit focuses the text input of the current row. It reports false for
// rows that cycle their value instead of accepting text.
func (e *optionsEditor) beginEdit() bool {
	switch e.currentRow() {
	case optionRowTimeout:
		e.timeoutInput.Focus()
	case optionRowMaxRedirects:
		e.maxRedirectsInput.Focus()
	case optionRowProxy:
		e.proxyInput.Focus()
	default:
		return false
	}
	e.editing = true
	return true
}

func (e *optionsEditor) endEdit() {
	e.editing = false
	e.timeoutInput.Blur()
	e.maxRedirectsInput.Blur()
	e.proxyInput.Blur()
}

// cycle moves a choice row (redirects, TLS) by delta. Text rows are unchanged.
func (e *optionsEditor) cycle(delta int) {
	switch e.currentRow() {
	case optionRowFollow:
		count := int(followOff) + 1
		e.follow = followChoice((int(e.follow) + delta + count) % count)
	case optionRowSkipTLS:
		e.skipTLS = !e.skipTLS
	}
}

// input returns the selection and widget behind a text row, or nil for other rows.
func (e *optionsEditor) input(row optionRowID) (*textedit.Selection, *textinput.Model) {
	switch row {
	case optionRowTimeout:
		return &e.timeoutSel, &e.timeoutInput
	case optionRowMaxRedirects:
		return &e.maxRedirectsSel, &e.maxRedirectsInput
	case optionRowProxy:
		return &e.proxySel, &e.proxyInput
	}
	return nil, nil
}

// options parses the editor's rows into request options. A row that cannot be
// applied is reported by name, and nothing is saved.
func (e optionsEditor) options() (domain.RequestOptions, error) {
	var opts domain.RequestOptions
	if raw := strings.TrimSpace(e.timeoutInput.Value()); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d < time.Millisecond {
			return domain.RequestOptions{},
				errors.New("timeout must be a duration of at least 1ms, such as 30s or 1500ms")
		}
		opts.TimeoutMS = int(d / time.Millisecond)
	}
	switch e.follow {
	case followOn:
		opts.FollowRedirects = boolPtr(true)
	case followOff:
		opts.FollowRedirects = boolPtr(false)
	}
	if raw := strings.TrimSpace(e.maxRedirectsInput.Value()); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 0 {
			return domain.RequestOptions{},
				errors.New("max redirects must be a whole number, 0 or more")
		}
		opts.MaxRedirects = &limit
	}
	if raw := strings.TrimSpace(e.proxyInput.Value()); raw != "" {
		proxy, err := domain.NormalizeProxyURL(raw)
		if err != nil {
			return domain.RequestOptions{}, fmt.Errorf("proxy: %w", err)
		}
		opts.Proxy = proxy
	}
	opts.SkipTLSVerify = e.skipTLS
	if err := opts.Validate(); err != nil {
		return domain.RequestOptions{}, err
	}
	return opts, nil
}

func boolPtr(v bool) *bool { return &v }

func optionRowLabel(row optionRowID) string {
	switch row {
	case optionRowTimeout:
		return "Timeout"
	case optionRowFollow:
		return "Follow redirects"
	case optionRowMaxRedirects:
		return "Max redirects"
	case optionRowProxy:
		return "Proxy"
	case optionRowSkipTLS:
		return "Skip TLS verify"
	}
	return ""
}

func (e optionsEditor) valueForRow(row optionRowID, muted lipgloss.Style) string {
	switch row {
	case optionRowTimeout:
		return textValueOrPlaceholder(e.timeoutInput, e.editing, "(default)", muted)
	case optionRowFollow:
		switch e.follow {
		case followOn:
			return "follow"
		case followOff:
			return "do not follow"
		default:
			return muted.Render("(default)")
		}
	case optionRowMaxRedirects:
		return textValueOrPlaceholder(e.maxRedirectsInput, e.editing, "(default)", muted)
	case optionRowProxy:
		if e.editing && e.proxyInput.Focused() {
			return e.proxyInput.View()
		}
		if e.proxyInput.Value() == "" {
			return muted.Render("(none)")
		}
		// The password in a proxy URL stays hidden unless the row is being edited.
		return domain.RedactProxyURL(e.proxyInput.Value())
	case optionRowSkipTLS:
		if e.skipTLS {
			return "yes"
		}
		return "no"
	}
	return ""
}

// textValueOrPlaceholder shows the text input while it is being edited, the
// value otherwise, or a muted placeholder when the value is empty.
func textValueOrPlaceholder(
	in textinput.Model,
	editing bool,
	placeholder string,
	muted lipgloss.Style,
) string {
	if editing && in.Focused() {
		return in.View()
	}
	if in.Value() == "" {
		return muted.Render(placeholder)
	}
	return in.Value()
}

// viewOptionsEditor renders the options rows inside the request pane.
func (m Model) viewOptionsEditor(_ int, contentWidth int) string {
	ed := m.optionsEditor
	selected := min(max(0, ed.cursor), len(optionRows)-1)
	var sb strings.Builder
	sb.WriteString(m.styles.titleStyle.Render("Options"))
	sb.WriteString("\n")
	for idx, row := range optionRows {
		cursor := "  "
		if idx == selected {
			cursor = "▸ "
		}
		label := lipgloss.NewStyle().Foreground(m.styles.info).Render(optionRowLabel(row))
		value := ed.valueForRow(row, m.styles.mutedStyle)
		if ed.editing && idx == selected {
			if sel, in := ed.input(row); in != nil && sel.Active() {
				value = textedit.RenderInput(in, *sel, m.styles.selectionStyle)
			}
		}
		line := cursor + label + ": " + value
		if idx == selected {
			line = lipgloss.NewStyle().Bold(true).Render(line)
		}
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	if ed.err != "" {
		sb.WriteString(m.styles.errorStyle.Render(truncate("  "+ed.err, contentWidth)))
		sb.WriteString("\n")
	}
	return sb.String()
}

func (m Model) beginOptionsEdit() Model {
	if m.activeRequest == nil {
		return m.status("error", "Select a request first")
	}
	m.optionsEditor = newOptionsEditor(m.activeRequest)
	m.styleInputs()
	m.activeField = optionsField
	return m.clearStatus()
}

func (m Model) handleOptionsFieldKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.optionsEditor.editing {
		return m.handleOptionsFieldEdit(msg)
	}
	action, ok := m.resolver.Resolve(10, 0, msg)
	if !ok {
		return m, nil
	}
	switch action {
	case "save":
		return m.saveOptions()
	case keybindings.ActionNavigateDown:
		m.optionsEditor.cursor++
		m.optionsEditor.clampCursor()
	case keybindings.ActionNavigateUp:
		m.optionsEditor.cursor--
		m.optionsEditor.clampCursor()
	case "edit":
		if m.optionsEditor.beginEdit() {
			return m, textinput.Blink
		}
		m.optionsEditor.cycle(1)
	case "option_next":
		m.optionsEditor.cycle(1)
	case "option_prev":
		m.optionsEditor.cycle(-1)
	case keybindings.ActionCancel:
		return m.cancelActiveFieldEdit(), nil
	}
	return m, nil
}

func (m Model) handleOptionsFieldEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	sel, input := m.optionsEditor.input(m.optionsEditor.currentRow())
	if input == nil {
		m.optionsEditor.endEdit()
		return m, nil
	}
	ed := m.newTextEditor()
	b := textedit.Input(input)
	if cmd, handled := ed.HandleKey(sel, b, msg); handled {
		return m, cmd
	}
	if m.textKeys().Copy.Match(msg.String()) {
		// No selection: nothing to copy. Ctrl+C never quits the app.
		return m.ctrlCNoSelection(), nil
	}
	if msg.Type == tea.KeyEnter {
		m.optionsEditor.endEdit()
		return m, nil
	}
	var cmd tea.Cmd
	*input, cmd = input.Update(msg)
	ed.Sync(sel, b)
	return m, cmd
}

// saveOptions stores the editor's options on the active request. Invalid rows
// keep the editor open with the reason shown.
func (m Model) saveOptions() (tea.Model, tea.Cmd) {
	if m.activeRequest == nil {
		return m, nil
	}
	opts, err := m.optionsEditor.options()
	if err != nil {
		m.optionsEditor.err = err.Error()
		return m, nil
	}
	m.activeRequest.Options = opts
	m.activeField = noneField
	m.optionsEditor = optionsEditor{}
	m = m.status("success", "Options updated")
	return m, saveRequestCmd(m.ctx, m.writer, m.reader, m.activeRequest)
}
