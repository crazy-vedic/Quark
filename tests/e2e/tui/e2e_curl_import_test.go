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
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'I'}})
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
	m = callUpdate(t, m, tea.MouseMsg{X: 10, Y: 10, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	require.Equal(t, before, m.View())
}
