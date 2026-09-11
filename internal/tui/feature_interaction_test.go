package tui

import (
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/domain"
)

func TestRequestURLTabAcceptsLoadedURLSuggestion(t *testing.T) {
	m := New(Deps{Config: config.Default(t.TempDir())})
	m.collectionRequests = map[string][]*domain.Request{
		"collection": {{URL: "https://api.example.com/users"}},
	}
	m.activeField = urlField
	m.urlInput.SetValue("https://api.example.com/u")

	updated, _ := m.handleRequestKey("", tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)

	require.Equal(t, "https://api.example.com/users", m.urlInput.Value())
}

func TestHeaderEditTabCompletesNameAndValue(t *testing.T) {
	m := New(Deps{Config: config.Default(t.TempDir())})
	m.headerKeyInput = textinput.New()
	m.headerValueInput = textinput.New()
	m.headerKeyInput.SetValue("auth")
	m.headerKeyInput.Focus()
	m.headerValueInput.Blur()

	updated, _ := m.handleHeaderFieldEdit(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	require.Equal(t, "Authorization", m.headerKeyInput.Value())
	require.True(t, m.headerKeyInput.Focused())

	m.headerValueInput.SetValue("app")
	m.headerKeyInput.SetValue("Content-Type")
	m.headerKeyInput.Blur()
	m.headerValueInput.Focus()
	updated, _ = m.handleHeaderFieldEdit(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	require.Equal(t, "application/json", m.headerValueInput.Value())
}

func TestSaveBodyFormatsJSONBeforePersisting(t *testing.T) {
	m := New(Deps{Config: config.Default(t.TempDir())})
	m.activeRequest = &domain.Request{
		ID:      "request-1",
		Headers: `{"Content-Type":"application/json"}`,
	}
	m.bodyTextarea = textarea.New()
	m.bodyTextarea.SetValue(`{"name":"quark","items":[1,2]}`)
	m.activeField = bodyField

	updated, cmd := m.saveBody()
	m = updated
	require.NotNil(t, cmd)
	require.Equal(t, "{\n  \"name\": \"quark\",\n  \"items\": [\n    1,\n    2\n  ]\n}", m.bodyTextarea.Value())
}

func TestSaveBodyRejectsInvalidJSONWithoutSaving(t *testing.T) {
	m := New(Deps{Config: config.Default(t.TempDir())})
	m.activeRequest = &domain.Request{
		ID:      "request-1",
		Headers: `{"Content-Type":"application/json"}`,
	}
	m.bodyTextarea = textarea.New()
	m.bodyTextarea.SetValue(`{"invalid":`)
	m.activeField = bodyField

	updated, cmd := m.saveBody()
	m = updated
	require.Nil(t, cmd)
	require.Contains(t, m.statusErr, "invalid JSON body")
	require.Equal(t, `{"invalid":`, m.bodyTextarea.Value())
}

func TestSaveBodyPreservesFileReference(t *testing.T) {
	m := New(Deps{Config: config.Default(t.TempDir())})
	m.activeRequest = &domain.Request{
		ID:      "request-1",
		Headers: `{"Content-Type":"application/json"}`,
	}
	m.bodyTextarea = textarea.New()
	m.bodyTextarea.SetValue("@payload.json")
	m.activeField = bodyField

	updated, cmd := m.saveBody()
	m = updated
	require.NotNil(t, cmd)
	require.Equal(t, "@payload.json", m.bodyTextarea.Value())
}
