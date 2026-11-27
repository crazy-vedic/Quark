package exec

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

// RFC 7636 Appendix B.
func TestPKCEChallengeRFC7636Vector(t *testing.T) {
	assert.Equal(t,
		"E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
		pkceChallenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"))
}

func TestParseOAuth2Callback(t *testing.T) {
	q := func(s string) url.Values { v, _ := url.ParseQuery(s); return v }

	_, ok := parseOAuth2Callback(q("code=c&state=wrong"), "right")
	assert.False(t, ok, "a callback with another state is not ours")

	_, ok = parseOAuth2Callback(q("state=right"), "right")
	assert.False(t, ok, "no code and no error is not an answer")

	cb, ok := parseOAuth2Callback(q("code=c&state=right"), "right")
	require.True(t, ok)
	assert.Equal(t, "c", cb.code)

	cb, ok = parseOAuth2Callback(q("error=access_denied&error_description=no&state=right"), "right")
	require.True(t, ok)
	require.Error(t, cb.err)
	assert.Equal(t, "access_denied: no", cb.err.Error())
}

func TestOAuth2AuthorizationHeader(t *testing.T) {
	assert.Equal(t, "Bearer abc", oauth2Token{accessToken: "abc"}.authorization())
	assert.Equal(
		t,
		"Bearer abc",
		oauth2Token{tokenType: "bearer", accessToken: "abc"}.authorization(),
	)
	assert.Equal(t, "MAC abc", oauth2Token{tokenType: "MAC", accessToken: "abc"}.authorization())
}

func TestOAuth2TokenUsableRespectsSkew(t *testing.T) {
	now := time.Now()
	assert.True(t, oauth2Token{accessToken: "a", expiresAt: now.Add(time.Hour)}.usable(now))
	assert.False(t, oauth2Token{accessToken: "a", expiresAt: now.Add(10 * time.Second)}.usable(now))
	assert.False(t, oauth2Token{accessToken: "a"}.usable(now), "no expiry is never reused")
}

// tokenServer answers token requests. handle receives the parsed form and
// returns the status and JSON body.
func tokenServer(
	t *testing.T,
	hits *atomic.Int32,
	handle func(form url.Values, r *http.Request) (int, string),
) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		status, body := handle(r.PostForm, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
}

// apiServer accepts a request only with the given Authorization header.
func apiServer(t *testing.T, want string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != want {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
}

func oauthRequest(
	t *testing.T,
	apiURL string,
	authType string,
	cfg domain.AuthConfig,
) *domain.Request {
	t.Helper()
	return &domain.Request{
		Method:     http.MethodGet,
		URL:        apiURL,
		AuthType:   authType,
		AuthConfig: domain.MustAuthConfigJSON(cfg),
	}
}

func TestExecute_ClientCredentialsCachesToken(t *testing.T) {
	var tokenHits atomic.Int32
	tokens := tokenServer(t, &tokenHits, func(form url.Values, r *http.Request) (int, string) {
		assert.Equal(t, "client_credentials", form.Get("grant_type"))
		assert.Equal(t, "read write", form.Get("scope"))
		assert.Empty(t, form.Get("client_id"), "confidential clients send credentials in Basic")
		id, secret, ok := r.BasicAuth()
		require.True(t, ok)
		assert.Equal(t, "app", id)
		assert.Equal(t, "s3cret", secret)
		return http.StatusOK, `{"access_token":"tok-1","token_type":"Bearer","expires_in":3600}`
	})
	defer tokens.Close()
	api := apiServer(t, "Bearer tok-1")
	defer api.Close()

	e := New(api.Client().Transport)
	req := oauthRequest(t, api.URL, domain.AuthTypeOAuth2ClientCredentials, domain.AuthConfig{
		TokenURL: tokens.URL, ClientID: "app", ClientSecret: "s3cret", Scope: "read write",
	})
	for range 2 {
		res, err := e.Execute(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, res.StatusCode)
		assert.Equal(t, "ok", string(res.Body))
	}
	assert.Equal(t, int32(1), tokenHits.Load(), "the second request reuses the cached token")
}

func TestExecute_ClientCredentialsFetchesAgainWhenTokenIsNotCacheable(t *testing.T) {
	var tokenHits atomic.Int32
	tokens := tokenServer(t, &tokenHits, func(_ url.Values, _ *http.Request) (int, string) {
		// expires_in shorter than the expiry skew is never reused.
		return http.StatusOK, `{"access_token":"short","expires_in":5}`
	})
	defer tokens.Close()
	api := apiServer(t, "Bearer short")
	defer api.Close()

	e := New(api.Client().Transport)
	req := oauthRequest(t, api.URL, domain.AuthTypeOAuth2ClientCredentials, domain.AuthConfig{
		TokenURL: tokens.URL, ClientID: "app", ClientSecret: "s3cret",
	})
	for range 2 {
		res, err := e.Execute(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, res.StatusCode)
	}
	assert.Equal(t, int32(2), tokenHits.Load())
}

func TestExecute_OAuth2TokenErrorDoesNotLeakSecret(t *testing.T) {
	var tokenHits atomic.Int32
	tokens := tokenServer(t, &tokenHits, func(_ url.Values, _ *http.Request) (int, string) {
		return http.StatusUnauthorized,
			`{"error":"invalid_client","error_description":"bad client"}`
	})
	defer tokens.Close()

	e := New(tokens.Client().Transport)
	_, err := e.Execute(context.Background(), oauthRequest(
		t, "http://127.0.0.1:1/api", domain.AuthTypeOAuth2ClientCredentials,
		domain.AuthConfig{TokenURL: tokens.URL, ClientID: "app", ClientSecret: "topsecret-value"},
	))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 401: invalid_client (bad client)")
	assert.NotContains(t, err.Error(), "topsecret-value")
}

func TestExecute_OAuth2ConfigValidation(t *testing.T) {
	e := New(http.DefaultTransport)
	//nolint:gosec // G101 flags the ClientSecret fixture field; these are not credentials.
	cases := map[string]domain.AuthConfig{
		"token url is required": {ClientID: "app", ClientSecret: "s"},
		"must be an http": {
			TokenURL:     "ftp://example.test/token",
			ClientID:     "app",
			ClientSecret: "s",
		},
		"client id is required": {TokenURL: "https://example.test/token", ClientSecret: "s"},
		"client secret is required": {
			TokenURL: "https://example.test/token", ClientID: "app",
		},
	}
	for want, cfg := range cases {
		_, err := e.Execute(context.Background(), oauthRequest(
			t, "http://127.0.0.1:1", domain.AuthTypeOAuth2ClientCredentials, cfg))
		require.Error(t, err, want)
		assert.Contains(t, err.Error(), want)
	}
}

// fakeBrowser stands in for the user's browser. It reads the authorization URL,
// records the PKCE challenge and redirects back with a code. Each entry in
// redirects is sent as a request to the loopback callback.
type fakeBrowser struct {
	mu        sync.Mutex
	opened    int
	challenge string
	redirects func(state string) []string
	openErr   error
}

func (b *fakeBrowser) open(target string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.openErr != nil {
		return b.openErr
	}
	b.opened++
	u, err := url.Parse(target)
	if err != nil {
		return err
	}
	q := u.Query()
	b.challenge = q.Get("code_challenge")
	redirect := q.Get("redirect_uri")
	state := q.Get("state")
	if b.redirects == nil {
		return nil
	}
	go func() {
		for _, suffix := range b.redirects(state) {
			resp, err := http.Get(redirect + suffix) //nolint:noctx // test browser
			if err == nil {
				_ = resp.Body.Close()
			}
		}
	}()
	return nil
}

func (b *fakeBrowser) openCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.opened
}

func authCodeConfig(tokenURL string) domain.AuthConfig {
	return domain.AuthConfig{
		AuthURL:  "https://auth.example.test/authorize?tenant=t1",
		TokenURL: tokenURL, ClientID: "app", ClientSecret: "s3cret", Scope: "read",
	}
}

func TestExecute_AuthorizationCodeSignsInWithPKCE(t *testing.T) {
	var tokenHits atomic.Int32
	var verifierSeen string
	browser := &fakeBrowser{redirects: func(state string) []string {
		return []string{
			"?code=wrong-state&state=forged", // ignored: wrong state
			"?code=the-code&state=" + state,
		}
	}}
	tokens := tokenServer(t, &tokenHits, func(form url.Values, _ *http.Request) (int, string) {
		assert.Equal(t, "authorization_code", form.Get("grant_type"))
		assert.Equal(t, "the-code", form.Get("code"))
		assert.True(t, strings.HasPrefix(form.Get("redirect_uri"), "http://127.0.0.1:"))
		verifierSeen = form.Get("code_verifier")
		return http.StatusOK, `{"access_token":"user-token","token_type":"Bearer","expires_in":3600,"refresh_token":"r1"}`
	})
	defer tokens.Close()
	api := apiServer(t, "Bearer user-token")
	defer api.Close()

	e := New(api.Client().Transport, WithBrowserOpener(browser.open))
	req := oauthRequest(
		t,
		api.URL,
		domain.AuthTypeOAuth2AuthorizationCode,
		authCodeConfig(tokens.URL),
	)

	res, err := e.Execute(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "ok", string(res.Body))
	require.NotEmpty(t, verifierSeen)
	browser.mu.Lock()
	challengeSeen := browser.challenge
	browser.mu.Unlock()
	assert.Equal(
		t,
		pkceChallenge(verifierSeen),
		challengeSeen,
		"the challenge matches the verifier",
	)

	// A second request reuses the token: no second sign-in.
	_, err = e.Execute(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, 1, browser.openCount())
	assert.Equal(t, int32(1), tokenHits.Load())
}

func TestExecute_AuthorizationCodeRefreshesExpiredToken(t *testing.T) {
	var refreshes atomic.Int32
	browser := &fakeBrowser{redirects: func(state string) []string {
		return []string{"?code=c1&state=" + state}
	}}
	tokens := tokenServer(
		t,
		new(atomic.Int32),
		func(form url.Values, _ *http.Request) (int, string) {
			switch form.Get("grant_type") {
			case "authorization_code":
				// expires_in under the skew: the token is never reused directly.
				return http.StatusOK, `{"access_token":"first","expires_in":5,"refresh_token":"r1"}`
			case "refresh_token":
				refreshes.Add(1)
				assert.Equal(t, "r1", form.Get("refresh_token"))
				return http.StatusOK, `{"access_token":"second","expires_in":5}`
			}
			return http.StatusBadRequest, `{"error":"unsupported_grant_type"}`
		},
	)
	defer tokens.Close()
	api := apiServer(t, "Bearer second")
	defer api.Close()

	e := New(api.Client().Transport, WithBrowserOpener(browser.open))
	req := oauthRequest(
		t,
		api.URL,
		domain.AuthTypeOAuth2AuthorizationCode,
		authCodeConfig(tokens.URL),
	)
	_, err := e.Execute(context.Background(), req)
	require.NoError(t, err)

	res, err := e.Execute(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, int32(1), refreshes.Load())
	assert.Equal(t, 1, browser.openCount(), "refresh replaces the second sign-in")
}

func TestExecute_AuthorizationCodeSignsInAgainWhenRefreshFails(t *testing.T) {
	var codes atomic.Int32
	browser := &fakeBrowser{redirects: func(state string) []string {
		return []string{"?code=c&state=" + state}
	}}
	tokens := tokenServer(
		t,
		new(atomic.Int32),
		func(form url.Values, _ *http.Request) (int, string) {
			if form.Get("grant_type") == "refresh_token" {
				return http.StatusBadRequest, `{"error":"invalid_grant"}`
			}
			codes.Add(1)
			return http.StatusOK, `{"access_token":"tok","expires_in":5,"refresh_token":"r"}`
		},
	)
	defer tokens.Close()
	api := apiServer(t, "Bearer tok")
	defer api.Close()

	e := New(api.Client().Transport, WithBrowserOpener(browser.open))
	req := oauthRequest(
		t,
		api.URL,
		domain.AuthTypeOAuth2AuthorizationCode,
		authCodeConfig(tokens.URL),
	)
	for range 2 {
		res, err := e.Execute(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, res.StatusCode)
	}
	assert.Equal(t, 2, browser.openCount())
	assert.Equal(t, int32(2), codes.Load())
}

func TestExecute_AuthorizationCodeReportsDeniedSignIn(t *testing.T) {
	browser := &fakeBrowser{redirects: func(state string) []string {
		return []string{"?error=access_denied&error_description=nope&state=" + state}
	}}
	tokens := tokenServer(t, new(atomic.Int32), func(_ url.Values, _ *http.Request) (int, string) {
		t.Error("no token request after a denied sign-in")
		return http.StatusInternalServerError, ""
	})
	defer tokens.Close()

	e := New(tokens.Client().Transport, WithBrowserOpener(browser.open))
	_, err := e.Execute(context.Background(), oauthRequest(
		t,
		"http://127.0.0.1:1",
		domain.AuthTypeOAuth2AuthorizationCode,
		authCodeConfig(tokens.URL),
	))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "access_denied: nope")
}

