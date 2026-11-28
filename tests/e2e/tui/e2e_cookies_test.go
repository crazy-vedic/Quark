//go:build e2e

package tui_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/tui"
)

// storedHeaders decodes a request's saved headers.
func storedHeaders(t *testing.T, raw string) http.Header {
	t.Helper()
	var h http.Header
	require.NoError(t, json.Unmarshal([]byte(raw), &h))
	return h
}

// runePress types each rune of s as a key press.
func runePress(t *testing.T, m tui.Model, s string) tui.Model {
	t.Helper()
	for _, r := range s {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

// sendRequest sends req from the request pane with Alt+Enter and returns the
// model after the response arrives.
func sendRequest(t *testing.T, m tui.Model, req *domain.Request) tui.Model {
	t.Helper()
	m = m.WithFocus(tui.RequestPane).
		WithActiveRequest(req).
		WithMethod(req.Method).
		WithURLValue(req.URL)
	model, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	return executeCmdUpdate(t, model, cmd)
}

// The cookie editor lists a request's Cookie header as name=value rows. Saving
// rewrites only the Cookie header and keeps every other header.
func TestE2E_CookieEditor_EditsCookieRowsAndKeepsOtherHeaders(t *testing.T) {
	col := &domain.Collection{ID: "cookie-col", Name: "Cookies"}
	st := setupStore(t, col)
	req := &domain.Request{
		ID: uuid.NewString(), CollectionID: col.ID, Name: "cookied", Method: http.MethodGet,
		URL:     "https://example.test/private",
		Headers: `{"Cookie":["sid=old; theme=dark"],"Accept":["application/json"]}`,
	}
	require.NoError(t, st.SaveRequest(context.Background(), req))

	m := newE2EModel(t, st, &mockExecutor{})
	m = resize(t, m, 130, 42)
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = m.WithActiveRequest(req).WithFocus(tui.RequestPane)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	require.True(t, m.CookieEditing(), "c opens the cookie editor")
	assert.Equal(t, tui.HeadersField, m.ActiveField())
	require.Len(t, m.HeaderPairs(), 2)
	assertViewContains(t, m, "sid=old")
	assertViewContains(t, m, "theme=dark")
	assertViewNotContains(t, m, "Accept")

	// Edit the first cookie's value, then save.
	m = runePress(t, m, "e")
	require.True(t, m.HeaderEditing())
	assertViewContains(t, m, "Name:")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = runePress(t, m, "x")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, tui.NoneField, m.ActiveField())
	assert.False(t, m.CookieEditing())

	require.NotNil(t, m.ActiveRequest())
	headers := storedHeaders(t, m.ActiveRequest().Headers)
	assert.Equal(t, []string{"sid=oldx; theme=dark"}, headers["Cookie"])
	assert.Equal(t, []string{"application/json"}, headers["Accept"])
	assertViewContains(t, m, "Cookies updated")
}

// Cookies can be added and deleted. An empty list removes the Cookie header.
func TestE2E_CookieEditor_AddAndDeleteCookies(t *testing.T) {
	col := &domain.Collection{ID: "cookie-add", Name: "Cookie add"}
	st := setupStore(t, col)
	req := &domain.Request{
		ID: uuid.NewString(), CollectionID: col.ID, Name: "add", Method: http.MethodGet,
		URL:     "https://example.test/add",
		Headers: `{"Cookie":["only=1"],"Accept":["text/plain"]}`,
	}
	require.NoError(t, st.SaveRequest(context.Background(), req))

	m := newE2EModel(t, st, &mockExecutor{})
	m = resize(t, m, 130, 42)
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = m.WithActiveRequest(req).WithFocus(tui.RequestPane)

	m = runePress(t, m, "c")
	m = runePress(t, m, "d") // delete the only cookie
	assertViewContains(t, m, "No cookies")
	m = runePress(t, m, "a") // add a cookie: name, then value
	m = runePress(t, m, "fresh")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = runePress(t, m, "2")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	headers := storedHeaders(t, m.ActiveRequest().Headers)
	assert.Equal(t, []string{"fresh=2"}, headers["Cookie"])
	assert.Equal(t, []string{"text/plain"}, headers["Accept"])

	// Deleting every cookie and saving removes the Cookie header itself.
	m = runePress(t, m, "c")
	m = runePress(t, m, "d")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	headers = storedHeaders(t, m.ActiveRequest().Headers)
	assert.NotContains(t, headers, "Cookie")
	assert.Equal(t, []string{"text/plain"}, headers["Accept"])
}

// Headers that are not valid JSON are never overwritten by the cookie editor.
func TestE2E_CookieEditor_RefusesInvalidStoredHeaders(t *testing.T) {
	col := &domain.Collection{ID: "cookie-bad", Name: "Cookie bad"}
	st := setupStore(t, col)
	req := &domain.Request{
		ID: uuid.NewString(), CollectionID: col.ID, Name: "broken", Method: http.MethodGet,
		URL:     "https://example.test/broken",
		Headers: `{"Cookie": `,
	}
	require.NoError(t, st.SaveRequest(context.Background(), req))

	m := newE2EModel(t, st, &mockExecutor{})
	m = resize(t, m, 130, 42)
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = m.WithActiveRequest(req).WithFocus(tui.RequestPane)

	m = runePress(t, m, "c")
	assert.False(t, m.CookieEditing())
	assertViewContains(t, m, "refusing to edit and overwrite them")
	assert.Equal(t, `{"Cookie": `, m.ActiveRequest().Headers)
}

// A session cookie jar keeps cookies that responses set, sends them on later
// requests, and forgets them when cleared.
func TestE2E_SessionJar_CookiesFlowUntilCleared(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			//nolint:gosec // G124: test cookie over plain HTTP; Secure would stop it being sent.
			http.SetCookie(w, &http.Cookie{
				Name:     "sid",
				Value:    "abc",
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
			w.WriteHeader(http.StatusOK)
		case "/me":
			if c, err := r.Cookie("sid"); err == nil && c.Value == "abc" {
				_, _ = io.WriteString(w, "hello sid")
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	col := &domain.Collection{ID: "jar-col", Name: "Jar"}
	st := setupStore(t, col)
	login := &domain.Request{
		ID: uuid.NewString(), CollectionID: col.ID, Name: "login", Method: http.MethodGet,
		URL: srv.URL + "/login",
	}
	me := &domain.Request{
		ID: uuid.NewString(), CollectionID: col.ID, Name: "me", Method: http.MethodGet,
		URL: srv.URL + "/me",
	}
	require.NoError(t, st.SaveRequest(context.Background(), login))
	require.NoError(t, st.SaveRequest(context.Background(), me))

	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	executor := exec.New(transport, exec.WithCookieJar(exec.NewSessionJar()))
	m := newE2EModel(t, st, executor)
	m = resize(t, m, 130, 42)
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))

	m = sendRequest(t, m, login)
	require.NotNil(t, m.Response())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight}) // headers tab
	assertViewContains(t, m, "Cookies set by response (1)")
	assertViewContains(t, m, "sid=abc")

	m = sendRequest(t, m, me)
	require.NotNil(t, m.Response())
	assert.Equal(t, http.StatusOK, m.Response().StatusCode, "the jar sends the login cookie")
	assert.Equal(t, "hello sid", string(m.Response().Body))

	m = m.WithFocus(tui.RequestPane)
	m = runePress(t, m, "X")
	assertViewContains(t, m, "Session cookies cleared")

	m = sendRequest(t, m, me)
	require.NotNil(t, m.Response())
	assert.Equal(t, http.StatusUnauthorized, m.Response().StatusCode, "cleared cookies are not sent")
}

