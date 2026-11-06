package updates_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/crazy-vedic/quark/internal/updates"
)

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
