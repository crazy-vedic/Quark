//go:build e2e && !windows

package cli

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/tests/e2e"
)

// runQuarkWithFakeBrowser runs quark with a stand-in for the system browser
// opener first on PATH. The stand-in records the URL quark asks it to open,
// and drive plays the user: it follows that URL the way a browser would.
func runQuarkWithFakeBrowser(
	t *testing.T,
	home string,
	drive func(target string) error,
	args ...string,
) (string, string, int) {
	t.Helper()
	dir := t.TempDir()
	urlFile := filepath.Join(dir, "opened-url.txt")
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	script := "#!/bin/sh\nprintf '%s' \"$1\" > '" + urlFile + "'\n"
	// The stand-in must be executable, so 0700 is the least permission that works.
	require.NoError(t, os.WriteFile(filepath.Join(dir, opener), []byte(script), 0o700)) //nolint:gosec // G306: executable stand-in

	cmd := exec.Command(binaryPath, args...)
	cmd.Dir = home
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"USERPROFILE="+home,
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Start())

	var target []byte
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(urlFile); err == nil && len(b) > 0 {
			target = b
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(target) == 0 {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("quark never opened the browser; stderr: %s", stderr.String())
	}
	require.NoError(t, drive(string(target)), "fake browser could not finish sign-in")

	exitCode := 0
	if err := cmd.Wait(); err != nil {
		exitErr, ok := err.(*exec.ExitError)
		require.True(t, ok, "wait failed: %v", err)
		exitCode = exitErr.ExitCode()
	}
	return stdout.String(), stderr.String(), exitCode
}

func authCodeRequest(t *testing.T, home, colID, srvURL, clientID string) {
	t.Helper()
	seedRequest(t, home, colID, &domain.Request{
		Name:     "Signed",
		Method:   http.MethodGet,
		URL:      srvURL + "/api",
		AuthType: domain.AuthTypeOAuth2AuthorizationCode,
		AuthConfig: fmt.Sprintf(
			`{"auth_url":%q,"token_url":%q,"client_id":%q,"scope":"openid profile"}`,
			srvURL+"/authorize", srvURL+"/token", clientID),
	})
}

// A public client signs in with PKCE through the browser. Quark receives the
// redirect on a loopback port, exchanges the code, and sends the token.
func TestCLI_OAuth2AuthorizationCodeSignsInThroughBrowser(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	srv := e2e.NewOAuth2Server(t, e2e.OAuth2Options{ClientID: "desktop"})
	authCodeRequest(t, home, colID, srv.URL, "desktop")

	out, errOut, code := runQuarkWithFakeBrowser(t, home, srv.Browse, "run", "API/Signed")
	require.Equal(t, 0, code, "run failed: %s %s", out, errOut)
	assert.Contains(t, out, "resource ok")

	queries := srv.AuthorizeQueries()
	require.Len(t, queries, 1)
	q := queries[0]
	assert.Equal(t, "code", q.Get("response_type"))
	assert.Equal(t, "desktop", q.Get("client_id"))
	assert.Equal(t, "S256", q.Get("code_challenge_method"))
	assert.Equal(t, "openid profile", q.Get("scope"))
	assert.NotEmpty(t, q.Get("code_challenge"))
	assert.Contains(t, q.Get("redirect_uri"), "http://127.0.0.1:")

	grants := srv.Grants()
	require.Len(t, grants, 1)
	assert.Equal(t, "authorization_code", grants[0].Grant)
	assert.False(t, grants[0].HasBasic, "a public client sends no Basic credentials")
	assert.Equal(t, "desktop", grants[0].Form.Get("client_id"))
	assert.NotEmpty(t, grants[0].Form.Get("code_verifier"))
	assert.Equal(t, []string{"Bearer tok-1"}, srv.ResourceAuthorizations())
}

// A callback with a wrong state is not the answer to this sign-in. Quark
// keeps waiting for the real redirect and still completes the request.
func TestCLI_OAuth2AuthorizationCodeIgnoresStaleCallback(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	srv := e2e.NewOAuth2Server(t, e2e.OAuth2Options{ClientID: "desktop", StaleCallbackFirst: true})
	authCodeRequest(t, home, colID, srv.URL, "desktop")

	out, errOut, code := runQuarkWithFakeBrowser(t, home, srv.Browse, "run", "API/Signed")
	require.Equal(t, 0, code, "run failed: %s %s", out, errOut)
	assert.Contains(t, out, "resource ok")
	assert.Len(t, srv.Grants(), 1)
}

// The user refusing access at the authorization server ends the request with
// the server's reason, and no token is requested.
func TestCLI_OAuth2DeniedSignInReportsTheReason(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	srv := e2e.NewOAuth2Server(t, e2e.OAuth2Options{ClientID: "desktop", DenyAccess: true})
	authCodeRequest(t, home, colID, srv.URL, "desktop")

	out, errOut, code := runQuarkWithFakeBrowser(t, home, srv.Browse, "run", "API/Signed")
	assert.NotEqual(t, 0, code, "run must fail: %s", out)
	assert.Contains(t, errOut+out, "oauth2 sign-in was not completed: access_denied: user denied")
	assert.Empty(t, srv.Grants())
	assert.Empty(t, srv.ResourceAuthorizations())
}

// With no browser opener on PATH, sign-in fails at once and says so.
func TestCLI_OAuth2SignInWithoutBrowserFails(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	srv := e2e.NewOAuth2Server(t, e2e.OAuth2Options{ClientID: "desktop"})
	authCodeRequest(t, home, colID, srv.URL, "desktop")

	noOpener := t.TempDir()
	out, errOut, code := runQuarkWithHomeEnv(t, home,
		[]string{"PATH=" + noOpener}, "run", "API/Signed")
	assert.NotEqual(t, 0, code, "run must fail: %s", out)
	assert.Contains(t, errOut+out, "oauth2 sign-in: open browser")
	assert.Empty(t, srv.Grants())
}
