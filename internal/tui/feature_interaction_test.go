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
	m = requireModel(t, updated)

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
	m = requireModel(t, updated)
	require.Equal(t, "Authorization", m.headerKeyInput.Value())
	require.True(t, m.headerKeyInput.Focused())

	m.headerValueInput.SetValue("app")
	m.headerKeyInput.SetValue("Content-Type")
	m.headerKeyInput.Blur()
	m.headerValueInput.Focus()
	updated, _ = m.handleHeaderFieldEdit(tea.KeyMsg{Type: tea.KeyTab})
	m = requireModel(t, updated)
	require.Equal(t, "application/json", m.headerValueInput.Value())
}

// Opening the header editor on headers it cannot parse would show an empty
// list, and saving that list would overwrite the stored headers with {}.
func TestBeginHeadersEdit_RefusesUnparseableHeaders(t *testing.T) {
	const stored = `{"Authorization": "Bearer abc"` // truncated JSON
	m := New(Deps{Config: config.Default(t.TempDir())})
	m.activeRequest = &domain.Request{ID: "request-1", Headers: stored}

	m = m.beginHeadersEdit()

	require.NotEqual(t, headersField, m.activeField, "editor must not open")
	require.Contains(t, m.statusErr, "headers")
	require.Equal(t, stored, m.activeRequest.Headers, "stored headers must be untouched")
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
	require.Equal(
		t,
		"{\n  \"name\": \"quark\",\n  \"items\": [\n    1,\n    2\n  ]\n}",
		m.bodyTextarea.Value(),
	)
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

// E2E tests for JSON formatting on body save (Issue #49)

func TestFinishBodyEdit_FormatsJSONWhenEnabled(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.Format.JSONOnSave = true

	m := New(Deps{Config: cfg})
	m.activeRequest = &domain.Request{ID: "req-1"}
	m.bodyTextarea = textarea.New()
	m.bodyTextarea.SetValue(`{"name":"alice","age":30}`)
	m.activeField = bodyField

	updated := m.finishBodyEdit()
	m = updated

	// Should be formatted with newlines
	require.Contains(t, m.activeRequest.Body, "\n")
	require.Contains(t, m.statusSuccess, "JSON formatted")
}

func TestFinishBodyEdit_LeavesMalformedJSONUnchanged(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.Format.JSONOnSave = true

	m := New(Deps{Config: cfg})
	m.activeRequest = &domain.Request{ID: "req-1"}
	m.bodyTextarea = textarea.New()
	malformed := `{"invalid":,}`
	m.bodyTextarea.SetValue(malformed)
	m.activeField = bodyField

	updated := m.finishBodyEdit()
	m = updated

	require.Equal(t, malformed, m.activeRequest.Body)
	require.Equal(t, "Body updated", m.statusSuccess)
}

func TestFinishBodyEdit_DisablesFormattingWhenConfigFalse(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.Format.JSONOnSave = false

	m := New(Deps{Config: cfg})
	m.activeRequest = &domain.Request{ID: "req-1"}
	m.bodyTextarea = textarea.New()
	minified := `{"name":"alice","age":30}`
	m.bodyTextarea.SetValue(minified)
	m.activeField = bodyField

	updated := m.finishBodyEdit()
	m = updated

	require.Equal(t, minified, m.activeRequest.Body)
	require.Equal(t, "Body updated", m.statusSuccess)
	require.NotContains(t, m.statusSuccess, "formatted")
}

func TestFinishBodyEdit_PresservesIndentStyle(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.Format.JSONOnSave = true

	m := New(Deps{Config: cfg})
	m.activeRequest = &domain.Request{ID: "req-1"}
	m.bodyTextarea = textarea.New()
	// Input with 4-space indent
	m.bodyTextarea.SetValue("{\n    \"a\": 1\n}")
	m.activeField = bodyField

	updated := m.finishBodyEdit()
	m = updated

	// Formatted with 4-space indent
	require.Contains(t, m.activeRequest.Body, "    \"a\"",
		"4-space indent style should be preserved")
}

func TestFinishBodyEdit_HandlesLargeNestedJSON(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.Format.JSONOnSave = true

	m := New(Deps{Config: cfg})
	m.activeRequest = &domain.Request{ID: "req-1"}
	m.bodyTextarea = textarea.New()
	// Deeply nested structure
	deepJSON := `{"level1":{"level2":{"level3":{"level4":{"level5":{"value":42}}}}}}`
	m.bodyTextarea.SetValue(deepJSON)
	m.activeField = bodyField

	updated := m.finishBodyEdit()
	m = updated

	// Should format and maintain structure
	require.Contains(t, m.activeRequest.Body, "\n")
	require.Contains(t, m.activeRequest.Body, "\"value\"")
	require.Contains(t, m.statusSuccess, "JSON formatted")
}

func TestFinishBodyEdit_PreservesNonJSONBody(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.Format.JSONOnSave = true

	m := New(Deps{Config: cfg})
	m.activeRequest = &domain.Request{ID: "req-1"}
	m.bodyTextarea = textarea.New()
	plainText := "This is plain text\nNot JSON"
	m.bodyTextarea.SetValue(plainText)
	m.activeField = bodyField

	updated := m.finishBodyEdit()
	m = updated

	require.Equal(t, plainText, m.activeRequest.Body)
	require.Equal(t, "Body updated", m.statusSuccess)
}

func TestFinishBodyEdit_PreservesNumericPrecision(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.Format.JSONOnSave = true

	m := New(Deps{Config: cfg})
	m.activeRequest = &domain.Request{ID: "req-1"}
	m.bodyTextarea = textarea.New()
	// Large number that could lose precision
	preciseJSON := `{"big":12345678901234567890,"float":3.14159265359}`
	m.bodyTextarea.SetValue(preciseJSON)
	m.activeField = bodyField

	updated := m.finishBodyEdit()
	m = updated

	require.Contains(t, m.activeRequest.Body, "12345678901234567890")
	require.Contains(t, m.activeRequest.Body, "3.14159265359")
}

func TestFinishBodyEdit_EmptyBodyRemains(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.Format.JSONOnSave = true

	m := New(Deps{Config: cfg})
	m.activeRequest = &domain.Request{ID: "req-1"}
	m.bodyTextarea = textarea.New()
	m.bodyTextarea.SetValue("")
	m.activeField = bodyField

	updated := m.finishBodyEdit()
	m = updated

	require.Equal(t, "", m.activeRequest.Body)
	require.Equal(t, "Body updated", m.statusSuccess)
}
