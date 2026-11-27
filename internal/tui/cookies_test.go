package tui_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/tui"
)

// jarExecutor is a RequestExecutor whose session jar can be cleared, standing
// in for an executor built with a session cookie jar.
type jarExecutor struct {
	clears int
}

func (e *jarExecutor) Execute(context.Context, *domain.Request) (*exec.ExecuteResult, error) {
	return nil, nil
}

func (e *jarExecutor) ClearSessionCookies() bool {
	e.clears++
	return true
}

func cookieTestRequest(headers string) *domain.Request {
	return &domain.Request{
		ID:           "cookie-req",
		CollectionID: "cookie-col",
		Name:         "Cookies",
		Method:       "GET",
		URL:          "https://example.com",
		Headers:      headers,
	}
}

// storedHeaders decodes the request's stored headers JSON for assertions.
func storedHeaders(t *testing.T, raw string) map[string][]string {
	t.Helper()
	var headers map[string][]string
	require.NoError(t, json.Unmarshal([]byte(raw), &headers))
	return headers
}

func typeText(t *testing.T, m tui.Model, text string) tui.Model {
	t.Helper()
	for _, r := range text {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func TestCookies_EditorShowsCookieRowsOnly(t *testing.T) {
	req := cookieTestRequest(`{"Accept":["application/json"],"Cookie":["sid=abc; theme=dark"]}`)
	m := newModel(defaultConfig()).WithActiveRequest(req).WithFocus(tui.RequestPane)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})

	assert.True(t, m.CookieEditing())
	assert.Equal(t, tui.HeadersField, m.ActiveField())
	pairs := m.HeaderPairs()
	require.Len(t, pairs, 2)
	assert.Equal(t, "sid", pairs[0].Key)
	assert.Equal(t, "abc", pairs[0].Value)
	assert.Equal(t, "theme", pairs[1].Key)
	assert.Equal(t, "dark", pairs[1].Value)
}

func TestCookies_SaveRewritesCookieHeaderAndKeepsOthers(t *testing.T) {
	req := cookieTestRequest(`{"Accept":["application/json"],"Cookie":["sid=abc"]}`)
	m := newModel(defaultConfig()).WithActiveRequest(req).WithFocus(tui.RequestPane)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = typeText(t, m, "x")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = typeText(t, m, "1")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // finish the row
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // save the list

	assert.False(t, m.CookieEditing())
	assert.Equal(t, tui.NoneField, m.ActiveField())
	headers := storedHeaders(t, m.ActiveRequest().Headers)
	assert.Equal(t, []string{"application/json"}, headers["Accept"], "non-cookie headers are kept")
	assert.Equal(t, []string{"sid=abc; x=1"}, headers["Cookie"])
	assert.Equal(t, "Cookies updated", m.StatusSuccess())
}

func TestCookies_RemovingAllRowsRemovesCookieHeader(t *testing.T) {
	req := cookieTestRequest(`{"Accept":["application/json"],"Cookie":["sid=abc"]}`)
	m := newModel(defaultConfig()).WithActiveRequest(req).WithFocus(tui.RequestPane)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	headers := storedHeaders(t, m.ActiveRequest().Headers)
	assert.NotContains(t, headers, "Cookie")
	assert.Equal(t, []string{"application/json"}, headers["Accept"])
}

func TestCookies_CancelLeavesRequestUnchanged(t *testing.T) {
	const stored = `{"Cookie":["sid=abc"]}`
	req := cookieTestRequest(stored)
	m := newModel(defaultConfig()).WithActiveRequest(req).WithFocus(tui.RequestPane)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})

	assert.False(t, m.CookieEditing())
	assert.Equal(t, tui.NoneField, m.ActiveField())
	assert.Equal(t, stored, m.ActiveRequest().Headers)
}

func TestCookies_HeaderEditorAfterCookieEditIsNotCookieMode(t *testing.T) {
	req := cookieTestRequest(`{"Cookie":["sid=abc"]}`)
	m := newModel(defaultConfig()).WithActiveRequest(req).WithFocus(tui.RequestPane)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})

	assert.False(t, m.CookieEditing(), "the plain header editor must not inherit cookie mode")
	assert.Equal(t, tui.HeadersField, m.ActiveField())
}

func TestCookies_RefusesInvalidStoredHeaders(t *testing.T) {
	req := cookieTestRequest(`not json`)
	m := newModel(defaultConfig()).WithActiveRequest(req).WithFocus(tui.RequestPane)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})

	assert.False(t, m.CookieEditing())
	assert.Equal(t, tui.NoneField, m.ActiveField())
	assert.Equal(t, `not json`, m.ActiveRequest().Headers,
		"invalid headers must not be overwritten")
	assert.NotEmpty(t, m.StatusErr())
}

func TestCookies_ClearSessionCookiesWithJar(t *testing.T) {
	jar := &jarExecutor{}
	m := tui.New(tui.Deps{Config: defaultConfig(), Executor: jar}).WithFocus(tui.RequestPane)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})

	assert.Equal(t, 1, jar.clears)
	assert.Equal(t, "Session cookies cleared", m.StatusSuccess())
}

func TestCookies_ClearSessionCookiesWithoutJarExplainsHowToEnable(t *testing.T) {
	m := newModel(defaultConfig()).WithFocus(tui.RequestPane)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})

	assert.Contains(t, m.StatusErr(), "http.cookie_jar")
}

func TestCookies_ResponseHeadersListSetCookies(t *testing.T) {
	m := newModel(defaultConfig())
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 160, Height: 50})
	m = m.WithResponse(&exec.ExecuteResult{
		StatusCode: http.StatusOK,
		Headers: http.Header{
			"Set-Cookie": {"sid=abc; Path=/; HttpOnly", "theme=dark"},
		},
	}).WithResponseTab(tui.HeadersTab)

	view := m.View()

	assert.Contains(t, view, "Cookies set by response (2)")
	assert.Contains(t, view, "sid=abc")
	assert.Contains(t, view, "path=/, httponly")
	assert.Contains(t, view, "theme=dark")
}

func TestCookies_ResponseWithoutSetCookieShowsNoCookieSection(t *testing.T) {
	m := newModel(defaultConfig())
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 160, Height: 50})
	m = m.WithResponse(&exec.ExecuteResult{
		StatusCode: http.StatusOK,
		Headers:    http.Header{"Content-Type": {"text/plain"}},
	}).WithResponseTab(tui.HeadersTab)

	assert.NotContains(t, m.View(), "Cookies set by response")
}
