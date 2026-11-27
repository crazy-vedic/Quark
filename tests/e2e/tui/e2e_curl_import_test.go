//go:build e2e

package tui_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/tui"
)

func TestE2E_CurlImportPersistsThroughModelAndSQLite(t *testing.T) {
	collection := &domain.Collection{ID: "curl-col", Name: "Imported APIs"}
	st := setupStore(t, collection)
	command := `curl -X POST 'https://access.dev.wealthcareadmin.com/access/connect/token' --header 'Content-Type: application/x-www-form-urlencoded' --header 'Authorization: Bearer secret-token' --header 'X-Trace: first' --header 'X-Trace: second' --data-urlencode 'scope=mbi_api offline_access openid'`

	m := newE2EModel(t, st, &mockExecutor{})
	m = resize(t, m, 130, 42)
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{collection}))
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(command), Paste: true})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlS})
	assertViewContains(t, m, "POST")
	assertViewContains(t, m, "[REDACTED]")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("IAM token")})
	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		m = callUpdate(t, m, runCmd(t, cmd))
	}

	requests, err := st.ListRequests(context.Background(), collection.ID)
	require.NoError(t, err)
	require.Len(t, requests, 1)
	require.Equal(t, "IAM token", requests[0].Name)
	require.Equal(t, http.MethodPost, requests[0].Method)
	var headers http.Header
	require.NoError(t, json.Unmarshal([]byte(requests[0].Headers), &headers))
	require.Equal(t, []string{"first", "second"}, headers.Values("X-Trace"))

	// Import mode deliberately owns the input path; mouse input must not mutate it.
	before := m.View()
	m = callUpdate(
		t,
		m,
		tea.MouseMsg{X: 10, Y: 10, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft},
	)
	require.Equal(t, before, m.View())
}

// Ctrl+V in the import dialog reads the clipboard asynchronously and inserts
// the text at the cursor, both into an empty field and after existing text.
func TestE2E_CurlImportCtrlVPastesIntoInputField(t *testing.T) {
	const command = "curl -X GET https://example.com/pasted"
	restore := tui.StubClipboardForTest(func() (string, error) { return command, nil })
	t.Cleanup(restore)

	collection := &domain.Collection{ID: "paste-col", Name: "Paste APIs"}
	st := setupStore(t, collection)
	m := newE2EModel(t, st, &mockExecutor{})
	m = resize(t, m, 130, 42)
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{collection}))
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})

	paste := func(m tui.Model) tui.Model {
		m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlV})
		require.NotNil(t, cmd, "Ctrl+V must start a clipboard read")
		return callUpdate(t, m, runCmd(t, cmd))
	}

	m = paste(m)
	require.Equal(t, command, m.ImportInputValue(), "paste into an empty import field")

	m = paste(m)
	require.Equal(
		t,
		command+command,
		m.ImportInputValue(),
		"paste into a non-empty import field inserts at the cursor",
	)
}
