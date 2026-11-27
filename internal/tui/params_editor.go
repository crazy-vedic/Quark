package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/keybindings"
)

// The query param editor reuses the header pair editor state (headerPairs,
// headerCursor, headerEditing and the key/value inputs). The URL stays the
// source of truth for enabled params: every change to the rows is written
// back to the URL field at once, and the editor is rebuilt from the URL each
// time it opens. Disabled params cannot live in the URL, so they are kept in
// Request.Params.

// pairEditorActive reports whether the pair editor (headers, query params or
// path params) owns the request pane.
func (m Model) pairEditorActive() bool {
	return m.activeField == headersField || m.activeField == paramsField ||
		m.activeField == pathField
}

func (m Model) beginParamsEdit() Model {
	if m.activeRequest == nil {
		return m.status("error", "Select a request first")
	}
	stored, err := domain.ParseRequestParams(m.activeRequest.Params)
	if err != nil {
		return m.status(
			"error",
			"This request's stored query params are not valid JSON; refusing to edit and overwrite them",
		)
	}
	m.preEditURL = m.urlInput.Value()
	var pairs []headerPair
	for _, p := range domain.ParseURLQuery(m.urlInput.Value()) {
		pairs = append(pairs, headerPair{Key: p.Key, Value: p.Value, raw: p.Raw})
	}
	for _, p := range stored.DisabledQuery {
		pairs = append(pairs, headerPair{Key: p.Key, Value: p.Value, Disabled: true})
	}
	m.headerPairs = pairs
	m.headerCursor = 0
	m.headerEditing = false
	m.activeField = paramsField
	return m.clearStatus()
}

func queryParamsFromPairs(pairs []headerPair) []domain.QueryParam {
	params := make([]domain.QueryParam, 0, len(pairs))
	for _, p := range pairs {
		params = append(params, domain.QueryParam{
			Key: p.Key, Value: p.Value, Disabled: p.Disabled, Raw: p.raw,
		})
	}
	return params
}

// syncParamsURL rewrites the URL field's query from the enabled rows so the
// URL bar always matches the editor.
func (m Model) syncParamsURL() Model {
	m.urlInput.SetValue(domain.SetURLQuery(m.urlInput.Value(), queryParamsFromPairs(m.headerPairs)))
	return m
}

func (m Model) handleParamsFieldKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if !m.headerEditing {
		if action, ok := m.resolver.Resolve(5, 0, msg); ok {
			switch action {
			case "save":
				return m.saveParams()
			case keybindings.ActionTogglePair:
				if m.headerCursor < len(m.headerPairs) {
					m.headerPairs[m.headerCursor].Disabled = !m.headerPairs[m.headerCursor].Disabled
				}
				return m.syncParamsURL(), nil
			case "delete_pair":
				next, cmd := m.handleHeadersFieldKey(msg)
				if model, ok := next.(Model); ok {
					return model.syncParamsURL(), cmd
				}
				return next, cmd
			}
		}
	}
	// Navigation, add and edit behave exactly as in the header editor;
	// finishHeaderPairEdit syncs the URL when a row edit is confirmed.
	return m.handleHeadersFieldKey(msg)
}

func (m Model) saveParams() (Model, tea.Cmd) {
	if m.activeRequest == nil {
		return m, nil
	}
	// Parsed successfully in beginParamsEdit; keep any other stored fields.
	stored, _ := domain.ParseRequestParams(m.activeRequest.Params)
	stored.DisabledQuery = nil
	for _, p := range m.headerPairs {
		if p.Disabled && (p.Key != "" || p.Value != "") {
			stored.DisabledQuery = append(
				stored.DisabledQuery,
				domain.QueryParam{Key: p.Key, Value: p.Value},
			)
		}
	}
	m = m.syncParamsURL()
	m.activeRequest.URL = strings.TrimSpace(m.urlInput.Value())
	m.activeRequest.Params = stored.String()
	m.headerPairs = nil
	m.activeField = noneField
	m = m.status("success", "Query params updated")
	return m, saveRequestCmd(m.ctx, m.writer, m.reader, m.activeRequest)
}

// viewParamsPreview renders the query param list, or the row being edited.
func (m Model) viewParamsPreview(w int) string {
	if m.headerEditing {
		return m.viewHeadersPreview(w)
	}
	var preview strings.Builder
	if len(m.headerPairs) == 0 {
		preview.WriteString(m.styles.mutedStyle.Render(
			"  No query params. Press " + m.renderHintKeys(
				[]string{"header_add"},
				false,
			) + " to add.\n",
		))
		return preview.String()
	}
	for i, p := range m.headerPairs {
		cursor := "  "
		if i == m.headerCursor {
			cursor = "▸ "
		}
		check := "[x] "
		if p.Disabled {
			check = "[ ] "
		}
		plain := truncate(cursor+check+p.Key+"="+p.Value, w-4)
		var line string
		switch {
		case i == m.headerCursor:
			line = lipgloss.NewStyle().Bold(true).Render(plain)
		case p.Disabled:
			line = m.styles.mutedStyle.Render(plain)
		default:
			line = lipgloss.NewStyle().Foreground(m.styles.info).Render(plain)
		}
		preview.WriteString(line)
		preview.WriteString("\n")
	}
	preview.WriteString(m.styles.mutedStyle.Render(truncate(m.renderHints([]hintItem{
		{Label: "toggle", Actions: []string{"param_toggle"}},
		{Label: "add", Actions: []string{"header_add"}},
		{Label: "edit", Actions: []string{"header_edit"}},
		{Label: "delete", Actions: []string{"header_delete"}},
		{Label: "save", Actions: []string{"header_save"}},
	}), w-4)))
	preview.WriteString("\n")
	return preview.String()
}
