//go:build e2e

package tui_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/tui"
)

// optionKeys types each rune of s as a key press.
func optionKeys(t *testing.T, m tui.Model, s string) tui.Model {
	t.Helper()
	for _, r := range s {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

// sendOptionsRequest sends req from the request pane with Alt+Enter and returns
// the model once the response or error has arrived.
func sendOptionsRequest(t *testing.T, m tui.Model, req *domain.Request) tui.Model {
	t.Helper()
	m = m.WithFocus(tui.RequestPane).
		WithActiveRequest(req).
		WithMethod(req.Method).
		WithURLValue(req.URL)
	model, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	return executeCmdUpdate(t, model, cmd)
}

func optionsModel(t *testing.T, req *domain.Request, executor tui.RequestExecutor) tui.Model {
	t.Helper()
	col := &domain.Collection{ID: uuid.NewString(), Name: "Options"}
	st := setupStore(t, col)
	req.CollectionID = col.ID
	require.NoError(t, st.SaveRequest(context.Background(), req))
	m := newE2EModel(t, st, executor).
		WithFocus(tui.RequestPane).
		WithActiveRequest(req).
		WithMethod(req.Method).
		WithURLValue(req.URL)
	return resize(t, m, 130, 42)
}

func transportExecutor(t *testing.T) *exec.Executor {
	t.Helper()
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	return exec.New(transport)
}

// A timeout set in the options editor applies to that request, so a slow
// server is cut off, and the saved option is shown on the request pane.
func TestE2E_OptionsEditor_TimeoutAppliesToRequest(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(400 * time.Millisecond)
		_, _ = io.WriteString(w, "late")
	}))
	t.Cleanup(slow.Close)
	req := &domain.Request{Name: "slow", Method: http.MethodGet, URL: slow.URL}
	m := optionsModel(t, req, transportExecutor(t))

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	assert.Equal(t, tui.OptionsField, m.ActiveField())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = optionKeys(t, m, "50ms")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // finish the row
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // save the options
	assert.Equal(t, tui.NoneField, m.ActiveField())
	require.NotNil(t, m.ActiveRequest())
	assert.Equal(t, 50, m.ActiveRequest().Options.TimeoutMS)
	assertViewContains(t, m, "Options: timeout 50ms")

	m = sendOptionsRequest(t, m, req)
	assert.Nil(t, m.Response(), "the 50ms timeout must stop the 400ms response")
	assert.False(t, m.Loading())
	assert.Contains(t, m.StatusErr(), "timed out")
	assertViewContains(t, m, "timed out")
}

// Redirect settings from the editor apply to that request only: "do not follow"
// returns the 302, and a max of 0 does the same.
func TestE2E_OptionsEditor_RedirectSettings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/landed", http.StatusFound)
			return
		}
		_, _ = io.WriteString(w, "landed")
	}))
	t.Cleanup(srv.Close)

	tests := []struct {
		name    string
		keys    func(t *testing.T, m tui.Model) tui.Model
		wantCod int
		wantBod string
	}{
		{
			name: "do not follow redirects",
			keys: func(t *testing.T, m tui.Model) tui.Model {
				m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}) // to redirects
				m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight})                     // default -> follow
				return callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight})                  // -> do not follow
			},
			wantCod: http.StatusFound,
		},
		{
			name: "max redirects of zero",
			keys: func(t *testing.T, m tui.Model) tui.Model {
				m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
				m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
				m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
				m = optionKeys(t, m, "0")
				return callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
			},
			wantCod: http.StatusFound,
		},
		{
			name: "follow redirects explicitly",
			keys: func(t *testing.T, m tui.Model) tui.Model {
				m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
				return callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight}) // default -> follow
			},
			wantCod: http.StatusOK,
			wantBod: "landed",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := &domain.Request{Name: "start", Method: http.MethodGet, URL: srv.URL + "/start"}
			m := optionsModel(t, req, transportExecutor(t))
			m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
			m = tc.keys(t, m)
			m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // save

			m = sendOptionsRequest(t, m, req)
			require.NotNil(t, m.Response())
			assert.Equal(t, tc.wantCod, m.Response().StatusCode)
			if tc.wantBod != "" {
				assert.Equal(t, tc.wantBod, string(m.Response().Body))
			}
		})
	}
}

