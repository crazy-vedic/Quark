package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/keybindings"
)

// The path editor reuses the header pair editor state. Each row is one URL
// path placeholder: its key is the name (shown as :name) and its value is
// stored on the request (RequestParams.PathValues). The URL decides which
// rows exist, so rows are never added or removed here and only values can
// be edited. The URL itself is not changed; the values are filled in when the
// request is sent.

func (m Model) beginPathEdit() Model {
	if m.activeRequest == nil {
		return m.status("error", "Select a request first")
	}
	stored, err := domain.ParseRequestParams(m.activeRequest.Params)
	if err != nil {
		return m.status(
			"error",
			"This request's stored path params are not valid JSON; refusing to edit and overwrite them",
		)
	}
	names := domain.PathPlaceholders(m.urlInput.Value())
	pairs := make([]headerPair, 0, len(names))
	for _, name := range names {
		pairs = append(pairs, headerPair{Key: name, Value: stored.PathValues[name]})
	}
	m.headerPairs = pairs
	m.headerCursor = 0
	m.headerEditing = false
	m.activeField = pathField
	return m.clearStatus()
}

// beginPathValueEdit edits the value of the selected placeholder. The name
// is shown but not focused, so it cannot be typed over.
func (m Model) beginPathValueEdit() (Model, tea.Cmd) {
	pair := m.headerPairs[m.headerCursor]
	m.headerEditing = true
	m.headerKeyInput.SetValue(pair.Key)
	m.headerValueInput.SetValue(pair.Value)
	m.headerKeyInput.Blur()
	m.headerValueInput.Focus()
	return m, textinput.Blink
}

func (m Model) handlePathFieldKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.headerEditing {
		return m.handleHeadersFieldKey(msg)
	}
	action, ok := m.resolver.Resolve(5, 0, msg)
	if !ok {
		return m, nil
	}
	switch action {
	case "save":
		return m.savePath()
	case keybindings.ActionNavigateDown:
		if m.headerCursor < len(m.headerPairs)-1 {
			m.headerCursor++
		}
	case keybindings.ActionNavigateUp:
		if m.headerCursor > 0 {
			m.headerCursor--
		}
	case "edit_pair":
		if m.headerCursor < len(m.headerPairs) {
			return m.beginPathValueEdit()
		}
	}
	return m, nil
}

func (m Model) savePath() (Model, tea.Cmd) {
	if m.activeRequest == nil {
		return m, nil
	}
	stored, err := domain.ParseRequestParams(m.activeRequest.Params)
	if err != nil {
		return m.status("error",
			"This request's stored path params are not valid JSON; not saving over them"), nil
	}
	stored.PathValues = nil
	for _, p := range m.headerPairs {
		if p.Value == "" {
			continue
		}
		if stored.PathValues == nil {
			stored.PathValues = make(map[string]string)
		}
		stored.PathValues[p.Key] = p.Value
	}
	m.activeRequest.Params = stored.String()
	m.headerPairs = nil
	m.activeField = noneField
	m = m.status("success", "Path params updated")
	return m, saveRequestCmd(m.ctx, m.writer, m.reader, m.activeRequest)
}

// viewPathPreview renders the placeholder list, or the value being edited.
func (m Model) viewPathPreview(w int) string {
	if m.headerEditing {
		return m.viewHeadersPreview(w)
	}
	var preview strings.Builder
	if len(m.headerPairs) == 0 {
		preview.WriteString(m.styles.mutedStyle.Render(
			"  No path placeholders in the URL. Add a segment such as /users/:id to use one.\n",
		))
		return preview.String()
	}
	for i, p := range m.headerPairs {
		cursor := "  "
		if i == m.headerCursor {
			cursor = "▸ "
		}
		value := p.Value
		if value == "" {
			value = "(not set: sent as written)"
		}
		plain := truncate(cursor+":"+p.Key+" = "+value, w-4)
		var line string
		switch {
		case i == m.headerCursor:
			line = lipgloss.NewStyle().Bold(true).Render(plain)
		case p.Value == "":
			line = m.styles.mutedStyle.Render(plain)
		default:
			line = lipgloss.NewStyle().Foreground(m.styles.info).Render(plain)
		}
		preview.WriteString(line)
		preview.WriteString("\n")
	}
	preview.WriteString(m.styles.mutedStyle.Render(truncate(m.renderHints([]hintItem{
		{Label: "edit", Actions: []string{"header_edit"}},
		{Label: "save", Actions: []string{"header_save"}},
	}), w-4)))
	preview.WriteString("\n")
	return preview.String()
}
