package exec

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	osexec "os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/crazy-vedic/quark/internal/domain"
)

const (
	// oauth2ExpirySkew stops a token from being used this long before it
	// expires, so it does not expire while the request is in flight.
	oauth2ExpirySkew = 30 * time.Second
	// oauth2LoginTimeout bounds how long the user has to finish signing in
	// in the browser.
	oauth2LoginTimeout = 5 * time.Minute
	// maxOAuth2Response bounds the token endpoint response that is read.
	maxOAuth2Response = 64 << 10
)

// oauth2Token is a token response from the token endpoint.
type oauth2Token struct {
	tokenType    string
	accessToken  string
	refreshToken string
	// expiresAt is zero when the server sent no expires_in. Such a token is
	// used for the request that fetched it and never reused from the cache.
	expiresAt time.Time
}

func (t oauth2Token) usable(now time.Time) bool {
	return t.accessToken != "" && !t.expiresAt.IsZero() &&
		now.Before(t.expiresAt.Add(-oauth2ExpirySkew))
}

// authorization returns the Authorization header value for the token.
func (t oauth2Token) authorization() string {
	tokenType := strings.TrimSpace(t.tokenType)
	if tokenType == "" || strings.EqualFold(tokenType, "bearer") {
		tokenType = "Bearer"
	}
	return tokenType + " " + t.accessToken
}

// oauth2Cache holds tokens for the life of an Executor. Tokens are never
// written to the database or to disk.
type oauth2Cache struct {
	mu     sync.Mutex
	tokens map[string]oauth2Token
}

func newOAuth2Cache() *oauth2Cache {
	return &oauth2Cache{tokens: make(map[string]oauth2Token)}
}

func (c *oauth2Cache) get(key string) (oauth2Token, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.tokens[key]
	return t, ok
}

func (c *oauth2Cache) put(key string, t oauth2Token) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tokens[key] = t
}

// isOAuth2AuthType reports whether authType is an OAuth 2.0 grant.
func isOAuth2AuthType(authType string) bool {
	switch authType {
	case domain.AuthTypeOAuth2ClientCredentials, domain.AuthTypeOAuth2AuthorizationCode:
		return true
	default:
		return false
	}
}

