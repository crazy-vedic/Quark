//go:build e2e

package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

// newRunFlagsServer answers every request with body and echoes the request path
// as the body when echoPath is set.
func newRunFlagsServer(t *testing.T, contentType string, body []byte, echoPath bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("X-Run-Flag", "seen")
		w.WriteHeader(http.StatusOK)
		if echoPath {
			_, _ = w.Write([]byte(r.URL.Path))
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCLI_RunOutputFileWritesBodyAndKeepsStdoutEmpty(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	body := []byte(`{"ok":true,"items":[1,2,3]}`)
	srv := newRunFlagsServer(t, "application/json", body, false)
	seedRequest(t, home, colID, &domain.Request{
		Name: "Fetch", Method: http.MethodGet, URL: srv.URL + "/file",
	})
	outPath := filepath.Join(t.TempDir(), "body.json")

	stdout, stderr, code := runQuarkWithHome(t, home, "run", "API/Fetch", "-o", outPath)

	require.Equal(t, 0, code, "run -o must succeed: %s", stderr)
	assert.Empty(t, stdout, "with -o the body must not reach stdout")
	assert.Contains(t, stderr, "Status: 200 OK", "the status block stays on stderr")
	got, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Equal(t, body, got, "the file holds the body exactly as received")
}

func TestCLI_RunOutputFileStreamsLargeBodyFromTempFile(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	body := bytes.Repeat([]byte("p"), 11<<20)
	srv := newRunFlagsServer(t, "application/octet-stream", body, false)
	seedRequest(t, home, colID, &domain.Request{
		Name: "Fetch", Method: http.MethodGet, URL: srv.URL + "/big",
	})
	outPath := filepath.Join(t.TempDir(), "big.bin")

	stdout, stderr, code := runQuarkWithHome(t, home, "run", "API/Fetch", "-o", outPath)

	require.Equal(t, 0, code, "run -o must succeed: %s", stderr)
	assert.Empty(t, stdout)
	got, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(body, got), "the 11 MiB streamed body must be copied byte for byte")
}

func TestCLI_RunIncludeHeadersPrintsStatusAndHeadersBeforeBody(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	srv := newRunFlagsServer(t, "text/plain", []byte("hello flags"), false)
	seedRequest(t, home, colID, &domain.Request{
		Name: "Fetch", Method: http.MethodGet, URL: srv.URL + "/text",
	})

	stdout, stderr, code := runQuarkWithHome(t, home, "run", "API/Fetch", "-i")

	require.Equal(t, 0, code, "run -i must succeed: %s", stderr)
	assert.True(t, strings.HasPrefix(stdout, "200 OK\n"), "stdout starts with the status line, got %q", stdout)
	assert.Contains(t, stdout, "Content-Type: text/plain\n")
	assert.Contains(t, stdout, "X-Run-Flag: seen\n")
	assert.Contains(t, stdout, "\n\nhello flags", "a blank line separates the headers from the body")
	assert.NotContains(t, stderr, "Content-Type:", "headers belong on stdout with -i")
}

func TestCLI_RunEnvUsesNamedEnvironmentForOneRunOnly(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	srv := newRunFlagsServer(t, "text/plain", nil, true)
	// The default environment is active and points nowhere reachable; the
	// named "dev" environment points at the test server.
	setEnvVars(t, home, colID, map[string]string{"base_url": "http://127.0.0.1:1"}, nil)
	setEnvironmentVarsByName(t, home, colID, "dev", map[string]string{"base_url": srv.URL})
	seedRequest(t, home, colID, &domain.Request{
		Name: "Fetch", Method: http.MethodGet, URL: "{{base_url}}/fetch",
	})
	activeBefore := activeEnvironmentID(t, home, colID)

	stdout, stderr, code := runQuarkWithHome(t, home, "run", "API/Fetch", "--env", "dev")

	require.Equal(t, 0, code, "--env dev must send to the dev base_url: %s", stderr)
	assert.Equal(t, "/fetch\n", stdout, "the echoed path is printed with a newline")
	assert.Equal(t, activeBefore, activeEnvironmentID(t, home, colID),
		"--env must not change the stored active environment")
}

func activeEnvironmentID(t *testing.T, home, colID string) string {
	t.Helper()
	st, err := store.New(filepath.Join(home, ".quark", "quark.db"), store.WithCacheSize(100))
	require.NoError(t, err)
	defer st.Close()
	id, err := st.GetActiveEnvironment(context.Background(), colID)
	require.NoError(t, err)
	return id
}

func TestCLI_RunEnvWithoutFlagUsesActiveEnvironment(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	srv := newRunFlagsServer(t, "text/plain", nil, true)
	setEnvVars(t, home, colID, map[string]string{"base_url": "http://127.0.0.1:1"}, nil)
	setEnvironmentVarsByName(t, home, colID, "dev", map[string]string{"base_url": srv.URL})
	seedRequest(t, home, colID, &domain.Request{
		Name: "Fetch", Method: http.MethodGet, URL: "{{base_url}}/fetch",
	})

	_, _, code := runQuarkWithHome(t, home, "run", "API/Fetch")

	assert.NotEqual(t, 0, code, "without --env the active environment's unreachable base_url is used")
}

func TestCLI_RunEnvUnknownNameFailsBeforeSending(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	srv := newRunFlagsServer(t, "text/plain", nil, true)
	seedRequest(t, home, colID, &domain.Request{
		Name: "Fetch", Method: http.MethodGet, URL: srv.URL + "/fetch",
	})

	stdout, stderr, code := runQuarkWithHome(t, home, "run", "API/Fetch", "--env", "nosuch")

	assert.NotEqual(t, 0, code)
	assert.Contains(t, stderr, `environment "nosuch" not found in this collection or its parents`)
	assert.Empty(t, stdout, "no request is sent when the environment is missing")
}

// setEnvironmentVarsByName replaces the variables of the named environment in
// the collection, leaving its other settings alone.
func setEnvironmentVarsByName(t *testing.T, home, colID, name string, vars map[string]string) {
	t.Helper()
	st, err := store.New(filepath.Join(home, ".quark", "quark.db"), store.WithCacheSize(100))
	require.NoError(t, err)
	defer st.Close()
	ctx := context.Background()
	envs, err := st.ListCollectionEnvironments(ctx, colID)
	require.NoError(t, err)
	for _, env := range envs {
		if env.Name == name {
			env.SetVars(vars)
			require.NoError(t, st.SaveEnvironment(ctx, env))
			return
		}
	}
	t.Fatalf("environment %q not found in collection %s", name, colID)
}
