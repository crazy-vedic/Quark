//go:build e2e

package tui_test

import (
	"context"
	"net/http"
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

// The auth editor offers Digest after API key. Choosing it, filling the user
// and password, and sending makes the executor answer the server's 401
// challenge, and the server can verify the response.
func TestE2E_AuthEditor_DigestAnswersChallengeWhenSent(t *testing.T) {
	st, col := authTestStore(t)
	ctx := context.Background()

	ds := e2e.NewDigestServer(t, "alice", "s3cret",
		`Digest realm="api", nonce="tui-nonce", qop="auth", algorithm=SHA-256`,
	)
	req := &domain.Request{
		ID:           uuid.NewString(),
		CollectionID: col.ID,
		Name:         "digest",
		Method:       "GET",
		URL:          ds.URL + "/digest",
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
	for i := 0; i < 4; i++ { // None -> Bearer -> Basic -> API key -> Digest
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight})
	}
	assertViewContains(t, m, "Type: Digest")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	for _, r := range "alice" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	for _, r := range "s3cret" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	model, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = executeCmdUpdate(t, model, cmd)
	assert.Equal(t, domain.AuthTypeDigest, m.ActiveRequest().AuthType)
	assertViewContains(t, m, "Auth: Digest")

	saved, err := st.GetRequest(ctx, req.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.AuthTypeDigest, saved.AuthType)
	assert.JSONEq(t, `{"username":"alice","password":"s3cret"}`, saved.AuthConfig)

	model, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	m = executeCmdUpdate(t, model, cmd)
	require.NotNil(t, m.Response())
	assert.Equal(t, http.StatusOK, m.Response().StatusCode)

	reqs := ds.Requests()
	require.Len(t, reqs, 2, "one unauthenticated send and one answered retry")
	require.True(t, reqs[1].Authorized, "the server must accept the digest response")
	assert.Equal(t, "SHA-256", reqs[1].Params["algorithm"])
}

