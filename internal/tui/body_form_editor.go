package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/crazy-vedic/quark/internal/domain"
)

// The form body editor reuses the header pair editor state (headerPairs,
// headerCursor, headerEditing and the key/value inputs). Each row is one
// form field. The stored body is the urlencoded text itself, so rows are
// decoded when the editor opens and encoded again when it is saved.

// requestBodyType returns the request's body type, treating a blank or
// unknown value as raw.
func requestBodyType(req *domain.Request) domain.BodyType {
	bodyType, err := domain.NormalizeBodyType(req.BodyType)
	if err != nil {
		return domain.BodyTypeRaw
	}
	return bodyType
}

// cycleBodyType moves the active request to the next body type and saves it.
func (m Model) cycleBodyType() (Model, tea.Cmd) {
	if m.activeRequest == nil {
		return m.status("error", "Select a request first"), nil
	}
	m.activeRequest.BodyType = domain.NextBodyType(requestBodyType(m.activeRequest))
	m = m.status("success", "Body type: "+m.activeRequest.BodyType)
	return m, saveRequestCmd(m.ctx, m.writer, m.reader, m.activeRequest)
}

// The rows of a multipart body are its parts. A row whose value starts with
// '@' is a file part, and the rest of the value is its path (as in curl -F).
// A text value that starts with '@' is written with one more '@' in front.

// multipartRowValue shows a stored part as a row value.
func multipartRowValue(part domain.MultipartPart) string {
	if part.File != "" {
		return "@" + part.File
	}
	if strings.HasPrefix(part.Value, "@") {
		return "@" + part.Value
	}
	return part.Value
}

// multipartPartFromRow reads a row value back as a part.
func multipartPartFromRow(name, value string) domain.MultipartPart {
	if strings.HasPrefix(value, "@@") {
		return domain.MultipartPart{Name: name, Value: value[1:]}
	}
	if strings.HasPrefix(value, "@") {
		return domain.MultipartPart{Name: name, File: value[1:]}
	}
	return domain.MultipartPart{Name: name, Value: value}
}

func (m Model) beginBodyFormEdit() Model {
	var pairs []headerPair
	if requestBodyType(m.activeRequest) == domain.BodyTypeMultipart {
		parts, err := domain.ParseMultipartParts(m.activeRequest.Body)
		if err != nil {
			return m.status(
				"error",
				"This request's stored multipart body is not valid; refusing to edit and overwrite it",
			)
		}
		for _, part := range parts {
			pairs = append(pairs, headerPair{Key: part.Name, Value: multipartRowValue(part)})
		}
	} else {
		for _, f := range domain.ParseFormBody(m.activeRequest.Body) {
			pairs = append(pairs, headerPair{Key: f.Key, Value: f.Value})
		}
	}
	m.headerPairs = pairs
	m.headerCursor = 0
	m.headerEditing = false
	m.activeField = bodyFormField
	return m.clearStatus()
}

func (m Model) handleBodyFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if !m.headerEditing {
		if action, ok := m.resolver.Resolve(5, 0, msg); ok && action == "save" {
			return m.saveBodyForm()
		}
	}
	// Navigation, add, edit and delete behave as in the header editor.
	return m.handleHeadersFieldKey(msg)
}

func (m Model) saveBodyForm() (Model, tea.Cmd) {
	if m.activeRequest == nil {
		return m, nil
	}
	if requestBodyType(m.activeRequest) == domain.BodyTypeMultipart {
		parts := make([]domain.MultipartPart, 0, len(m.headerPairs))
		for _, p := range m.headerPairs {
			if p.Key == "" && p.Value == "" {
				continue
			}
			parts = append(parts, multipartPartFromRow(p.Key, p.Value))
		}
		body, err := domain.MarshalMultipartParts(parts)
		if err != nil {
			return m.status("error", "Could not save multipart body: "+err.Error()), nil
		}
		m.activeRequest.Body = body
	} else {
		fields := make([]domain.FormField, 0, len(m.headerPairs))
		for _, p := range m.headerPairs {
			fields = append(fields, domain.FormField{Key: p.Key, Value: p.Value})
		}
		m.activeRequest.Body = domain.EncodeFormBody(fields)
	}
	m.headerPairs = nil
	m.activeField = noneField
	if requestBodyType(m.activeRequest) == domain.BodyTypeMultipart {
		m = m.status("success", "Multipart body updated")
	} else {
		m = m.status("success", "Form body updated")
	}
	return m, saveRequestCmd(m.ctx, m.writer, m.reader, m.activeRequest)
}

// viewBodyFormPreview renders the field list, or the row being edited.
func (m Model) viewBodyFormPreview(w int) string {
	if m.headerEditing {
		return m.viewHeadersPreview(w)
	}
	var preview strings.Builder
	if len(m.headerPairs) == 0 {
		preview.WriteString(m.styles.mutedStyle.Render(
			"  No form fields. Press " + m.renderHintKeys(
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
		plain := truncate(cursor+p.Key+" = "+p.Value, w-4)
		var line string
		if i == m.headerCursor {
			line = lipgloss.NewStyle().Bold(true).Render(plain)
		} else {
			line = lipgloss.NewStyle().Foreground(m.styles.info).Render(plain)
		}
		preview.WriteString(line)
		preview.WriteString("\n")
	}
	preview.WriteString(m.styles.mutedStyle.Render(truncate(m.renderHints([]hintItem{
		{Label: "add", Actions: []string{"header_add"}},
		{Label: "edit", Actions: []string{"header_edit"}},
		{Label: "delete", Actions: []string{"header_delete"}},
		{Label: "save", Actions: []string{"header_save"}},
	}), w-4)))
	preview.WriteString("\n")
	return preview.String()
}
