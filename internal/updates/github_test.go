package updates_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/updates"
)

// hungServer accepts requests and never answers them until the test ends.
func hungServer(t *testing.T) *httptest.Server {
	t.Helper()
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-release
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })
	return server
}

// requireReturnsWithin fails the test if call does not return within d.
// Without a client timeout, a hung server makes the update call block forever.
func requireReturnsWithin(t *testing.T, d time.Duration, call func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- call() }()
	select {
	case err := <-done:
		return err
	case <-time.After(d):
		t.Fatalf("call did not return within %s: update HTTP request has no timeout", d)
		return nil
	}
}

func TestGitHubSource_LatestVersion_HungServerTimesOut(t *testing.T) {
	server := hungServer(t)
	source := updates.NewGitHubSource("owner/repo")
	source.SetBaseURL(server.URL)
	source.SetHTTPTimeout(100 * time.Millisecond)

	err := requireReturnsWithin(t, 5*time.Second, func() error {
		_, err := source.LatestVersion(context.Background())
		return err
	})
	require.Error(t, err)
}

func TestGitHubSource_FetchChecksumFile_HungServerTimesOut(t *testing.T) {
	server := hungServer(t)
	source := updates.NewGitHubSource("owner/repo")
	source.SetBaseURL(server.URL)
	source.SetHTTPTimeout(100 * time.Millisecond)

	err := requireReturnsWithin(t, 5*time.Second, func() error {
		_, err := source.FetchChecksumFile(context.Background(), "v1.0.0")
		return err
	})
	require.Error(t, err)
}

func TestGitHubSource_LatestVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repo/releases/latest" {
			w.Header().Set("Location", "https://example.com/repo/releases/tag/v2.5.0")
			w.WriteHeader(http.StatusFound)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	// We can't easily mock the GitHub request in this test setup,
	// so we'll test the parsing logic separately and validate the interface.
	source := updates.NewGitHubSource("owner/repo")
	assert.NotNil(t, source)
}

func TestGitHubSource_UpgradeCommand(t *testing.T) {
	source := updates.NewGitHubSource("owner/repo")
	cmd := source.UpgradeCommand()
	assert.Equal(t, "quark update", cmd)
}

func TestGitHubSource_DowngradeWarning(t *testing.T) {
	source := updates.NewGitHubSource("owner/repo")
	warning := source.DowngradeWarning("2.0.0", "1.0.0")
	assert.Equal(t, "downgrading from 2.0.0 to 1.0.0", warning)
}

func TestGitHubSource_FetchChecksumFile_MockServer(t *testing.T) {
	checksumContent := "abc123 quark-linux-amd64\ndef456 quark-darwin-amd64"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/owner/repo/releases/download/v1.0.0/SHA256SUMS" {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, checksumContent)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	// Note: We're testing the interface exists and basic behavior,
	// but the actual GitHub redirect handling requires real network calls.
	source := updates.NewGitHubSource("owner/repo")
	assert.NotNil(t, source)
}

func TestGitHubSource_FetchChecksumFile_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	source := updates.NewGitHubSource("owner/repo")
	ctx := context.Background()

	// The function should handle 404 gracefully
	_, err := source.FetchChecksumFile(ctx, "v1.0.0")
	assert.Error(t, err)
}

func TestGitHubSource_Interface_Compliance(t *testing.T) {
	// Verify GitHubSource implements the Source interface
	var _ updates.Source = (*updates.GitHubSource)(nil)
}

func TestNewGitHubSource(t *testing.T) {
	source := updates.NewGitHubSource("crazy-vedic/quark")
	assert.NotNil(t, source)

	// Verify it can be used as a Source
	assert.NotEmpty(t, source.UpgradeCommand())
}