func TestExecute_AuthorizationCodeBrowserFailure(t *testing.T) {
	tokens := tokenServer(t, new(atomic.Int32), func(_ url.Values, _ *http.Request) (int, string) {
		return http.StatusInternalServerError, ""
	})
	defer tokens.Close()

	browser := &fakeBrowser{openErr: errors.New("no opener")}
	e := New(tokens.Client().Transport, WithBrowserOpener(browser.open))
	_, err := e.Execute(context.Background(), oauthRequest(
		t,
		"http://127.0.0.1:1",
		domain.AuthTypeOAuth2AuthorizationCode,
		authCodeConfig(tokens.URL),
	))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open browser: no opener")
}

func TestExecute_AuthorizationCodeRequiresAuthURL(t *testing.T) {
	e := New(http.DefaultTransport)
	cfg := authCodeConfig("https://example.test/token")
	cfg.AuthURL = ""
	_, err := e.Execute(context.Background(), oauthRequest(
		t, "http://127.0.0.1:1", domain.AuthTypeOAuth2AuthorizationCode, cfg))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "oauth2 auth url is required")
}

func TestOAuth2CacheKeyDoesNotDependOnOtherFields(t *testing.T) {
	//nolint:gosec // G101 flags the ClientSecret fixture field; this is not a credential.
	a := domain.AuthConfig{TokenURL: "https://a", ClientID: "x", ClientSecret: "s"}
	b := a
	b.Scope = "other"
	assert.NotEqual(t, oauth2CacheKey(domain.AuthTypeOAuth2ClientCredentials, a),
		oauth2CacheKey(domain.AuthTypeOAuth2ClientCredentials, b))
	assert.NotEqual(t, oauth2CacheKey(domain.AuthTypeOAuth2ClientCredentials, a),
		oauth2CacheKey(domain.AuthTypeOAuth2AuthorizationCode, a))
	assert.Len(t, oauth2CacheKey(domain.AuthTypeOAuth2ClientCredentials, a), 64)
}

func TestRandomTokenIsURLSafe(t *testing.T) {
	tok, err := randomToken(32)
	require.NoError(t, err)
	raw, err := base64.RawURLEncoding.DecodeString(tok)
	require.NoError(t, err)
	assert.Len(t, raw, 32)
	assert.Len(t, tok, 43, "PKCE verifiers are 43 to 128 characters")
}
