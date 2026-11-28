//go:build e2e

package cli

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
	"github.com/crazy-vedic/quark/tests/e2e"
)

const digestUser, digestPass = "alice", "s3cret"

func digestRequest(t *testing.T, home, colID, name, method, url, body string) {
	t.Helper()
	seedRequest(t, home, colID, &domain.Request{
		Name:       name,
		Method:     method,
		URL:        url,
		Body:       body,
		AuthType:   domain.AuthTypeDigest,
		AuthConfig: `{"username":"alice","password":"s3cret"}`,
	})
}

// A 401 Digest challenge with qop=auth is answered by one retry that carries
// a response the server can verify, with the challenge's opaque echoed back.
func TestCLI_RunDigestAnswersQopAuthChallenge(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	challenge := `Digest realm="api@example.test", nonce="dcd98b7102dd2f0e8b11d0f600bfb0c0",` +
		` qop="auth", opaque="5ccc069c403ebaf9", algorithm=MD5`
	ds := e2e.NewDigestServer(t, digestUser, digestPass, challenge)
	digestRequest(t, home, colID, "Digest", http.MethodGet, ds.URL+"/private", "")

	out, errOut, code := runQuarkWithHome(t, home, "run", "API/Digest")
	require.Equal(t, 0, code, "run failed: %s %s", out, errOut)
	assert.Contains(t, out, "authorized")

	reqs := ds.Requests()
	require.Len(t, reqs, 2, "one unauthenticated send and one retry")
	assert.Empty(t, reqs[0].Params)
	require.True(t, reqs[1].Authorized, "retry carried a verifiable digest response")
	assert.Equal(t, "MD5", reqs[1].Params["algorithm"])
	assert.Equal(t, "auth", reqs[1].Params["qop"])
	assert.Equal(t, "00000001", reqs[1].Params["nc"])
	assert.Equal(t, "5ccc069c403ebaf9", reqs[1].Params["opaque"])
	assert.Equal(t, "api@example.test", reqs[1].Params["realm"])
}

// Several challenges may arrive in separate headers or in one header value.
// SHA-256 wins over MD5, and schemes other than Digest are skipped.
func TestCLI_RunDigestPrefersSHA256Challenge(t *testing.T) {
	tests := []struct {
		name       string
		challenges []string
		wantNonce  string
	}{
		{
			name: "separate WWW-Authenticate headers",
			challenges: []string{
				`Basic realm="ignored"`,
				`Digest realm="api", nonce="n-md5", algorithm=MD5, qop="auth"`,
				`Digest realm="api", nonce="n-sha", algorithm=SHA-256, qop="auth,auth-int"`,
			},
			wantNonce: "n-sha",
		},
		{
			name: "several challenges in one header value",
			challenges: []string{
				`Basic realm="x", Digest realm="api", nonce="n-md5", algorithm=MD5, qop="auth", Digest realm="api", nonce="n-combined", algorithm=SHA-256, qop="auth"`,
			},
			wantNonce: "n-combined",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home, colID := setupHomeWithCollection(t)
			ds := e2e.NewDigestServer(t, digestUser, digestPass, tc.challenges...)
			digestRequest(t, home, colID, "Pick", http.MethodGet, ds.URL+"/pick", "")

			out, errOut, code := runQuarkWithHome(t, home, "run", "API/Pick")
			require.Equal(t, 0, code, "run failed: %s %s", out, errOut)
			reqs := ds.Requests()
			require.Len(t, reqs, 2)
			require.True(t, reqs[1].Authorized)
			assert.Equal(t, "SHA-256", reqs[1].Params["algorithm"])
			assert.Equal(t, tc.wantNonce, reqs[1].Params["nonce"])
		})
	}
}

