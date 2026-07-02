package installtest

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallWarpCompletionWritesPluginWhenWarpExists(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh helper tests require a POSIX shell")
	}
	home := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(home, ".warp"), 0o755))
	quark := fakeQuark(t, "plugin-v1", 0)

	out, err := runInstallFunction(t, home, quark, "install_warp_completion")
	require.NoError(t, err, out)

	pluginPath := filepath.Join(home, ".warp", "plugins", "quark", "main.js")
	content, readErr := os.ReadFile(pluginPath)
	require.NoError(t, readErr)
	assert.Equal(t, "plugin-v1\n", string(content))
	assert.Contains(t, out, "Installed Warp completions")
	assert.Contains(t, out, "/reload-plugins or restart Warp")
}

func TestPromptWarpCompletionSkipsWhenWarpIsAbsent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh helper tests require a POSIX shell")
	}
	home := t.TempDir()
	quark := fakeQuark(t, "plugin-v1", 0)

	out, err := runInstallFunction(t, home, quark, "prompt_install_warp_completion")
	require.NoError(t, err, out)

	assert.Empty(t, out)
	assert.NoFileExists(t, filepath.Join(home, ".warp", "plugins", "quark", "main.js"))
}

func TestInstallWarpCompletionCreatesWarpPluginDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh helper tests require a POSIX shell")
	}
	home := t.TempDir()
	quark := fakeQuark(t, "plugin-v1", 0)

	out, err := runInstallFunction(t, home, quark, "install_warp_completion")
	require.NoError(t, err, out)

	pluginPath := filepath.Join(home, ".warp", "plugins", "quark", "main.js")
	content, readErr := os.ReadFile(pluginPath)
	require.NoError(t, readErr)
	assert.Equal(t, "plugin-v1\n", string(content))
}

func TestHasWarpInstalledDetectsWarpHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh helper tests require a POSIX shell")
	}
	home := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(home, ".warp"), 0o755))
	quark := fakeQuark(t, "plugin-v1", 0)

	out, err := runInstallFunction(t, home, quark, "has_warp_installed")
	require.NoError(t, err, out)
	assert.Empty(t, out)
}

func TestInstallWarpCompletionOverwritesExistingPlugin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh helper tests require a POSIX shell")
	}
	home := t.TempDir()
	pluginDir := filepath.Join(home, ".warp", "plugins", "quark")
	require.NoError(t, os.MkdirAll(pluginDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "main.js"), []byte("old\n"), 0o600))
	quark := fakeQuark(t, "new-plugin", 0)

	out, err := runInstallFunction(t, home, quark, "install_warp_completion")
	require.NoError(t, err, out)

	content, readErr := os.ReadFile(filepath.Join(pluginDir, "main.js"))
	require.NoError(t, readErr)
	assert.Equal(t, "new-plugin\n", string(content))
}

func TestInstallWarpCompletionWarnsButDoesNotFailWhenEmitterFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh helper tests require a POSIX shell")
	}
	home := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(home, ".warp"), 0o755))
	quark := fakeQuark(t, "", 9)

	out, err := runInstallFunction(t, home, quark, "install_warp_completion")
	require.NoError(t, err, out)

	assert.Contains(t, out, "Warning: failed to install Warp completions")
	assert.NoFileExists(t, filepath.Join(home, ".warp", "plugins", "quark", "main.js"))
}

func runInstallFunction(t *testing.T, home, quarkPath, functionName string) (string, error) {
	t.Helper()
	bash, err := exec.LookPath("bash")
	require.NoError(t, err)

	installPath := repoFile(t, "install.sh")
	cmd := exec.Command(bash, "-c", fmt.Sprintf("source %q; %s", installPath, functionName))
	cmd.Env = append(os.Environ(),
		"QUARK_INSTALL_LIBRARY_MODE=1",
		"HOME="+home,
		"INSTALL_PATH="+quarkPath,
		"PLATFORM=darwin-arm64",
		"BINARY=quark",
		"SHELL=/bin/zsh",
		"PATH=/usr/bin:/bin",
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	return out.String(), err
}

func fakeQuark(t *testing.T, plugin string, exitCode int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "quark")
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "__warp_completion_plugin" ]; then
  printf '%%s\n' %q
  exit %d
fi
exit 2
`, plugin, exitCode)
	require.NoError(t, os.WriteFile(path, []byte(script), 0o600))
	require.NoError(t, os.Chmod(path, 0o700))
	return path
}

func repoFile(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Join(filepath.Dir(file), "..", "..", name)
}
