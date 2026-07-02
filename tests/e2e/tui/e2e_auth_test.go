//go:build e2e

package tui_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/store"
	"github.com/crazy-vedic/quark/internal/tui"
)

func authTestStore(t *testing.T) (*store.Store, *domain.Collection) {
	st, err := store.New(filepath.Join(t.TempDir(), "auth-e2e.db"), store.WithCacheSize(100))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	col := &domain.Collection{ID: uuid.NewString(), Name: "API"}
	require.NoError(t, st.SaveCollection(context.Background(), col))
	return st, col
}

func TestE2E_AuthEditor_BearerUsesEnvAndRedactsPreview(t *testing.T) {
	st, col := authTestStore(t)
	ctx := context.Background()

	defaultEnv, err := st.CreateDefaultEnvironment(ctx, col.ID)
	require.NoError(t, err)
	defaultEnv.SetVars(map[string]string{"token": "env-token"})
	require.NoError(t, st.SaveEnvironment(ctx, defaultEnv))
	require.NoError(t, st.SetActiveEnvironment(ctx, col.ID, defaultEnv.ID))

	var receivedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	req := &domain.Request{
		ID:           uuid.NewString(),
		CollectionID: col.ID,
		Name:         "get-users",
		Method:       "GET",
		URL:          srv.URL,
	}
	require.NoError(t, st.SaveRequest(ctx, req))

	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	executor := exec.New(transport)

	m := newE2EModel(t, st, executor).
		WithFocus(tui.RequestPane).
		WithActiveRequest(req).
		WithMethod(req.Method).
		WithURLValue(req.URL)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))

	model, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = model
	require.Equal(t, tui.AuthField, m.ActiveField())
	assertViewContains(t, m, "Secrets stay hidden in the preview")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}}) // None -> Bearer
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	for _, r := range "{{token}}" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	model, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = executeCmdUpdate(t, model, cmd)

	require.Equal(t, tui.NoneField, m.ActiveField())
	require.NotNil(t, m.ActiveRequest())
	assert.Equal(t, domain.AuthTypeBearer, m.ActiveRequest().AuthType)
	assert.Equal(t, `{"token":"{{token}}"}`, m.ActiveRequest().AuthConfig)
	assertViewContains(t, m, "Auth: Bearer")
	assertViewNotContains(t, m, "env-token")

	saved, err := st.GetRequest(ctx, req.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.AuthTypeBearer, saved.AuthType)

	model, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	m = executeCmdUpdate(t, model, cmd)
	require.NotNil(t, m.Response())
	assert.Equal(t, "Bearer env-token", receivedAuth)
}

func TestE2E_AuthEditor_BasicAuthSendsEncodedHeader(t *testing.T) {
	st, col := authTestStore(t)
	ctx := context.Background()

	var receivedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	req := &domain.Request{
		ID:           uuid.NewString(),
		CollectionID: col.ID,
		Name:         "basic",
		Method:       "GET",
		URL:          srv.URL,
	}
	require.NoError(t, st.SaveRequest(ctx, req))

	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	executor := exec.New(transport)

	m := newE2EModel(t, st, executor).
		WithFocus(tui.RequestPane).
		WithActiveRequest(req).
		WithMethod(req.Method).
		WithURLValue(req.URL)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight}) // None -> Bearer
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight}) // Bearer -> Basic

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	for _, r := range "alice" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	for _, r := range "secret" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	model, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = executeCmdUpdate(t, model, cmd)
	model, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	_ = executeCmdUpdate(t, model, cmd)

	assert.Equal(
		t,
		"Basic "+base64.StdEncoding.EncodeToString([]byte("alice:secret")),
		receivedAuth,
	)
}

func TestE2E_AuthEditor_APIKeyQueryUsesRightSideOptionCycling(t *testing.T) {
	st, col := authTestStore(t)
	ctx := context.Background()

	var receivedToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.URL.Query().Get("merchant_id")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	req := &domain.Request{
		ID:           uuid.NewString(),
		CollectionID: col.ID,
		Name:         "apikey",
		Method:       "GET",
		URL:          srv.URL,
	}
	require.NoError(t, st.SaveRequest(ctx, req))

	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	executor := exec.New(transport)

	m := newE2EModel(t, st, executor).
		WithFocus(tui.RequestPane).
		WithActiveRequest(req).
		WithMethod(req.Method).
		WithURLValue(req.URL)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight}) // Basic -> API key

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight}) // Header -> Query

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	for _, r := range "merchant_id" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	for _, r := range "mid-123" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	assertViewContains(t, m, "[REDACTED]")
	model, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = executeCmdUpdate(t, model, cmd)
	model, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	_ = executeCmdUpdate(t, model, cmd)

	assert.Equal(t, "mid-123", receivedToken)
}