// Every challenge Quark answers, sent through the executor the TUI uses, gets
// a response the server verifies. Challenges Quark cannot answer leave the
// 401 as the response, with no retry.
func TestE2E_DigestExecutorChallengeMatrix(t *testing.T) {
	tests := []struct {
		name         string
		challenge    []string
		method       string
		body         string
		path         string
		wantAlg      string
		wantQop      string
		status       int
		attempts     int
		wantUserhash bool
	}{
		{
			name:      "MD5 with qop and opaque",
			challenge: []string{`Digest realm="r", nonce="n1", qop="auth", opaque="op", algorithm=MD5`},
			method:    http.MethodGet, path: "/md5", wantAlg: "MD5", wantQop: "auth",
			status: http.StatusOK, attempts: 2,
		},
		{
			name: "prefers SHA-256 over MD5 across headers",
			challenge: []string{
				`Basic realm="x"`,
				`Digest realm="r", nonce="n-md5", algorithm=MD5, qop="auth"`,
				`Digest realm="r", nonce="n-sha", algorithm=SHA-256, qop="auth"`,
			},
			method: http.MethodGet, path: "/pick", wantAlg: "SHA-256", wantQop: "auth",
			status: http.StatusOK, attempts: 2,
		},
		{
			name:      "several challenges in one header value",
			challenge: []string{`Basic realm="x", Digest realm="r", nonce="n-one", algorithm=SHA-256, qop="auth"`},
			method:    http.MethodGet, path: "/one", wantAlg: "SHA-256", wantQop: "auth",
			status: http.StatusOK, attempts: 2,
		},
		{
			name:      "MD5-sess",
			challenge: []string{`Digest realm="r", nonce="n-s", qop="auth", algorithm=MD5-sess`},
			method:    http.MethodGet, path: "/sess", wantAlg: "MD5-SESS", wantQop: "auth",
			status: http.StatusOK, attempts: 2,
		},
		{
			name:      "SHA-256-sess",
			challenge: []string{`Digest realm="r", nonce="n-ss", qop="auth", algorithm=SHA-256-sess`},
			method:    http.MethodGet, path: "/sha-sess", wantAlg: "SHA-256-SESS", wantQop: "auth",
			status: http.StatusOK, attempts: 2,
		},
		{
			name:      "RFC 2069 challenge without qop",
			challenge: []string{`Digest realm="legacy", nonce="n-2069"`},
			method:    http.MethodGet, path: "/2069", wantAlg: "MD5",
			status: http.StatusOK, attempts: 2,
		},
		{
			name:      "userhash and escaped realm",
			challenge: []string{`Digest realm="say \"hi\"", nonce="n-uh", qop="auth", userhash=true`},
			method:    http.MethodGet, path: "/uh", wantAlg: "MD5", wantQop: "auth",
			status: http.StatusOK, attempts: 2, wantUserhash: true,
		},
		{
			name:      "POST body is resent on the retry",
			challenge: []string{`Digest realm="r", nonce="n-post", qop="auth"`},
			method:    http.MethodPost, body: `{"k":"v"}`, path: "/post?q=1", wantAlg: "MD5", wantQop: "auth",
			status: http.StatusOK, attempts: 2,
		},
		{
			name:      "unsupported algorithm keeps the 401",
			challenge: []string{`Digest realm="r", nonce="n", algorithm=SHA-512-256, qop="auth"`},
			method:    http.MethodGet, path: "/sha512", status: http.StatusUnauthorized, attempts: 1,
		},
		{
			name:      "unsupported qop keeps the 401",
			challenge: []string{`Digest realm="r", nonce="n", qop="auth-int"`},
			method:    http.MethodGet, path: "/authint", status: http.StatusUnauthorized, attempts: 1,
		},
		{
			name:      "non-Digest 401 is not answered",
			challenge: []string{`Basic realm="r"`},
			method:    http.MethodGet, path: "/basic", status: http.StatusUnauthorized, attempts: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ds := e2e.NewDigestServer(t, "alice", "s3cret", tc.challenge...)
			transport := &http.Transport{}
			t.Cleanup(transport.CloseIdleConnections)
			executor := exec.New(transport)

			req := &domain.Request{
				ID:           uuid.NewString(),
				CollectionID: "matrix",
				Name:         tc.name,
				Method:       tc.method,
				URL:          ds.URL + tc.path,
				Body:         tc.body,
				AuthType:     domain.AuthTypeDigest,
				AuthConfig:   `{"username":"alice","password":"s3cret"}`,
			}
			result, err := executor.Execute(context.Background(), req)
			require.NoError(t, err)
			assert.Equal(t, tc.status, result.StatusCode)

			reqs := ds.Requests()
			require.Len(t, reqs, tc.attempts)
			if tc.attempts == 1 {
				return
			}
			retry := reqs[1]
			assert.True(t, retry.Authorized, "server rejected the response")
			assert.Equal(t, tc.method, retry.Method)
			assert.Equal(t, tc.path, retry.URI)
			assert.Equal(t, tc.body, retry.Body)
			assert.Equal(t, tc.wantAlg, retry.Params["algorithm"])
			assert.Equal(t, tc.wantQop, retry.Params["qop"])
			if tc.wantUserhash {
				assert.Equal(t, "true", retry.Params["userhash"])
				assert.Equal(t, `say "hi"`, retry.Params["realm"])
			}
		})
	}
}

// Digest auth with no credentials fails before any request is sent.
func TestE2E_DigestExecutorRejectsMissingCredentials(t *testing.T) {
	ds := e2e.NewDigestServer(t, "alice", "s3cret", `Digest realm="r", nonce="n", qop="auth"`)
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	executor := exec.New(transport)

	_, err := executor.Execute(context.Background(), &domain.Request{
		ID:         uuid.NewString(),
		Name:       "no-creds",
		Method:     http.MethodGet,
		URL:        ds.URL + "/nocreds",
		AuthType:   domain.AuthTypeDigest,
		AuthConfig: `{}`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "digest username or password is required")
	assert.Empty(t, ds.Requests())
}

// Importing `curl --digest -u user:pass` in the import dialog saves Digest
// request auth. The Basic Authorization header that -u sets is not kept.
func TestE2E_CurlImportDigestSavesDigestAuth(t *testing.T) {
	collection := &domain.Collection{ID: "digest-import-col", Name: "Digest imports"}
	st := setupStore(t, collection)
	command := `curl --digest -u alice:s3cret https://api.example.test/private`

	m := newE2EModel(t, st, &mockExecutor{})
	m = resize(t, m, 130, 42)
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{collection}))
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(command), Paste: true})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlS})
	assertViewContains(t, m, "GET")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Digest import")})
	_, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		callUpdate(t, m, runCmd(t, cmd))
	}

	requests, err := st.ListRequests(context.Background(), collection.ID)
	require.NoError(t, err)
	require.Len(t, requests, 1)
	assert.Equal(t, "Digest import", requests[0].Name)
	assert.Equal(t, domain.AuthTypeDigest, requests[0].AuthType)
	assert.JSONEq(t, `{"username":"alice","password":"s3cret"}`, requests[0].AuthConfig)
	assert.NotContains(t, requests[0].Headers, "Authorization")
}
