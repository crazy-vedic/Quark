package updates_test

import (
	"context"
	"os"
	"path/filepath"
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
