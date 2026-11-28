//go:build e2e

package cli

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/tests/e2e"
)

// Client credentials sends the client secret as HTTP Basic, the scope in the
// body, and the token it gets as a Bearer header on the request.
func TestCLI_OAuth2ClientCredentialsSendsSecretAsBasicAndUsesToken(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	srv := e2e.NewOAuth2Server(t, e2e.OAuth2Options{ //nolint:gosec // G101: test credential
		ClientID:     "app",
		ClientSecret: "s e/cret",
		TokenType:    "bearer",
		ExpiresIn:    3600,
	})
	seedRequest(t, home, colID, &domain.Request{
		Name:     "Machine",
		Method:   http.MethodGet,
		URL:      srv.URL + "/api",
		AuthType: domain.AuthTypeOAuth2ClientCredentials,
		AuthConfig: fmt.Sprintf(
			`{"token_url":%q,"client_id":"app","client_secret":"s e/cret","scope":"read write"}`,
			srv.URL+"/token"),
	})

	out, errOut, code := runQuarkWithHome(t, home, "run", "API/Machine")
	require.Equal(t, 0, code, "run failed: %s %s", out, errOut)
	assert.Contains(t, out, "resource ok")

	grants := srv.Grants()
	require.Len(t, grants, 1)
	assert.Equal(t, "client_credentials", grants[0].Grant)
	assert.Equal(t, "read write", grants[0].Form.Get("scope"))
	assert.True(t, grants[0].HasBasic, "the secret must be sent as HTTP Basic")
	assert.Empty(t, grants[0].Form.Get("client_id"), "a confidential client does not repeat client_id in the body")
	assert.Equal(t, []string{"Bearer tok-1"}, srv.ResourceAuthorizations())
}

// A bad or missing setting is reported before any request leaves the machine.
func TestCLI_OAuth2InvalidConfigFailsBeforeSending(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   string
	}{
		{
			name:   "client credentials without a token URL",
			config: `{"client_id":"app","client_secret":"x"}`,
			want:   "oauth2 token url is required",
		},
		{
			name:   "token URL with a non-HTTP scheme",
			config: `{"token_url":"ftp://idp.example.test/token","client_id":"app","client_secret":"x"}`,
			want:   "oauth2 token url must be an http or https URL",
		},
		{
			name:   "client credentials without a client id",
			config: `{"token_url":"TOKEN","client_secret":"x"}`,
			want:   "oauth2 client id is required",
		},
		{
			name:   "client credentials without a secret",
			config: `{"token_url":"TOKEN","client_id":"app"}`,
			want:   "oauth2 client secret is required",
		},
		{
			name:   "authorization code without an authorization URL",
			config: `{"token_url":"TOKEN","client_id":"desktop"}`,
			want:   "oauth2 auth url is required",
		},
		{
			name:   "authorization URL that is not a URL",
			config: `{"auth_url":"not a url","token_url":"TOKEN","client_id":"desktop"}`,
			want:   "oauth2 auth url must be an http or https URL",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home, colID := setupHomeWithCollection(t)
			srv := e2e.NewOAuth2Server(t, e2e.OAuth2Options{ClientID: "app", ClientSecret: "x"})
			authType := domain.AuthTypeOAuth2ClientCredentials
			if strings.HasPrefix(tc.name, "authorization") {
				authType = domain.AuthTypeOAuth2AuthorizationCode
			}
			config := strings.ReplaceAll(tc.config, "TOKEN", srv.URL+"/token")
			seedRequest(t, home, colID, &domain.Request{
				Name:       "Bad",
				Method:     http.MethodGet,
				URL:        srv.URL + "/api",
				AuthType:   authType,
				AuthConfig: config,
			})

			out, errOut, code := runQuarkWithHome(t, home, "run", "API/Bad")
			assert.NotEqual(t, 0, code, "run must fail: %s", out)
			assert.Contains(t, errOut+out, tc.want)
			assert.Empty(t, srv.Grants(), "no token request may be sent for an invalid config")
			assert.Empty(t, srv.ResourceAuthorizations(), "the API must not be called")
		})
	}
}

// Errors from the token endpoint stop the request and name what went wrong.
func TestCLI_OAuth2TokenEndpointErrorsStopTheRequest(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{
			name: "OAuth error response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = fmt.Fprint(w, `{"error":"invalid_client","error_description":"bad secret"}`)
			},
			want: "oauth2 token endpoint returned HTTP 401: invalid_client (bad secret)",
		},
		{
			name: "HTML error page",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = fmt.Fprint(w, "<html>gateway</html>")
			},
			want: "oauth2 token response is not JSON (HTTP 500)",
		},
		{
			name: "success without an access token",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, `{"token_type":"bearer"}`)
			},
			want: "oauth2 token response has no access_token",
		},
		{
			name: "oversized response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"access_token":"x","pad":"%s"}`, strings.Repeat("a", 70<<10))
			},
			want: "oauth2 token response is too large",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home, colID := setupHomeWithCollection(t)
			srv := e2e.NewOAuth2Server(t, e2e.OAuth2Options{ClientID: "app", ClientSecret: "x"})
			srv.Override("/token", tc.handler)
			seedRequest(t, home, colID, &domain.Request{
				Name:     "Denied",
				Method:   http.MethodGet,
				URL:      srv.URL + "/api",
				AuthType: domain.AuthTypeOAuth2ClientCredentials,
				AuthConfig: fmt.Sprintf(
					`{"token_url":%q,"client_id":"app","client_secret":"x"}`, srv.URL+"/token"),
			})

			out, errOut, code := runQuarkWithHome(t, home, "run", "API/Denied")
			assert.NotEqual(t, 0, code, "run must fail: %s", out)
			assert.Contains(t, errOut+out, tc.want)
			assert.Empty(t, srv.ResourceAuthorizations(), "the API must not be called without a token")
		})
	}
}
