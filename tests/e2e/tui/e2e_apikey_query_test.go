//go:build e2e

package tui_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/tui"
)

// Sending from the TUI with API-key auth "in query" appends one parameter and
// leaves the query the user wrote as it was; a parameter of the same name is
// replaced.
func TestE2E_TUISendAPIKeyInQueryKeepsTheUsersQuery(t *testing.T) {
	var mu sync.Mutex
	var rawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		rawQuery = r.URL.RawQuery
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	tests := []struct{ name, path, want string }{
		{"appended after the existing query", "/p?zeta=1&alpha=a%20b", "zeta=1&alpha=a%20b&api_key=s3cret%26x"},
		{"replaces a parameter of the same name", "/p?api_key=old&b=2", "api_key=s3cret%26x&b=2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			col := &domain.Collection{ID: uuid.NewString(), Name: "Keys"}
			st := setupStore(t, col)
			req := &domain.Request{
				ID:         uuid.NewString(),
				Name:       "Keyed",
				Method:     "GET",
				URL:        srv.URL + tc.path,
				AuthType:   domain.AuthTypeAPIKey,
				AuthConfig: `{"in":"query","name":"api_key","value":"s3cret&x"}`,
			}
			seedRequests(t, st, col.ID, req)

			transport := &http.Transport{}
			t.Cleanup(transport.CloseIdleConnections)
			m := newE2EModel(t, st, exec.New(transport))
			m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
			m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
			m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))
			m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
			m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
			m = executeCmdUpdate(t, m, cmd)

			require.NotNil(t, m.Response())
			assert.Equal(t, 200, m.Response().StatusCode)
			mu.Lock()
			defer mu.Unlock()
			assert.Equal(t, tc.want, rawQuery)
		})
	}
}