// Each algorithm and qop variant Quark supports produces a response the
// server verifies: the -sess variants fold the nonce and cnonce into HA1, and
// a challenge without qop uses the RFC 2069 form with no nc or cnonce.
func TestCLI_RunDigestAlgorithmAndQopVariants(t *testing.T) {
	tests := []struct {
		name      string
		challenge string
		wantAlg   string
		wantQop   string
		wantUser  string
	}{
		{
			name:      "MD5-sess with qop",
			challenge: `Digest realm="r", nonce="n-sess", qop="auth", algorithm=MD5-sess`,
			wantAlg:   "MD5-SESS",
			wantQop:   "auth",
		},
		{
			name:      "SHA-256-sess with qop",
			challenge: `Digest realm="r", nonce="n-sha-sess", qop="auth", algorithm=SHA-256-sess`,
			wantAlg:   "SHA-256-SESS",
			wantQop:   "auth",
		},
		{
			name:      "RFC 2069 challenge without qop",
			challenge: `Digest realm="legacy", nonce="n-2069"`,
			wantAlg:   "MD5",
		},
		{
			name:      "userhash hashes the username",
			challenge: `Digest realm="r", nonce="n-uh", algorithm=sha-256, qop="auth", userhash=true`,
			wantAlg:   "SHA-256",
			wantQop:   "auth",
			wantUser:  "hashed",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home, colID := setupHomeWithCollection(t)
			ds := e2e.NewDigestServer(t, digestUser, digestPass, tc.challenge)
			digestRequest(t, home, colID, "Variant", http.MethodGet, ds.URL+"/v", "")

			out, errOut, code := runQuarkWithHome(t, home, "run", "API/Variant")
			require.Equal(t, 0, code, "run failed: %s %s", out, errOut)
			reqs := ds.Requests()
			require.Len(t, reqs, 2)
			require.True(t, reqs[1].Authorized, "server rejected the digest response")
			assert.Equal(t, tc.wantAlg, reqs[1].Params["algorithm"])
			assert.Equal(t, tc.wantQop, reqs[1].Params["qop"])
			if tc.wantUser != "" {
				assert.Equal(t, "true", reqs[1].Params["userhash"])
				assert.NotEqual(t, digestUser, reqs[1].Params["username"],
					"userhash=true must send a hashed username, not the plain one")
			}
		})
	}
}

// Quoted challenge values may contain escaped quotes and backslashes. Quark
// must echo them back in the form the server expects.
func TestCLI_RunDigestEscapedRealmRoundTrips(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	ds := e2e.NewDigestServer(t, digestUser, digestPass,
		`Digest realm="say \"hi\" \\ there", nonce="n-esc", qop="auth"`,
	)
	digestRequest(t, home, colID, "Escaped", http.MethodGet, ds.URL+"/esc", "")

	out, errOut, code := runQuarkWithHome(t, home, "run", "API/Escaped")
	require.Equal(t, 0, code, "run failed: %s %s", out, errOut)
	reqs := ds.Requests()
	require.Len(t, reqs, 2)
	require.True(t, reqs[1].Authorized)
	assert.Equal(t, `say "hi" \ there`, reqs[1].Params["realm"])
}

