//go:build e2e

package e2e

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// OAuth2Options controls how an OAuth2Server answers. Change them with
// OAuth2Server.Update before the requests that depend on them.
type OAuth2Options struct {
	ClientID     string
	ClientSecret string // empty means a public client: client_id goes in the body
	// TokenType is returned as token_type when non-empty.
	TokenType string
	// ExpiresIn is returned as expires_in when positive; zero omits it.
	ExpiresIn int
	// IssueRefresh returns a refresh_token from the authorization_code grant.
	IssueRefresh bool
	// RotateRefresh also returns a new refresh_token from refresh_token grants.
	RotateRefresh bool
	// RejectRefresh makes refresh_token grants fail with invalid_grant.
	RejectRefresh bool
	// DenyAccess makes /authorize redirect back with error=access_denied.
	DenyAccess bool
	// StaleCallbackFirst makes Browse send one redirect with a wrong state
	// before the real one.
	StaleCallbackFirst bool
}

// TokenGrant is one request received at the token endpoint.
type TokenGrant struct {
	Grant     string
	Form      url.Values
	HasBasic  bool
	BasicUser string
	BasicPass string
}

// OAuth2Server is a fake authorization server (/authorize), token endpoint
// (/token) and resource (/api). It checks PKCE, client authentication and
// the tokens it issued, so a test can tell whether Quark really signed in.
type OAuth2Server struct {
	*httptest.Server

	mu        sync.Mutex
	opts      OAuth2Options
	overrides map[string]http.HandlerFunc
	issued    int
	codeSeq   int
	codes     map[string]pendingCode
	refresh   map[string]bool
	tokens    map[string]bool
	grants    []TokenGrant
	resource  []string
	authorize []url.Values
}

type pendingCode struct {
	challenge   string
	redirectURI string
}

// NewOAuth2Server starts the fake server. The server is closed when the test ends.
func NewOAuth2Server(t testing.TB, opts OAuth2Options) *OAuth2Server {
	t.Helper()
	s := &OAuth2Server{
		opts:      opts,
		overrides: make(map[string]http.HandlerFunc),
		codes:     make(map[string]pendingCode),
		refresh:   make(map[string]bool),
		tokens:    make(map[string]bool),
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

// Update changes the options under the server's lock.
func (s *OAuth2Server) Update(change func(*OAuth2Options)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	change(&s.opts)
}

// Override replaces the handler for path, for error and malformed responses.
func (s *OAuth2Server) Override(path string, h http.HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.overrides[path] = h
}

// Grants returns the token requests received, in order.
func (s *OAuth2Server) Grants() []TokenGrant {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]TokenGrant(nil), s.grants...)
}

// ResourceAuthorizations returns the Authorization headers sent to /api.
func (s *OAuth2Server) ResourceAuthorizations() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.resource...)
}

// AuthorizeQueries returns the query of each /authorize request, in order.
func (s *OAuth2Server) AuthorizeQueries() []url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]url.Values(nil), s.authorize...)
}

func (s *OAuth2Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	h, overridden := s.overrides[r.URL.Path]
	s.mu.Unlock()
	if overridden {
		h(w, r)
		return
	}
	switch r.URL.Path {
	case "/authorize":
		s.serveAuthorize(w, r)
	case "/token":
		s.serveToken(w, r)
	case "/api":
		s.serveResource(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *OAuth2Server) serveAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	s.mu.Lock()
	s.authorize = append(s.authorize, q)
	opts := s.opts
	var code string
	if !opts.DenyAccess {
		s.codeSeq++
		code = fmt.Sprintf("code-%d", s.codeSeq)
		s.codes[code] = pendingCode{
			challenge:   q.Get("code_challenge"),
			redirectURI: q.Get("redirect_uri"),
		}
	}
	s.mu.Unlock()

	if q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" {
		http.Error(w, "unsupported authorize request", http.StatusBadRequest)
		return
	}
	loc, err := url.Parse(q.Get("redirect_uri"))
	if err != nil {
		http.Error(w, "bad redirect_uri", http.StatusBadRequest)
		return
	}
	back := loc.Query()
	back.Set("state", q.Get("state"))
	if opts.DenyAccess {
		back.Set("error", "access_denied")
		back.Set("error_description", "user denied")
	} else {
		back.Set("code", code)
	}
	loc.RawQuery = back.Encode()
	// The fake server sends the user back to the client's redirect_uri by design.
	//nolint:gosec // G710: test server echoes redirect_uri
	http.Redirect(w, r, loc.String(), http.StatusFound)
}

