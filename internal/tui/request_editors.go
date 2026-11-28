package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/format"
)

func requestContentType(req *domain.Request) string {
	if req == nil {
		return ""
	}
	for _, pair := range parseHeadersJSON(req.Headers) {
		if strings.EqualFold(strings.TrimSpace(pair.Key), headerContentType) {
			return strings.TrimSpace(pair.Value)
		}
	}
	return ""
}

func (m Model) clearStatus() Model {
	return m.status("", "")
}

func (m Model) beginURLEdit() (Model, tea.Cmd) {
	m.preEditURL = m.urlInput.Value()
	m.activeField = urlField
	m.urlInput.Focus()
	return m, textinput.Blink
}

func (m Model) beginBodyEdit() Model {
	if m.activeRequest == nil {
		return m.status("error", "Select a request first")
	}
	if requestBodyType(m.activeRequest) == domain.BodyTypeFormURLEncoded {
		return m.beginBodyFormEdit()
	}
	m.preEditBody = m.activeRequest.Body
	m.bodyTextarea.SetValue(m.activeRequest.Body)
	m.bodyTextarea.Focus()
	m.bodySelection.Collapse(0)
	m.activeField = bodyField
	m = m.clearStatus()
	return m.resizeBodyTextarea()
}

func (m Model) beginAuthEdit() Model {
	if m.activeRequest == nil {
		return m.status("error", "Select a request first")
	}
	m.authEditor = newAuthEditor(m.activeRequest)
	m.styleInputs()
	m.activeField = authField
	return m.clearStatus()
}

func (m Model) beginHeadersEdit() Model {
	if m.activeRequest == nil {
		return m.status("error", "Select a request first")
	}
	if !headersJSONValid(m.activeRequest.Headers) {
		// parseHeadersJSON would return nothing, the editor would look empty,
		// and saving it would replace the stored headers with {}.
		return m.status("error",
			"This request's stored headers are not valid JSON; refusing to edit and overwrite them")
	}
	m.headerPairs = parseHeadersJSON(m.activeRequest.Headers)
	m.headerCursor = 0
	m.headerEditing = false
	m.activeField = headersField
	return m.clearStatus()
}

func (m Model) beginHeaderPairEdit(pair headerPair) (Model, tea.Cmd) {
	m.headerEditing = true
	m.headerKeyInput.SetValue(pair.Key)
	m.headerValueInput.SetValue(pair.Value)
	m.headerKeyInput.Focus()
	m.headerValueInput.Blur()
	return m, textinput.Blink
}

func (m Model) finishHeaderPairEdit() Model {
	if m.headerCursor < len(m.headerPairs) {
		m.headerPairs[m.headerCursor] = headerPair{
			Key:   m.headerKeyInput.Value(),
			Value: m.headerValueInput.Value(),
		}
	}
	m.headerEditing = false
	m.headerKeyInput.Blur()
	m.headerValueInput.Blur()
	return m
}

func (m Model) cancelActiveFieldEdit() Model {
	switch m.activeField {
	case urlField:
		m.activeField = noneField
		m.urlInput.SetValue(m.preEditURL)
		m.urlInput.Blur()
		m.urlSelection.Collapse(0)
	case bodyField:
		m.activeField = noneField
		m.bodyTextarea.Blur()
		m.bodyTextarea.SetValue(m.preEditBody)
		m.bodySelection.Collapse(0)
	case authField:
		m.activeField = noneField
		m.authEditor = authEditor{}
	case headersField, bodyFormField:
		m.activeField = noneField
		m.headerPairs = nil
		m.headerEditing = false
		m.headerKeyInput.Blur()
		m.headerValueInput.Blur()
		m.headerKeySelection.Collapse(0)
		m.headerValueSelection.Collapse(0)
	}
	return m
}

// cycleMethod advances the HTTP method using the provided cycler and persists
// the change to the active request. Returns a save command when there is a
// saved active request to update.
func (m Model) cycleMethod(cycle func(string) string) (Model, tea.Cmd) {
	m.method = cycle(m.method)
	if m.activeRequest == nil {
		return m, nil
	}
	m.activeRequest.Method = m.method
	return m, saveRequestCmd(m.ctx, m.writer, m.reader, m.activeRequest)
}

// finishURLEdit commits the edited URL to the active request and persists it.
// Returns a save command when there is a saved active request to update.
func (m Model) finishURLEdit() (Model, tea.Cmd) {
	m.activeField = noneField
	m.urlInput.Blur()
	m.urlSelection.Collapse(0)
	if m.activeRequest == nil {
		return m, nil
	}
	m.activeRequest.URL = strings.TrimSpace(m.urlInput.Value())
	return m, saveRequestCmd(m.ctx, m.writer, m.reader, m.activeRequest)
}

func (m Model) finishBodyEdit() Model {
	body := m.bodyTextarea.Value()
	statusMsg := "Body updated"

	// Format JSON if enabled and the body is valid JSON.
	if m.cfg.Format.JSONOnSave {
		if formatted, changed := format.FormatJSONIfValid(body); changed {
			body = formatted
			statusMsg = "Body updated (JSON formatted)"
		}
	}

	m.activeRequest.Body = body
	m.bodyTextarea.Blur()
	m.activeField = noneField
	return m.status("success", statusMsg)
}

func (m Model) finishAuthEdit() Model {
	m.activeRequest.AuthType = m.authEditor.authType
	if m.authEditor.authType == domain.AuthTypeNone {
		m.activeRequest.AuthConfig = "{}"
	} else {
		m.activeRequest.AuthConfig = marshalAuthConfig(m.authEditor.config())
	}
	m.activeField = noneField
	m.authEditor = authEditor{}
	return m.status("success", "Auth updated")
}