// A challenge Quark cannot answer leaves the 401 as the response. Nothing is
// retried, and a non-Digest 401 is never answered.
func TestCLI_RunDigestUnanswerableChallengeReturns401(t *testing.T) {
	tests := []struct {
		name      string
		challenge string
	}{
		{"unsupported algorithm", `Digest realm="r", nonce="n", algorithm=SHA-512-256, qop="auth"`},
		{"unsupported qop", `Digest realm="r", nonce="n", qop="auth-int"`},
		{"not a Digest challenge", `Basic realm="r"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home, colID := setupHomeWithCollection(t)
			ds := e2e.NewDigestServer(t, digestUser, digestPass, tc.challenge)
			digestRequest(t, home, colID, "Unanswered", http.MethodGet, ds.URL+"/u", "")

			out, errOut, code := runQuarkWithHome(t, home, "run", "API/Unanswered")
			require.Equal(t, 0, code, "run failed: %s %s", out, errOut)
			assert.Contains(t, out, "login required", "the 401 body is the response")
			assert.Len(t, ds.Requests(), 1, "a 401 that cannot be answered must not be retried")
		})
	}
}

// The retry resends the body and keeps the method and the query string the
// signature covers.
func TestCLI_RunDigestRetryResendsBodyAndQuery(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	ds := e2e.NewDigestServer(t, digestUser, digestPass,
		`Digest realm="api", nonce="n-post", qop="auth"`,
	)
	digestRequest(t, home, colID, "Upload", http.MethodPost,
		ds.URL+"/submit?x=1&y=two", `{"item":"a"}`)

	out, errOut, code := runQuarkWithHome(t, home, "run", "API/Upload")
	require.Equal(t, 0, code, "run failed: %s %s", out, errOut)
	reqs := ds.Requests()
	require.Len(t, reqs, 2)
	for _, r := range reqs {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/submit?x=1&y=two", r.URI)
		assert.Equal(t, `{"item":"a"}`, r.Body)
	}
	assert.True(t, reqs[1].Authorized)
}

// Digest auth with no username or password is rejected before anything is sent.
func TestCLI_RunDigestWithoutCredentialsFailsBeforeSending(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	ds := e2e.NewDigestServer(t, digestUser, digestPass,
		`Digest realm="api", nonce="n", qop="auth"`,
	)
	seedRequest(t, home, colID, &domain.Request{
		Name:       "NoCreds",
		Method:     http.MethodGet,
		URL:        ds.URL + "/nocreds",
		AuthType:   domain.AuthTypeDigest,
		AuthConfig: `{}`,
	})

	out, errOut, code := runQuarkWithHome(t, home, "run", "API/NoCreds")
	assert.NotEqual(t, 0, code, "run must fail: %s", out)
	assert.Contains(t, errOut+out, "digest username or password is required")
	assert.Empty(t, ds.Requests(), "no request may be sent without credentials")
}

// `-u user:pass --digest` imports as Digest request auth, not as a Basic
// Authorization header, and the imported request answers the challenge.
func TestCLI_CurlImportDigestUserAnswersChallenge(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	ds := e2e.NewDigestServer(t, digestUser, digestPass,
		`Digest realm="api", nonce="n-import", qop="auth"`,
	)
	command := "curl --digest -u " + digestUser + ":" + digestPass + " " + ds.URL + "/imported"

	out, errOut, code := runQuarkWithHome(t, home, "import", "curl", command, "--collection", colID, "--name", "Digest import")
	require.Equal(t, 0, code, "import failed: %s %s %s", out, errOut)

	st, err := store.New(filepath.Join(home, ".quark", "quark.db"))
	require.NoError(t, err)
	requests, err := st.ListRequests(context.Background(), colID)
	require.NoError(t, err)
	require.NoError(t, st.Close())
	require.Len(t, requests, 1)
	assert.Equal(t, domain.AuthTypeDigest, requests[0].AuthType)
	assert.JSONEq(t, `{"username":"alice","password":"s3cret"}`, requests[0].AuthConfig)
	assert.NotContains(t, requests[0].Headers, "Authorization", "-u must not persist as a Basic header")

	out, errOut, code = runQuarkWithHome(t, home, "run", "API/Digest import")
	require.Equal(t, 0, code, "run failed: %s %s", out, errOut)
	assert.Contains(t, out, "authorized")
	reqs := ds.Requests()
	require.Len(t, reqs, 2)
	assert.True(t, reqs[1].Authorized)
}

// Export writes the Digest credentials as curl's --digest -u pair, so the
// command reproduces the request.
func TestCLI_RequestExportCurlDigestAuth(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	seedRequest(t, home, colID, &domain.Request{
		Name:       "DigestExport",
		Method:     http.MethodGet,
		URL:        "https://api.example.test/private",
		AuthType:   domain.AuthTypeDigest,
		AuthConfig: `{"username":"alice","password":"s3cret"}`,
	})

	out, errOut, code := runQuarkWithHome(t, home, "request", "export-curl", "API/DigestExport")
	require.Equal(t, 0, code, "export failed: %s %s", out, errOut)
	assert.Equal(t, "curl -X 'GET' --digest -u 'alice:s3cret' 'https://api.example.test/private'\n", out)
}