func (s *OAuth2Server) serveToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	basicUser, basicPass, hasBasic := r.BasicAuth()
	grant := TokenGrant{
		Grant:     r.PostForm.Get("grant_type"),
		Form:      r.PostForm,
		HasBasic:  hasBasic,
		BasicUser: basicUser,
		BasicPass: basicPass,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.grants = append(s.grants, grant)
	if !clientAuthenticated(s.opts, r, basicUser, basicPass, hasBasic) {
		writeJSON(w, http.StatusUnauthorized, oauthErr("invalid_client", ""))
		return
	}

	switch grant.Grant {
	case "client_credentials":
	case "authorization_code":
		pending, ok := s.codes[r.PostForm.Get("code")]
		delete(s.codes, r.PostForm.Get("code"))
		if !ok || pending.redirectURI != r.PostForm.Get("redirect_uri") {
			writeJSON(w, http.StatusBadRequest, oauthErr("invalid_grant", ""))
			return
		}
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if base64.RawURLEncoding.EncodeToString(sum[:]) != pending.challenge {
			writeJSON(w, http.StatusBadRequest,
				oauthErr("invalid_grant", "PKCE verifier does not match the challenge"))
			return
		}
	case "refresh_token":
		if s.opts.RejectRefresh || !s.refresh[r.PostForm.Get("refresh_token")] {
			writeJSON(w, http.StatusBadRequest, oauthErr("invalid_grant", "refresh token revoked"))
			return
		}
	default:
		writeJSON(w, http.StatusBadRequest, oauthErr("unsupported_grant_type", ""))
		return
	}

	s.issued++
	access := fmt.Sprintf("tok-%d", s.issued)
	s.tokens[access] = true
	body := map[string]any{"access_token": access}
	if s.opts.TokenType != "" {
		body["token_type"] = s.opts.TokenType
	}
	if s.opts.ExpiresIn > 0 {
		body["expires_in"] = s.opts.ExpiresIn
	}
	issue := (grant.Grant == "authorization_code" && s.opts.IssueRefresh) ||
		(grant.Grant == "refresh_token" && s.opts.RotateRefresh)
	if issue {
		refresh := fmt.Sprintf("rt-%d", s.issued)
		s.refresh[refresh] = true
		body["refresh_token"] = refresh
	}
	writeJSON(w, http.StatusOK, body)
}

// clientAuthenticated checks client credentials the way the server would: a
// secret must arrive as HTTP Basic, and a public client sends client_id only.
func clientAuthenticated(
	opts OAuth2Options, r *http.Request, user, pass string, hasBasic bool,
) bool {
	if opts.ClientSecret == "" {
		return !hasBasic && r.PostForm.Get("client_id") == opts.ClientID
	}
	return hasBasic &&
		user == url.QueryEscape(opts.ClientID) && pass == url.QueryEscape(opts.ClientSecret)
}

func (s *OAuth2Server) serveResource(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	s.mu.Lock()
	s.resource = append(s.resource, auth)
	token := strings.TrimPrefix(auth, strings.SplitN(auth, " ", 2)[0]+" ")
	ok := strings.Contains(auth, " ") && s.tokens[token]
	s.mu.Unlock()
	if !ok {
		http.Error(w, "no valid token", http.StatusUnauthorized)
		return
	}
	_, _ = io.WriteString(w, "resource ok")
}

// Browse acts as the user's browser: it opens the authorization URL, follows
// the redirect to the Quark callback, and, when StaleCallbackFirst is set,
// first sends the callback with a wrong state.
func (s *OAuth2Server) Browse(target string) error {
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Get(target)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		return fmt.Errorf("authorize returned HTTP %d", resp.StatusCode)
	}
	location := resp.Header.Get("Location")

	s.mu.Lock()
	stale := s.opts.StaleCallbackFirst
	s.mu.Unlock()
	if stale {
		wrong, err := url.Parse(location)
		if err != nil {
			return err
		}
		q := wrong.Query()
		q.Set("state", "not-the-state-we-sent")
		wrong.RawQuery = q.Encode()
		if err := getDiscard(client, wrong.String()); err != nil {
			return err
		}
	}
	return getDiscard(client, location)
}

func getDiscard(client *http.Client, target string) error {
	resp, err := client.Get(target)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.Body.Close()
}

// oauthErr is an RFC 6749 §5.2 error body.
func oauthErr(code, description string) map[string]string {
	body := map[string]string{"error": code}
	if description != "" {
		body["error_description"] = description
	}
	return body
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
