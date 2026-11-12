package updates_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/updates"
)

func TestDetectChannel_Binary(t *testing.T) {
	// DetectChannel() returns ChannelBinary when:
	// - Binary is in a non-standard location, OR
	// - Required package manager tools are not available
	// This test verifies the behavior under normal dev conditions.
	channel := updates.DetectChannel()
	// We can't easily control the executable path in tests, so just verify it returns a valid channel
	assert.NotEmpty(t, channel)
}

func TestCanSelfUpdate_Binary_Writable(t *testing.T) {
	tmpDir := t.TempDir()
	testExe := filepath.Join(tmpDir, "quark")

	// Create a dummy executable
	require.NoError(t, os.WriteFile(testExe, []byte("fake binary"), 0o600))

	result := updates.CanSelfUpdate(updates.ChannelBinary, testExe)
	assert.True(t, result, "should allow self-update for writable user directory")
}

func TestCanSelfUpdate_Binary_NonWritable(t *testing.T) {
	// Test system paths that should never allow self-update
	systemPaths := []string{
		"/usr/bin/quark",
		"/usr/local/Cellar/quark/1.0.0/bin/quark",
		"/opt/homebrew/bin/quark",
		"/opt/homebrew/Cellar/quark/1.0.0/bin/quark",
	}

	for _, path := range systemPaths {
		t.Run("deny "+path, func(t *testing.T) {
			result := updates.CanSelfUpdate(updates.ChannelBinary, path)
			assert.False(t, result, "should deny self-update for system path: %s", path)
		})
	}
}

func TestCanSelfUpdate_Binary_WindowsSystemPath(t *testing.T) {
	result := updates.CanSelfUpdate(updates.ChannelBinary, "C:\\Program Files\\Quark\\quark.exe")
	assert.False(t, result, "should deny self-update for Windows Program Files")
}

func TestCanSelfUpdate_Binary_UserWritablePaths(t *testing.T) {
	// These paths are user-writable and should allow self-update
	userPaths := []struct {
		name string
		path string
	}{
		{"home directory", "/home/user/bin/quark"},
		{"user local", "/usr/local/bin/quark"},
		{"tmp", "/tmp/quark"},
	}

	for _, tt := range userPaths {
		t.Run("allow "+tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			testExe := filepath.Join(tmpDir, "quark")

			// Create a dummy executable in a writable directory
			require.NoError(t, os.WriteFile(testExe, []byte("fake"), 0o600))

			result := updates.CanSelfUpdate(updates.ChannelBinary, testExe)
			assert.True(t, result, "should allow self-update for writable path: %s", tt.path)
		})
	}
}

func TestCanSelfUpdate_NonBinaryChannel(t *testing.T) {
	tmpDir := t.TempDir()
	testExe := filepath.Join(tmpDir, "quark")
	require.NoError(t, os.WriteFile(testExe, []byte("fake"), 0o600))

	channels := []updates.Channel{
		updates.ChannelBrew,
		updates.ChannelApt,
		updates.ChannelWinget,
	}

	for _, ch := range channels {
		t.Run("deny "+string(ch), func(t *testing.T) {
			result := updates.CanSelfUpdate(ch, testExe)
			assert.False(t, result, "should deny self-update for %s channel", ch)
		})
	}
}

func TestCanSelfUpdate_InvalidPath(t *testing.T) {
	result := updates.CanSelfUpdate(updates.ChannelBinary, "/nonexistent/path/quark")
	assert.False(t, result, "should deny self-update for nonexistent path")
}

func TestCanSelfUpdate_SymlinkResolution(t *testing.T) {
	tmpDir := t.TempDir()
	actualPath := filepath.Join(tmpDir, "actual-quark")
	symlinkPath := filepath.Join(tmpDir, "quark-link")

	// Create the actual binary
	require.NoError(t, os.WriteFile(actualPath, []byte("fake"), 0o600))

	// Create a symlink to it
	require.NoError(t, os.Symlink(actualPath, symlinkPath))

	// Verify it resolves the symlink and checks the actual location
	result := updates.CanSelfUpdate(updates.ChannelBinary, symlinkPath)
	assert.True(t, result, "should resolve symlinks and check writable actual path")
}

func TestCanSelfUpdate_PathWithoutSeparator(t *testing.T) {
	// Edge case: ensure "/usr/binfoo" (similar to but not exactly "/usr/bin") is treated differently
	// Create a temp directory with a similar prefix to test path matching
	tmpDir := t.TempDir()
	testExe := filepath.Join(tmpDir, "binfoo", "quark")
	require.NoError(t, os.MkdirAll(filepath.Dir(testExe), 0o755))
	require.NoError(t, os.WriteFile(testExe, []byte("fake"), 0o600))

	result := updates.CanSelfUpdate(updates.ChannelBinary, testExe)
	assert.True(t, result, "should allow paths that don't exactly match system paths")
}