// Without a session jar, clearing cookies says how to turn one on.
func TestE2E_SessionJar_ClearWithoutJarExplainsConfig(t *testing.T) {
	st := setupStore(t)
	m := newE2EModel(t, st, &mockExecutor{})
	m = resize(t, m, 130, 42)
	m = m.WithFocus(tui.RequestPane)
	m = runePress(t, m, "X")
	assertViewContains(t, m, "No session cookie jar: set http.cookie_jar = true in config.toml")
}

// A response's Set-Cookie headers are listed with their attributes on the
// headers tab, whether or not a jar is in use.
func TestE2E_SetCookieResponseShowsAttributes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Set-Cookie", "a=1; Path=/app; Max-Age=60; Secure; HttpOnly; SameSite=Strict")
		w.Header().Add("Set-Cookie", "b=two; Domain=example.test; Expires=Wed, 21 Oct 2037 07:28:00 GMT; SameSite=None")
		w.Header().Add("Set-Cookie", "c=3; SameSite=Lax")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	col := &domain.Collection{ID: "set-cookie", Name: "Set cookie"}
	st := setupStore(t, col)
	req := &domain.Request{
		ID: uuid.NewString(), CollectionID: col.ID, Name: "set", Method: http.MethodGet,
		URL: srv.URL + "/set",
	}
	require.NoError(t, st.SaveRequest(context.Background(), req))

	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	m := newE2EModel(t, st, exec.New(transport))
	m = resize(t, m, 160, 50)
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))

	m = sendRequest(t, m, req)
	require.NotNil(t, m.Response())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight}) // headers tab
	assertViewContains(t, m, "Cookies set by response (3)")
	assertViewContains(t, m, "a=1  [path=/app, max-age=60, secure, httponly, samesite=strict]")
	assertViewContains(t, m, "b=two  [domain=example.test, expires=2037-10-21 07:28:00Z, samesite=none]")
	assertViewContains(t, m, "c=3  [samesite=lax]")
}