// oauth2CacheKey identifies the grant and its credentials. The key is a hash
// so secrets are not kept as map keys.
func oauth2CacheKey(authType string, cfg domain.AuthConfig) string {
	h := sha256.New()
	for _, part := range []string{
		authType, cfg.AuthURL, cfg.TokenURL, cfg.ClientID, cfg.ClientSecret, cfg.Scope,
	} {
		_, _ = io.WriteString(h, part)
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func validateOAuth2Config(authType string, cfg domain.AuthConfig) error {
	if err := requireHTTPURL("token url", cfg.TokenURL); err != nil {
		return err
	}
	if cfg.ClientID == "" {
		return fmt.Errorf("invalid auth config: oauth2 client id is required")
	}
	switch authType {
	case domain.AuthTypeOAuth2ClientCredentials:
		if cfg.ClientSecret == "" {
			return fmt.Errorf("invalid auth config: oauth2 client secret is required")
		}
	case domain.AuthTypeOAuth2AuthorizationCode:
		if err := requireHTTPURL("auth url", cfg.AuthURL); err != nil {
			return err
		}
	}
	return nil
}

func requireHTTPURL(name, raw string) error {
	if raw == "" {
		return fmt.Errorf("invalid auth config: oauth2 %s is required", name)
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("invalid auth config: oauth2 %s must be an http or https URL", name)
	}
	return nil
}

// applyOAuth2Auth sets the Authorization header for an OAuth 2.0 request. It
// runs before the request timeout starts, because a browser sign-in can take
// longer than the request timeout.
func (e *Executor) applyOAuth2Auth(
	ctx context.Context,
	httpReq *http.Request,
	req *domain.Request,
) error {
	authType := domain.NormalizeAuthType(req.AuthType)
	if !isOAuth2AuthType(authType) {
		return nil
	}
	cfg, err := domain.ParseAuthConfig(req.AuthConfig)
	if err != nil {
		return fmt.Errorf("invalid auth config: %w", err)
	}
	if err := validateOAuth2Config(authType, cfg); err != nil {
		return err
	}
	token, err := e.oauth2Token(ctx, authType, cfg)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Authorization", token.authorization())
	return nil
}

// oauth2Token returns a usable token for the grant, from the cache when it is
// still valid. An expired authorization-code token is refreshed when it has a
// refresh token; if that fails, the user signs in again.
func (e *Executor) oauth2Token(
	ctx context.Context,
	authType string,
	cfg domain.AuthConfig,
) (oauth2Token, error) {
	key := oauth2CacheKey(authType, cfg)
	cached, hasCached := e.tokens.get(key)
	if hasCached && cached.usable(time.Now()) {
		return cached, nil
	}

	if authType == domain.AuthTypeOAuth2ClientCredentials {
		token, err := e.clientCredentialsToken(ctx, cfg)
		if err != nil {
			return oauth2Token{}, err
		}
		e.tokens.put(key, token)
		return token, nil
	}

	if hasCached && cached.refreshToken != "" {
		token, err := e.refreshOAuth2Token(ctx, cfg, cached.refreshToken)
		if err == nil {
			e.tokens.put(key, token)
			return token, nil
		}
		e.logger.Debug("exec: oauth2 refresh failed; signing in again", "err", err)
	}
	token, err := e.signInOAuth2(ctx, cfg)
	if err != nil {
		return oauth2Token{}, err
	}
	e.tokens.put(key, token)
	return token, nil
}

func (e *Executor) clientCredentialsToken(
	ctx context.Context,
	cfg domain.AuthConfig,
) (oauth2Token, error) {
	form := url.Values{"grant_type": {"client_credentials"}}
	if cfg.Scope != "" {
		form.Set("scope", cfg.Scope)
	}
	return e.postTokenRequest(ctx, cfg, form)
}

func (e *Executor) refreshOAuth2Token(
	ctx context.Context,
	cfg domain.AuthConfig,
	refreshToken string,
) (oauth2Token, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
	if cfg.Scope != "" {
		form.Set("scope", cfg.Scope)
	}
	token, err := e.postTokenRequest(ctx, cfg, form)
	if err != nil {
		return oauth2Token{}, err
	}
	if token.refreshToken == "" {
		// The server did not rotate the refresh token; keep the one we have.
		token.refreshToken = refreshToken
	}
	return token, nil
}

// oauth2TokenResponse is the JSON body of a token endpoint response (RFC 6749
// §5.1) or error (§5.2).
type oauth2TokenResponse struct {
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int64  `json:"expires_in"`
	RefreshToken     string `json:"refresh_token"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// postTokenRequest calls the token endpoint. Client credentials go in an HTTP
// Basic header when a secret is set (RFC 6749 §2.3.1); public clients send
// client_id in the body.
func (e *Executor) postTokenRequest(
	ctx context.Context,
	cfg domain.AuthConfig,
	form url.Values,
) (oauth2Token, error) {
	if e.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.timeout)
		defer cancel()
	}

	useBasic := cfg.ClientSecret != ""
	if !useBasic {
		form.Set("client_id", cfg.ClientID)
	}
	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost, cfg.TokenURL, strings.NewReader(form.Encode()),
	)
	if err != nil {
		return oauth2Token{}, fmt.Errorf("oauth2 token request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.Header.Set("Accept", "application/json")
	if useBasic {
		httpReq.SetBasicAuth(url.QueryEscape(cfg.ClientID), url.QueryEscape(cfg.ClientSecret))
	}

	now := time.Now()
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return oauth2Token{}, fmt.Errorf("oauth2 token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxOAuth2Response+1))
	if err != nil {
		return oauth2Token{}, fmt.Errorf("oauth2 token response: %w", err)
	}
	if len(body) > maxOAuth2Response {
		return oauth2Token{}, errors.New("oauth2 token response is too large")
	}

	var parsed oauth2TokenResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return oauth2Token{}, fmt.Errorf(
			"oauth2 token response is not JSON (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return oauth2Token{}, tokenErrorf(resp.StatusCode, parsed)
	}
	if parsed.AccessToken == "" {
		return oauth2Token{}, errors.New("oauth2 token response has no access_token")
	}
	token := oauth2Token{
		tokenType:    parsed.TokenType,
		accessToken:  parsed.AccessToken,
		refreshToken: parsed.RefreshToken,
	}
	if parsed.ExpiresIn > 0 {
		token.expiresAt = now.Add(time.Duration(parsed.ExpiresIn) * time.Second)
	}
	return token, nil
}

func tokenErrorf(status int, parsed oauth2TokenResponse) error {
	msg := fmt.Sprintf("oauth2 token endpoint returned HTTP %d", status)
	if parsed.Error != "" {
		msg += ": " + parsed.Error
	}
	if parsed.ErrorDescription != "" {
		msg += " (" + parsed.ErrorDescription + ")"
	}
	return errors.New(msg)
}

// oauth2Callback is the outcome of the browser redirect.
type oauth2Callback struct {
	code string
	err  error
}

// parseOAuth2Callback reads the redirect query. It reports false when the
// request is not an answer to this sign-in (wrong state or no code and no
// error), so the listener keeps waiting for the real redirect.
func parseOAuth2Callback(query url.Values, state string) (oauth2Callback, bool) {
	if query.Get("state") != state {
		return oauth2Callback{}, false
	}
	if errCode := query.Get("error"); errCode != "" {
		msg := errCode
		if desc := query.Get("error_description"); desc != "" {
			msg += ": " + desc
		}
		return oauth2Callback{err: errors.New(msg)}, true
	}
	code := query.Get("code")
	if code == "" {
		return oauth2Callback{}, false
	}
	return oauth2Callback{code: code}, true
}

// pkceChallenge returns the S256 code challenge for verifier (RFC 7636 §4.2).
func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// signInOAuth2 runs the authorization code flow with PKCE. It listens on a
// loopback port, opens the authorization URL in the browser, waits for the
// redirect and exchanges the code for a token.
func (e *Executor) signInOAuth2(ctx context.Context, cfg domain.AuthConfig) (oauth2Token, error) {
	verifier, err := randomToken(32)
	if err != nil {
		return oauth2Token{}, fmt.Errorf("oauth2 sign-in: %w", err)
	}
	state, err := randomToken(16)
	if err != nil {
		return oauth2Token{}, fmt.Errorf("oauth2 sign-in: %w", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return oauth2Token{}, fmt.Errorf("oauth2 sign-in: listen for redirect: %w", err)
	}
	redirectURI := "http://" + listener.Addr().String() + "/callback"

	authURL, err := url.Parse(cfg.AuthURL)
	if err != nil {
		_ = listener.Close()
		return oauth2Token{}, fmt.Errorf("oauth2 sign-in: auth url: %w", err)
	}
	query := authURL.Query()
	query.Set("response_type", "code")
	query.Set("client_id", cfg.ClientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("state", state)
	query.Set("code_challenge", pkceChallenge(verifier))
	query.Set("code_challenge_method", "S256")
	if cfg.Scope != "" {
		query.Set("scope", cfg.Scope)
	}
	authURL.RawQuery = query.Encode()

	callbacks := make(chan oauth2Callback, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		cb, ok := parseOAuth2Callback(r.URL.Query(), state)
		if !ok {
			http.Error(w, "unexpected sign-in response", http.StatusBadRequest)
			return
		}
		select {
		case callbacks <- cb:
		default:
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "Signed in. You can close this tab and return to Quark.\n")
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(listener) }()
	defer func() { _ = server.Close() }()

	if err := e.openBrowser(authURL.String()); err != nil {
		return oauth2Token{}, fmt.Errorf("oauth2 sign-in: open browser: %w", err)
	}

	timer := time.NewTimer(oauth2LoginTimeout)
	defer timer.Stop()
	var cb oauth2Callback
	select {
	case cb = <-callbacks:
	case <-ctx.Done():
		return oauth2Token{}, fmt.Errorf("oauth2 sign-in: %w", ctx.Err())
	case <-timer.C:
		return oauth2Token{}, errors.New("oauth2 sign-in: timed out waiting for the browser")
	}
	if cb.err != nil {
		return oauth2Token{}, fmt.Errorf("oauth2 sign-in was not completed: %w", cb.err)
	}

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {cb.code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}
	return e.postTokenRequest(ctx, cfg, form)
}

// openSystemBrowser opens target in the user's browser without waiting for it.
func openSystemBrowser(target string) error {
	var cmd *osexec.Cmd
	// target is the authorization URL Quark built; it is passed as one
	// argument and never through a shell.
	switch runtime.GOOS {
	case "darwin":
		cmd = osexec.Command("open", target) //nolint:gosec // see above
	case "windows":
		cmd = osexec.Command( //nolint:gosec // see above
			"rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = osexec.Command("xdg-open", target) //nolint:gosec // see above
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
