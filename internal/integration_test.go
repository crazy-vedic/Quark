//go:build integration

// Run with: go test -tags integration ./internal/...
package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/store"
)

func TestRoundTrip_CreateAndExecute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		fmt.Fprint(w, `{"integration":"ok"}`)
	}))
	defer srv.Close()

	st, err := store.New(filepath.Join(t.TempDir(), "quark.db"))
	require.NoError(t, err)
	defer st.Close()

	ctx := context.Background()

	col := &domain.Collection{ID: uuid.New().String(), Name: "Integration"}
	require.NoError(t, st.SaveCollection(ctx, col))

	req := &domain.Request{
		ID:           uuid.New().String(),
		CollectionID: col.ID,
		Name:         "Test Get",
		Method:       "GET",
		URL:          srv.URL + "/test",
	}
	require.NoError(t, st.SaveRequest(ctx, req))

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	e := exec.New(transport)
	result, err := e.Execute(ctx, req)

	require.NoError(t, err)
	assert.Equal(t, 200, result.StatusCode)
	assert.Equal(t, `{"integration":"ok"}`, string(result.Body))

	ct := http.Header(result.Headers).Get("Content-Type")
	assert.Equal(t, "application/json", ct)
}

func TestRoundTrip_CurlImportAndExecute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := r.Header.Get("X-Import-Test")
		w.WriteHeader(200)
		fmt.Fprintf(w, `{"header":"%s"}`, body)
	}))
	defer srv.Close()

	curlCmd := fmt.Sprintf(
		`curl -X GET -H "X-Import-Test: hello" %s/api`,
		srv.URL,
	)

	im := curl.NewImporter()
	parsed, err := im.Parse(strings.NewReader(curlCmd))
	require.NoError(t, err)
	assert.Equal(t, "GET", parsed.Method)
	assert.Equal(t, "hello", parsed.Headers.Get("X-Import-Test"))
	headersJSON, err := json.Marshal(parsed.Headers)
	require.NoError(t, err)

	req := &domain.Request{
		ID:      uuid.New().String(),
		Method:  parsed.Method,
		URL:     parsed.URL,
		Headers: string(headersJSON),
	}

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	e := exec.New(transport)
	result, err := e.Execute(context.Background(), req)

	require.NoError(t, err)
	assert.Equal(t, 200, result.StatusCode)
	assert.Contains(t, string(result.Body), "hello")
}

func TestRoundTrip_HeaderCaseInsensitiveAccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Deliberately set non-canonical lowercase header
		w.Header()["content-type"] = []string{"application/json"}
		w.WriteHeader(200)
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	req := &domain.Request{ID: "r1", Method: "GET", URL: srv.URL}
	e := exec.New(transport)
	result, err := e.Execute(context.Background(), req)

	require.NoError(t, err)
	// http.Header.Get must work regardless of canonical casing.
	ct := http.Header(result.Headers).Get("Content-Type")
	assert.Equal(t, "application/json", ct)
}

func TestRoundTrip_BackupIntegrity(t *testing.T) {
	backupDir := t.TempDir()
	dbDir := t.TempDir()

	st, err := store.New(
		filepath.Join(dbDir, "quark.db"),
		store.WithBackup(backupDir),
	)
	require.NoError(t, err)

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		c := &domain.Collection{ID: uuid.New().String(), Name: fmt.Sprintf("Col-%d", i)}
		require.NoError(t, st.SaveCollection(ctx, c))
	}
	require.NoError(t, st.Close())

	// Backup files must exist.
	entries, err := filepath.Glob(filepath.Join(backupDir, "quark.db.*"))
	require.NoError(t, err)
	assert.NotEmpty(t, entries, "backup files must exist after saves")
}