// A proxy set in the editor carries the request through the proxy, and the
// password in the proxy URL stays hidden on the pane.
func TestE2E_OptionsEditor_ProxyCarriesRequest(t *testing.T) {
	var proxied string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxied = r.URL.String()
		_, _ = io.WriteString(w, "via proxy")
	}))
	t.Cleanup(proxy.Close)

	req := &domain.Request{Name: "proxied", Method: http.MethodGet, URL: "http://origin.example.test/data"}
	m := optionsModel(t, req, transportExecutor(t))
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = optionKeys(t, m, "http://user:pw-secret@"+strings.TrimPrefix(proxy.URL, "http://"))
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assertViewNotContains(t, m, "pw-secret")
	assertViewContains(t, m, "user:xxxxx@")

	m = sendOptionsRequest(t, m, req)
	require.NotNil(t, m.Response())
	assert.Equal(t, "via proxy", string(m.Response().Body))
	assert.Equal(t, "http://origin.example.test/data", proxied, "the proxy received the absolute URL")
}

// Skip TLS verify in the editor lets a request reach a self-signed server. The
// same request without it fails certificate verification.
func TestE2E_OptionsEditor_SkipTLSVerifyIsPerRequest(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "tls ok")
	}))
	t.Cleanup(srv.Close)

	req := &domain.Request{Name: "tls", Method: http.MethodGet, URL: srv.URL}
	m := optionsModel(t, req, transportExecutor(t))
	m = sendOptionsRequest(t, m, req)
	assert.Nil(t, m.Response(), "a self-signed certificate must be rejected by default")
	assert.Error(t, m.Err())

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	for i := 0; i < 4; i++ {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}}) // toggles the TLS row
	assertViewContains(t, m, "Skip TLS verify: yes")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assertViewContains(t, m, "TLS verification off")

	m = sendOptionsRequest(t, m, req)
	require.NotNil(t, m.Response())
	assert.Equal(t, "tls ok", string(m.Response().Body))
}

// Invalid values keep the editor open with the reason; esc leaves the request
// as it was.
func TestE2E_OptionsEditor_RejectsBadValuesAndCancels(t *testing.T) {
	bad := []struct {
		name string
		row  int
		text string
		want string
	}{
		{"timeout", 0, "soon", "timeout must be a duration"},
		{"max redirects", 2, "-1", "max redirects must be a whole number"},
		{"proxy scheme", 3, "ftp://files.example.test", "proxy: "},
	}
	for _, b := range bad {
		t.Run(b.name, func(t *testing.T) {
			req := &domain.Request{Name: "plain", Method: http.MethodGet, URL: "http://example.test/plain"}
			m := optionsModel(t, req, transportExecutor(t))
			m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
			for i := 0; i < b.row; i++ {
				m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
			}
			m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
			m = optionKeys(t, m, b.text)
			m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
			m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // save attempt
			assertViewContains(t, m, b.want)
			assert.Equal(t, tui.OptionsField, m.ActiveField(), "a rejected value keeps the editor open")
			require.NotNil(t, m.ActiveRequest())
			assert.True(t, m.ActiveRequest().Options.IsZero(), "nothing is saved for a rejected value")

			m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
			assert.Equal(t, tui.NoneField, m.ActiveField())
			assert.True(t, m.ActiveRequest().Options.IsZero())
		})
	}
}

// A stored proxy that is not a valid proxy URL fails the request before it is
// sent, and a transport that cannot apply per-request options says so.
func TestE2E_RequestOptions_ExecutorRefusals(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "should not be reached")
	}))
	t.Cleanup(srv.Close)

	t.Run("invalid stored proxy", func(t *testing.T) {
		executor := transportExecutor(t)
		_, err := executor.Execute(context.Background(), &domain.Request{
			ID: uuid.NewString(), Name: "bad-proxy", Method: http.MethodGet, URL: srv.URL,
			Options: domain.RequestOptions{Proxy: "gopher://proxy.example.test"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "proxy scheme")
	})

	t.Run("custom transport cannot apply proxy", func(t *testing.T) {
		executor := exec.New(roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("custom transport was used")
		}))
		_, err := executor.Execute(context.Background(), &domain.Request{
			ID: uuid.NewString(), Name: "custom", Method: http.MethodGet, URL: srv.URL,
			Options: domain.RequestOptions{SkipTLSVerify: true},
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, exec.ErrUnsupportedOption)
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
