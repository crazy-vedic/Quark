package tui_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/tui"
)

const testCurl = `curl -X POST https://api.example.com/items -H 'Content-Type: application/json' -d '{"a":1}'`

// newModelWithCurl is newModel with the real curl importer wired in.
func newModelWithCurl() tui.Model {
	return tui.New(tui.Deps{Config: defaultConfig(), Importer: curl.NewImporter()})
}

func existingRequest() *domain.Request {
	return &domain.Request{
		ID: "r1", Name: "keep me", Method: "GET", URL: "https://old.example", Body: "old body",
	}
}

func TestCurlEnterOpensReplaceDialogAndConfirmReplaces(t *testing.T) {
	req := existingRequest()
	m := newModelWithCurl().
		WithFocus(tui.RequestPane).
		WithActiveRequest(req).
		WithURLEdit().
		WithURLValue(testCurl)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, tui.ImportMode, m.Mode(), "Enter opens the replace dialog")
	assert.Equal(t, "https://old.example", req.URL, "nothing changes until confirmed")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, tui.NormalMode, m.Mode())
	assert.Equal(t, "POST", req.Method)
	assert.Equal(t, "https://api.example.com/items", req.URL)
	assert.Equal(t, `{"a":1}`, req.Body)
	assert.Contains(t, req.Headers, "Content-Type")
	assert.Equal(t, "keep me", req.Name, "name is not overwritten")
}

func TestCurlCancelLeavesRequestUnchanged(t *testing.T) {
	req := existingRequest()
	// Editing starts with the request's URL in the field, as it does in the app.
	m := newModelWithCurl().
		WithFocus(tui.RequestPane).
		WithActiveRequest(req).
		WithURLValue(req.URL).
		WithURLEdit().
		WithURLValue(testCurl)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.NormalMode, m.Mode())
	assert.Equal(t, "https://old.example", req.URL)
	assert.Equal(t, "GET", req.Method)
	assert.Equal(t, "https://old.example", m.URLValue(), "URL field is restored")
}

func TestCurlPastedIntoURLOpensReplaceDialog(t *testing.T) {
	req := existingRequest()
	m := newModelWithCurl().
		WithFocus(tui.RequestPane).
		WithActiveRequest(req).
		WithURLEdit().
		WithURLValue("")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(testCurl), Paste: true})
	assert.Equal(t, tui.ImportMode, m.Mode(), "pasting a curl command opens the dialog")
	assert.Equal(t, "https://old.example", req.URL, "paste alone does not replace the request")
}

func TestCurlWithoutRequestShowsMessage(t *testing.T) {
	m := newModelWithCurl().
		WithFocus(tui.RequestPane).
		WithURLEdit().
		WithURLValue("curl https://example.com")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Contains(t, m.StatusErr(), "Select a request first")
	assert.Equal(t, tui.NormalMode, m.Mode())
}

func TestCurlParseErrorKeepsTextForEditing(t *testing.T) {
	req := existingRequest()
	m := newModelWithCurl().
		WithFocus(tui.RequestPane).
		WithActiveRequest(req).
		WithURLEdit().
		WithURLValue("curl -d 'unterminated")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, tui.URLField, m.ActiveField(), "stay in the field to fix the command")
	assert.Equal(t, "https://old.example", req.URL)
	assert.Contains(t, m.StatusErr(), "Could not parse curl")
}

func TestCurlReplaceRejectsClientCertificate(t *testing.T) {
	req := existingRequest()
	m := newModelWithCurl().
		WithFocus(tui.RequestPane).
		WithActiveRequest(req).
		WithURLEdit().
		WithURLValue("curl --cert client.pem https://secure.example")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, tui.ImportMode, m.Mode())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, tui.ImportMode, m.Mode(), "dialog stays open")
	assert.Equal(t, "https://old.example", req.URL, "certificate case does not replace")
}
