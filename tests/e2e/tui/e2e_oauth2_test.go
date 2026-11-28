//go:build e2e

package tui_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/tui"
	"github.com/crazy-vedic/quark/tests/e2e"
)

func typeRunes(t *testing.T, m tui.Model, text string) tui.Model {
	for _, r := range text {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

// editRow moves to the row below the cursor, opens it, types text, and closes it.
func editRow(t *testing.T, m tui.Model, text string) tui.Model {
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = typeRunes(t, m, text)
	return callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
}

func oauthExecutor(t *testing.T, opener func(string) error, opts ...exec.Option) *exec.Executor {
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	all := append([]exec.Option{exec.WithBrowserOpener(opener)}, opts...)
	return exec.New(transport, all...)
}

// The auth editor sets up client credentials with a secret. Sending the request
// sends the token it got from the token endpoint, and the secret stays hidden.
func TestE2E_AuthEditor_ClientCredentialsSendsToken(t *testing.T) {
	st, col := authTestStore(t)
	ctx := context.Background()
	srv := e2e.NewOAuth2Server(t, e2e.OAuth2Options{ClientID: "app", ClientSecret: "shh-secret"})

	req := &domain.Request{
		ID:           uuid.NewString(),
		CollectionID: col.ID,
		Name:         "machine",
		Method:       "GET",
		URL:          srv.URL + "/api",
	}
	require.NoError(t, st.SaveRequest(ctx, req))
	executor := oauthExecutor(t, srv.Browse)

	m := newE2EModel(t, st, executor).
		WithFocus(tui.RequestPane).
		WithActiveRequest(req).
		WithMethod(req.Method).
		WithURLValue(req.URL)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	for i := 0; i < 4; i++ { // None -> Bearer -> Basic -> API key -> OAuth2 client credentials
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight})
	}
	assertViewContains(t, m, "OAuth2 Client Credentials")
	m = editRow(t, m, srv.URL+"/token")
	m = editRow(t, m, "app")
	m = editRow(t, m, "shh-secret")
	assertViewContains(t, m, "Client Secret")
	assertViewNotContains(t, m, "shh-secret")

	model, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = executeCmdUpdate(t, model, cmd)
	assert.Equal(t, domain.AuthTypeOAuth2ClientCredentials, m.ActiveRequest().AuthType)
	assertViewContains(t, m, "OAuth2 Client Credentials")
	assertViewNotContains(t, m, "shh-secret")

	model, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	m = executeCmdUpdate(t, model, cmd)
	require.NotNil(t, m.Response())
	assert.Equal(t, http.StatusOK, m.Response().StatusCode)
	assert.Equal(t, []string{"Bearer tok-1"}, srv.ResourceAuthorizations())
	grants := srv.Grants()
	require.Len(t, grants, 1)
	assert.Equal(t, "client_credentials", grants[0].Grant)
	assert.True(t, grants[0].HasBasic)
}

// The authorization code editor signs in through the browser from the request
// pane. The token is sent and the editor shows the Auth Code type.
func TestE2E_AuthEditor_AuthorizationCodeSignsIn(t *testing.T) {
	st, col := authTestStore(t)
	ctx := context.Background()
	srv := e2e.NewOAuth2Server(t, e2e.OAuth2Options{ClientID: "desktop"})

	req := &domain.Request{
		ID:           uuid.NewString(),
		CollectionID: col.ID,
		Name:         "signed",
		Method:       "GET",
		URL:          srv.URL + "/api",
	}
	require.NoError(t, st.SaveRequest(ctx, req))
	executor := oauthExecutor(t, srv.Browse)

	m := newE2EModel(t, st, executor).
		WithFocus(tui.RequestPane).
		WithActiveRequest(req).
		WithMethod(req.Method).
		WithURLValue(req.URL)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	for i := 0; i < 5; i++ { // ... -> API key -> OAuth2 client credentials -> OAuth2 auth code
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight})
	}
	assertViewContains(t, m, "OAuth2 Auth Code")
	m = editRow(t, m, srv.URL+"/authorize")
	m = editRow(t, m, srv.URL+"/token")
	m = editRow(t, m, "desktop")

	model, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = executeCmdUpdate(t, model, cmd)
	saved, err := st.GetRequest(ctx, req.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.AuthTypeOAuth2AuthorizationCode, saved.AuthType)
	assert.JSONEq(t,
		fmt.Sprintf(`{"auth_url":%q,"token_url":%q,"client_id":"desktop"}`,
			srv.URL+"/authorize", srv.URL+"/token"),
		saved.AuthConfig)

	model, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	m = executeCmdUpdate(t, model, cmd)
	require.NotNil(t, m.Response())
	assert.Equal(t, http.StatusOK, m.Response().StatusCode)
	assert.Equal(t, []string{"Bearer tok-1"}, srv.ResourceAuthorizations())
	require.Len(t, srv.AuthorizeQueries(), 1)
	assert.Equal(t, "S256", srv.AuthorizeQueries()[0].Get("code_challenge_method"))
}

