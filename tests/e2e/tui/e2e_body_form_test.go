//go:build e2e

package tui_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/store"
	"github.com/crazy-vedic/quark/internal/tui"
)

// openFormBodyRequest seeds one request with the given body type and body and
// focuses it in the request pane.
func openFormBodyRequest(
	t *testing.T,
	bodyType, body string,
) (tui.Model, *domain.Request, *store.Store) {
	t.Helper()
	col := &domain.Collection{ID: uuid.NewString(), Name: "Forms"}
	st := setupStore(t, col)
	req := &domain.Request{
		ID:       uuid.NewString(),
		Name:     "Login",
		Method:   "POST",
		URL:      "https://example.com/login",
		Headers:  `{}`,
		BodyType: bodyType,
		Body:     body,
	}
	seedRequests(t, st, col.ID, req)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))
	m = m.WithActiveRequest(req).WithFocus(tui.RequestPane)
	return m, req, st
}

func typeBodyFormText(t *testing.T, m tui.Model, text string) tui.Model {
	t.Helper()
	for _, r := range text {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func storedBody(t *testing.T, st *store.Store, id string) *domain.Request {
	t.Helper()
	req, err := st.GetRequest(context.Background(), id)
	require.NoError(t, err)
	return req
}

func TestE2E_BodyType_CycleSavesEachType(t *testing.T) {
	m, req, st := openFormBodyRequest(t, domain.BodyTypeNone, "")
	assertViewContains(t, m, "No body is sent (body type is none)")

	for _, want := range []string{
		domain.BodyTypeRaw,
		domain.BodyTypeJSON,
		domain.BodyTypeFormURLEncoded,
		domain.BodyTypeNone,
	} {
		var cmd tea.Cmd
		m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
		require.NotNil(t, cmd)
		m = executeCmdUpdate(t, m, cmd)
		assert.Equal(t, "Body type: "+want, m.StatusSuccess())
		assert.Equal(t, want, storedBody(t, st, req.ID).BodyType)
	}
}

func TestE2E_BodyForm_OpensDecodedFieldsAndSavesNewRow(t *testing.T) {
	m, req, st := openFormBodyRequest(t, domain.BodyTypeFormURLEncoded, "a=1&b=two+words")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	require.Equal(t, tui.BodyFormField, m.ActiveField())
	require.Len(t, m.HeaderPairs(), 2)
	assert.Equal(t, "two words", m.HeaderPairs()[1].Value, "fields are decoded when the editor opens")
	assertViewContains(t, m, "a = 1")
	assertViewContains(t, m, "b = two words")

	// Add a field: key, Tab, value, Enter confirms the row, Enter saves.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	require.True(t, m.HeaderEditing())
	m = typeBodyFormText(t, m, "c")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = typeBodyFormText(t, m, "x y")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.Len(t, m.HeaderPairs(), 3)

	var cmd tea.Cmd
	m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	m = executeCmdUpdate(t, m, cmd)
	assert.Equal(t, tui.NoneField, m.ActiveField())
	assert.Equal(t, "Form body updated", m.StatusSuccess())
	assert.Equal(t, "a=1&b=two+words&c=x+y", storedBody(t, st, req.ID).Body)
}

func TestE2E_BodyForm_EditAndDeleteFieldsSaveEncodedBody(t *testing.T) {
	m, req, st := openFormBodyRequest(t, domain.BodyTypeFormURLEncoded, "a=1&b=two+words")

	// Edit the first field's value: 'e', Tab, replace, Enter confirms the row.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	require.True(t, m.HeaderEditing())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	m = typeBodyFormText(t, m, "9")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.False(t, m.HeaderEditing())
	assert.Equal(t, "9", m.HeaderPairs()[0].Value)

	// Delete the second field, then save.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	require.Len(t, m.HeaderPairs(), 1)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	require.Empty(t, m.HeaderPairs())
	assertViewContains(t, m, "No form fields.")

	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	m = executeCmdUpdate(t, m, cmd)
	assert.Equal(t, "", storedBody(t, st, req.ID).Body)
}

func TestE2E_BodyForm_EscCancelKeepsStoredBody(t *testing.T) {
	m, req, st := openFormBodyRequest(t, domain.BodyTypeFormURLEncoded, "a=1")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	require.Empty(t, m.HeaderPairs())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.NoneField, m.ActiveField())
	assert.Equal(t, "a=1", storedBody(t, st, req.ID).Body)
}

func TestE2E_BodyForm_EmptyBodyShowsAddHint(t *testing.T) {
	m, _, _ := openFormBodyRequest(t, domain.BodyTypeFormURLEncoded, "")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	assert.Equal(t, tui.BodyFormField, m.ActiveField())
	assertViewContains(t, m, "No form fields. Press")
}

func TestE2E_BodyForm_SendUsesFormContentType(t *testing.T) {
	var receivedBody, receivedType string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)
		receivedType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local socket unavailable in this environment: %v", err)
	}
	srv := &httptest.Server{Listener: listener, Config: &http.Server{Handler: handler}}
	srv.Start()
	t.Cleanup(srv.Close)
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)

	col := &domain.Collection{ID: uuid.NewString(), Name: "Send"}
	st := setupStore(t, col)
	req := &domain.Request{
		ID:       uuid.NewString(),
		Name:     "Form post",
		Method:   "POST",
		URL:      srv.URL,
		Headers:  `{}`,
		BodyType: domain.BodyTypeFormURLEncoded,
		Body:     "name=quark+ui&q=a%26b",
	}
	seedRequests(t, st, col.ID, req)

	m := newE2EModel(t, st, exec.New(transport))
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))
	m = m.WithActiveRequest(req).WithFocus(tui.RequestPane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	m = executeCmdUpdate(t, m, cmd)

	require.NotNil(t, m.Response())
	assert.Equal(t, 200, m.Response().StatusCode)
	assert.Equal(t, "name=quark+ui&q=a%26b", receivedBody)
	assert.Equal(t, "application/x-www-form-urlencoded", receivedType)
}

func TestE2E_BodyForm_CurlExportCarriesFormBody(t *testing.T) {
	col := &domain.Collection{ID: uuid.NewString(), Name: "Export"}
	st := setupStore(t, col)
	req := &domain.Request{
		ID:       uuid.NewString(),
		Name:     "Form export",
		Method:   "POST",
		URL:      "https://example.test/form",
		Headers:  `{}`,
		BodyType: domain.BodyTypeFormURLEncoded,
		Body:     "a=1&b=two+words",
	}
	seedRequests(t, st, col.ID, req)

	persisted := storedBody(t, st, req.ID)
	command, err := curl.ExportRequest(persisted)
	require.NoError(t, err)
	assert.Equal(t,
		"curl -X 'POST' -H 'Content-Type: application/x-www-form-urlencoded' "+
			"--data-raw 'a=1&b=two+words' 'https://example.test/form'",
		command,
	)
}
