package tui

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/crazy-vedic/quark/internal/domain"
)

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
	m.preEditBody = m.activeRequest.Body
	m.bodyTextarea.SetValue(m.activeRequest.Body)
	m.bodyTextarea.Focus()
	m.activeField = bodyField
	m = m.clearStatus()
	return m.resizeBodyTextarea()
}

func (m Model) beginAuthEdit() Model {
	if m.activeRequest == nil {
		return m.status("error", "Select a request first")
	}
	m.authEditor = newAuthEditor(m.activeRequest)
	m.activeField = authField
	return m.clearStatus()
}

func (m Model) beginHeadersEdit() Model {
	if m.activeRequest == nil {
		return m.status("error", "Select a request first")
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
	case bodyField:
		m.activeField = noneField
		m.bodyTextarea.Blur()
		m.bodyTextarea.SetValue(m.preEditBody)
	case authField:
		m.activeField = noneField
		m.authEditor = authEditor{}
	case headersField:
		m.activeField = noneField
		m.headerPairs = nil
		m.headerEditing = false
		m.headerKeyInput.Blur()
		m.headerValueInput.Blur()
	}
	return m
}

func (m Model) finishBodyEdit() Model {
	m.activeRequest.Body = m.bodyTextarea.Value()
	m.bodyTextarea.Blur()
	m.activeField = noneField
	return m.status("success", "Body updated")
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