// Within one executor, a token that has not expired is reused, while a token
// without expires_in is never cached and is fetched again for each request.
func TestE2E_OAuth2TokenCacheReuse(t *testing.T) {
	tests := []struct {
		name      string
		expiresIn int
		wantToks  []string
		wantGrant int
	}{
		{name: "unexpired token is reused", expiresIn: 3600, wantToks: []string{"Bearer tok-1", "Bearer tok-1"}, wantGrant: 1},
		{name: "token without expires_in is not cached", expiresIn: 0, wantToks: []string{"Bearer tok-1", "Bearer tok-2"}, wantGrant: 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := e2e.NewOAuth2Server(t, e2e.OAuth2Options{ClientID: "app", ClientSecret: "x", ExpiresIn: tc.expiresIn})
			executor := oauthExecutor(t, srv.Browse)
			req := &domain.Request{
				ID: uuid.NewString(), CollectionID: "cache", Name: "cached", Method: http.MethodGet,
				URL:        srv.URL + "/api",
				AuthType:   domain.AuthTypeOAuth2ClientCredentials,
				AuthConfig: fmt.Sprintf(`{"token_url":%q,"client_id":"app","client_secret":"x"}`, srv.URL+"/token"),
			}
			for i := 0; i < 2; i++ {
				res, err := executor.Execute(context.Background(), req)
				require.NoError(t, err)
				assert.Equal(t, http.StatusOK, res.StatusCode)
			}
			assert.Equal(t, tc.wantToks, srv.ResourceAuthorizations())
			assert.Len(t, srv.Grants(), tc.wantGrant)
		})
	}
}

// Refresh: an expired authorization-code token is refreshed with its refresh
// token and the browser is not opened again. A rejected refresh falls back to
// signing in again.
func TestE2E_OAuth2RefreshAndSignInFallback(t *testing.T) {
	t.Run("refresh keeps the refresh token when the server does not rotate it", func(t *testing.T) {
		srv := e2e.NewOAuth2Server(t, e2e.OAuth2Options{ClientID: "desktop", ExpiresIn: 1, IssueRefresh: true})
		browsed := 0
		executor := oauthExecutor(t, func(target string) error {
			browsed++
			return srv.Browse(target)
		})
		req := authCodeRequest(srv.URL, "desktop")

		for i := 0; i < 3; i++ {
			res, err := executor.Execute(context.Background(), req)
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, res.StatusCode)
		}
		assert.Equal(t, 1, browsed, "refreshing must not open the browser again")
		assert.Equal(t, []string{"Bearer tok-1", "Bearer tok-2", "Bearer tok-3"}, srv.ResourceAuthorizations())
		grants := srv.Grants()
		require.Len(t, grants, 3)
		assert.Equal(t, "authorization_code", grants[0].Grant)
		assert.Equal(t, "refresh_token", grants[1].Grant)
		assert.Equal(t, "rt-1", grants[1].Form.Get("refresh_token"))
		assert.Equal(t, "rt-1", grants[2].Form.Get("refresh_token"), "the refresh token is kept when not rotated")
	})

	t.Run("refresh rotates the refresh token when the server sends a new one", func(t *testing.T) {
		srv := e2e.NewOAuth2Server(t, e2e.OAuth2Options{
			ClientID: "desktop", ExpiresIn: 1, IssueRefresh: true, RotateRefresh: true,
		})
		executor := oauthExecutor(t, srv.Browse)
		req := authCodeRequest(srv.URL, "desktop")
		for i := 0; i < 3; i++ {
			_, err := executor.Execute(context.Background(), req)
			require.NoError(t, err)
		}
		grants := srv.Grants()
		require.Len(t, grants, 3)
		assert.Equal(t, "rt-1", grants[1].Form.Get("refresh_token"))
		assert.Equal(t, "rt-2", grants[2].Form.Get("refresh_token"), "the rotated refresh token is used next")
	})

	t.Run("rejected refresh signs in again", func(t *testing.T) {
		srv := e2e.NewOAuth2Server(t, e2e.OAuth2Options{ClientID: "desktop", ExpiresIn: 1, IssueRefresh: true})
		browsed := 0
		executor := oauthExecutor(t, func(target string) error {
			browsed++
			return srv.Browse(target)
		})
		req := authCodeRequest(srv.URL, "desktop")
		_, err := executor.Execute(context.Background(), req)
		require.NoError(t, err)

		srv.Update(func(o *e2e.OAuth2Options) { o.RejectRefresh = true })
		res, err := executor.Execute(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, res.StatusCode)
		assert.Equal(t, 2, browsed, "a rejected refresh must sign the user in again")
		assert.Equal(t, "Bearer tok-2", srv.ResourceAuthorizations()[1])
	})
}

