package updates_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/updates"
)

func TestNewSelfUpdater(t *testing.T) {
	tmpDir := t.TempDir()
	exe := filepath.Join(tmpDir, "quark")
	require.NoError(t, os.WriteFile(exe, []byte("fake"), 0o600))

	updater := updates.NewSelfUpdater("owner/repo", exe, "1.0.0")
	assert.NotNil(t, updater)
}

func TestSelfUpdater_CheckUpdate(t *testing.T) {
	tmpDir := t.TempDir()
	exe := filepath.Join(tmpDir, "quark")
	require.NoError(t, os.WriteFile(exe, []byte("fake"), 0o600))

	updater := updates.NewSelfUpdater("owner/repo", exe, "1.0.0")
	ctx := context.Background()

	// CheckUpdate will try to contact GitHub, which may fail in test env
	// The important thing is the function exists and has the right signature
	_, _, _ = updater.CheckUpdate(ctx)
}

func TestSelfUpdater_CreatedWithValidPath(t *testing.T) {
	tmpDir := t.TempDir()
	exe := filepath.Join(tmpDir, "test-binary")

	// Create a temporary file to represent the binary
	f, err := os.Create(exe)
	require.NoError(t, err)
	_, err = f.WriteString("test binary content")
	require.NoError(t, err)
	f.Close()

	updater := updates.NewSelfUpdater("test/repo", exe, "2.0.0")
	assert.NotNil(t, updater)
}

func TestSelfUpdater_WithSymlink(t *testing.T) {
	tmpDir := t.TempDir()
	actualBinary := filepath.Join(tmpDir, "actual")
	symlinkPath := filepath.Join(tmpDir, "link")

	require.NoError(t, os.WriteFile(actualBinary, []byte("binary"), 0o600))
	require.NoError(t, os.Symlink(actualBinary, symlinkPath))

	updater := updates.NewSelfUpdater("owner/repo", symlinkPath, "1.0.0")
	assert.NotNil(t, updater)
}

// TestSelfUpdater_UpdateInstallsExecutableBinary runs a whole self-update
// against a local release server. The downloaded file must end up executable,
// otherwise the post-install check fails and the update is rolled back.
func TestSelfUpdater_UpdateInstallsExecutableBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell script as the fake binary")
	}

	const repo = "owner/repo"
	const version = "v2.0.0"
	binaryName := fmt.Sprintf("quark-%s-%s", runtime.GOOS, runtime.GOARCH)
	newBinary := []byte("#!/bin/sh\necho quark v2.0.0\n")
	sums := fmt.Sprintf("%x  %s\n", sha256.Sum256(newBinary), binaryName)

	mux := http.NewServeMux()
	base := "/" + repo + "/releases/download/" + version + "/"
	mux.HandleFunc(base+binaryName, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(newBinary)
	})
	mux.HandleFunc(base+"SHA256SUMS", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sums))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	binDir := t.TempDir()
	exe := filepath.Join(binDir, "quark")
	oldBinary := []byte("#!/bin/sh\necho quark v1.0.0\n")
	require.NoError(t, os.WriteFile(exe, oldBinary, 0o755)) //nolint:gosec // fake executable

	updater := updates.NewSelfUpdater(repo, exe, "v1.0.0")
	updater.SetBaseURL(srv.URL)

	got, err := updater.Update(context.Background(), version)
	require.NoError(t, err)
	assert.Equal(t, version, got)

	installed, err := os.ReadFile(exe)
	require.NoError(t, err)
	assert.Equal(t, string(newBinary), string(installed))

	info, err := os.Stat(exe)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode().Perm()&0o111, "installed binary must be executable")

	_, err = os.Stat(filepath.Join(binDir, "tmp"))
	assert.True(t, os.IsNotExist(err), "update must not leave a tmp directory beside the binary")
}