func authCodeRequest(srvURL, clientID string) *domain.Request {
	return &domain.Request{
		ID: uuid.NewString(), CollectionID: "signin", Name: "signed", Method: http.MethodGet,
		URL:      srvURL + "/api",
		AuthType: domain.AuthTypeOAuth2AuthorizationCode,
		AuthConfig: fmt.Sprintf(`{"auth_url":%q,"token_url":%q,"client_id":%q}`,
			srvURL+"/authorize", srvURL+"/token", clientID),
	}
}

// Token types other than Bearer are sent as they were issued.
func TestE2E_OAuth2TokenTypeIsKept(t *testing.T) {
	srv := e2e.NewOAuth2Server(t, e2e.OAuth2Options{ClientID: "app", ClientSecret: "x", TokenType: "MAC"})
	executor := oauthExecutor(t, srv.Browse)
	_, err := executor.Execute(context.Background(), &domain.Request{
		ID: uuid.NewString(), CollectionID: "mac", Name: "mac", Method: http.MethodGet,
		URL:        srv.URL + "/api",
		AuthType:   domain.AuthTypeOAuth2ClientCredentials,
		AuthConfig: fmt.Sprintf(`{"token_url":%q,"client_id":"app","client_secret":"x"}`, srv.URL+"/token"),
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"MAC tok-1"}, srv.ResourceAuthorizations())
}

// Sign-in failures in the browser flow are reported to the caller.
func TestE2E_OAuth2SignInFailures(t *testing.T) {
	t.Run("denied access", func(t *testing.T) {
		srv := e2e.NewOAuth2Server(t, e2e.OAuth2Options{ClientID: "desktop", DenyAccess: true})
		executor := oauthExecutor(t, srv.Browse)
		_, err := executor.Execute(context.Background(), authCodeRequest(srv.URL, "desktop"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "oauth2 sign-in was not completed: access_denied: user denied")
	})

	t.Run("browser cannot be opened", func(t *testing.T) {
		executor := oauthExecutor(t, func(string) error { return errors.New("no browser") })
		_, err := executor.Execute(context.Background(), authCodeRequest("http://127.0.0.1:1", "desktop"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "oauth2 sign-in: open browser: no browser")
	})

	t.Run("cancelled while waiting for the browser", func(t *testing.T) {
		srv := e2e.NewOAuth2Server(t, e2e.OAuth2Options{ClientID: "desktop"})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		executor := oauthExecutor(t, func(string) error {
			cancel()
			return nil
		})
		_, err := executor.Execute(ctx, authCodeRequest(srv.URL, "desktop"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "oauth2 sign-in: context canceled")
	})

	t.Run("stale callback is ignored", func(t *testing.T) {
		srv := e2e.NewOAuth2Server(t, e2e.OAuth2Options{ClientID: "desktop", StaleCallbackFirst: true})
		executor := oauthExecutor(t, srv.Browse)
		res, err := executor.Execute(context.Background(), authCodeRequest(srv.URL, "desktop"))
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, res.StatusCode)
	})

	t.Run("token endpoint unreachable", func(t *testing.T) {
		closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		closedURL := closed.URL
		closed.Close()
		executor := oauthExecutor(t, func(string) error { return nil })
		_, err := executor.Execute(context.Background(), &domain.Request{
			ID: uuid.NewString(), CollectionID: "down", Name: "down", Method: http.MethodGet,
			URL:        closedURL + "/api",
			AuthType:   domain.AuthTypeOAuth2ClientCredentials,
			AuthConfig: fmt.Sprintf(`{"token_url":%q,"client_id":"app","client_secret":"x"}`, closedURL+"/token"),
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "oauth2 token request:")
	})
}

// Token endpoint answers that are not usable are reported, not sent.
func TestE2E_OAuth2TokenEndpointResponseErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{
			name: "OAuth error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = fmt.Fprint(w, `{"error":"invalid_client","error_description":"bad secret"}`)
			},
			want: "oauth2 token endpoint returned HTTP 401: invalid_client (bad secret)",
		},
		{
			name: "not JSON",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprint(w, "<html>oops</html>")
			},
			want: "oauth2 token response is not JSON (HTTP 200)",
		},
		{
			name: "no access token",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprint(w, `{"token_type":"bearer","expires_in":60}`)
			},
			want: "oauth2 token response has no access_token",
		},
		{
			name: "too large",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprintf(w, `{"access_token":"x","pad":"%s"}`, strings.Repeat("a", 70<<10))
			},
			want: "oauth2 token response is too large",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := e2e.NewOAuth2Server(t, e2e.OAuth2Options{ClientID: "app", ClientSecret: "x"})
			srv.Override("/token", tc.handler)
			executor := oauthExecutor(t, srv.Browse)
			_, err := executor.Execute(context.Background(), &domain.Request{
				ID: uuid.NewString(), CollectionID: "errs", Name: "errs", Method: http.MethodGet,
				URL:        srv.URL + "/api",
				AuthType:   domain.AuthTypeOAuth2ClientCredentials,
				AuthConfig: fmt.Sprintf(`{"token_url":%q,"client_id":"app","client_secret":"x"}`, srv.URL+"/token"),
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.Empty(t, srv.ResourceAuthorizations())
		})
	}
}

// Invalid OAuth 2.0 settings fail before any token request.
func TestE2E_OAuth2InvalidSettingsFailBeforeSending(t *testing.T) {
	srv := e2e.NewOAuth2Server(t, e2e.OAuth2Options{ClientID: "app", ClientSecret: "x"})
	executor := oauthExecutor(t, srv.Browse)
	tests := []struct {
		name     string
		authType string
		config   string
		want     string
	}{
		{"no token url", domain.AuthTypeOAuth2ClientCredentials, `{"client_id":"app","client_secret":"x"}`, "token url is required"},
		{"token url scheme", domain.AuthTypeOAuth2ClientCredentials, `{"token_url":"ftp://x/t","client_id":"app","client_secret":"x"}`, "must be an http or https URL"},
		{"no client id", domain.AuthTypeOAuth2ClientCredentials, `{"token_url":"http://x/t","client_secret":"x"}`, "client id is required"},
		{"no client secret", domain.AuthTypeOAuth2ClientCredentials, `{"token_url":"http://x/t","client_id":"app"}`, "client secret is required"},
		{"no auth url", domain.AuthTypeOAuth2AuthorizationCode, `{"token_url":"http://x/t","client_id":"app"}`, "auth url is required"},
		{"auth url not http", domain.AuthTypeOAuth2AuthorizationCode, `{"auth_url":"::bad","token_url":"http://x/t","client_id":"app"}`, "auth url must be an http or https URL"},
		{"malformed JSON", domain.AuthTypeOAuth2ClientCredentials, `{"token_url":`, "invalid auth config"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := executor.Execute(context.Background(), &domain.Request{
				ID: uuid.NewString(), CollectionID: "bad", Name: "bad", Method: http.MethodGet,
				URL: srv.URL + "/api", AuthType: tc.authType, AuthConfig: tc.config,
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
	assert.Empty(t, srv.Grants())
}
