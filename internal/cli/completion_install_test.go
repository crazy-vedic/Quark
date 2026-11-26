package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallShellCompletionZshOnlyExtendsFpath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", "")

	var status bytes.Buffer
	require.NoError(t, installShellCompletion(testCompletionRoot(t), shellZsh, &status))

	completionDir := filepath.Join(home, ".zsh", "completions")
	_, err := os.Stat(filepath.Join(completionDir, "_quark"))
	require.NoError(t, err)

	rc := string(mustReadFile(t, filepath.Join(home, ".zshrc")))
	assert.Contains(t, rc, "fpath=(")
	assert.Contains(t, rc, completionDir)
	assert.NotContains(t, rc, "compinit")
}

func TestInstallShellCompletionZshRemovesOnlyThisHostsCompdump(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", "")
	host := shortHostname()
	require.NotEmpty(t, host)

	keep := []string{".zcompdump-other-host-5.9", ".zshrc.zcompdump-backup"}
	remove := []string{".zcompdump", ".zcompdump-" + host + "-5.9"}
	for _, name := range append(append([]string{}, keep...), remove...) {
		require.NoError(t, os.WriteFile(filepath.Join(home, name), []byte("x"), 0o600))
	}

	require.NoError(t, installShellCompletion(testCompletionRoot(t), shellZsh, &bytes.Buffer{}))

	for _, name := range keep {
		_, err := os.Stat(filepath.Join(home, name))
		assert.NoError(t, err, "%s should be kept", name)
	}
	for _, name := range remove {
		_, err := os.Stat(filepath.Join(home, name))
		assert.True(t, os.IsNotExist(err), "%s should be removed", name)
	}
}

func TestUninstallShellCompletionRemovesBlockAndScript(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))

	rcFile := filepath.Join(home, ".bashrc")
	require.NoError(t, os.WriteFile(rcFile, []byte("export A=1\n"), 0o600))
	root := testCompletionRoot(t)
	require.NoError(t, installShellCompletion(root, shellBash, &bytes.Buffer{}))
	require.NoError(t, os.WriteFile(
		rcFile,
		append(mustReadFile(t, rcFile), []byte("export B=2\n")...),
		0o600,
	))

	var status bytes.Buffer
	require.NoError(t, uninstallShellCompletion(root, shellBash, &status))

	rc := string(mustReadFile(t, rcFile))
	assert.NotContains(t, rc, completionMarkerStart)
	assert.NotContains(t, rc, completionMarkerEnd)
	assert.Equal(t, "export A=1\nexport B=2\n", rc)

	paths, err := completionPaths(shellBash, home, "quark")
	require.NoError(t, err)
	_, err = os.Stat(paths.completionFile)
	assert.True(t, os.IsNotExist(err))
	assert.Contains(t, status.String(), "Removed bash completions")
}

func TestUninstallShellCompletionWhenNothingInstalled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	var status bytes.Buffer
	require.NoError(t, uninstallShellCompletion(testCompletionRoot(t), shellFish, &status))
	assert.Contains(t, status.String(), "No fish completions installed")
}

func TestCompletionUninstallCommandTakesShellArgument(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", "")

	root := testCompletionRoot(t)
	require.NoError(t, installShellCompletion(root, shellZsh, &bytes.Buffer{}))
	root.AddCommand(NewCompletionCmd(root))
	root.SetArgs([]string{"completion", "uninstall", "zsh"})
	var stderr bytes.Buffer
	root.SetErr(&stderr)
	require.NoError(t, root.Execute())

	rc := string(mustReadFile(t, filepath.Join(home, ".zshrc")))
	assert.NotContains(t, rc, completionMarkerStart)
	_, err := os.Stat(filepath.Join(home, ".zsh", "completions", "_quark"))
	assert.True(t, os.IsNotExist(err))
	assert.Contains(t, stderr.String(), "Removed zsh completions")
}

func TestCompletionUninstallCommandDetectsShell(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("FISH_VERSION", "3.7")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	root := testCompletionRoot(t)
	root.AddCommand(NewCompletionCmd(root))
	root.SetArgs([]string{"completion", "uninstall"})
	var stderr bytes.Buffer
	root.SetErr(&stderr)
	require.NoError(t, root.Execute())
	assert.Contains(t, stderr.String(), "No fish completions installed")
}

func TestCompletionUninstallCommandRejectsUnknownShell(t *testing.T) {
	root := testCompletionRoot(t)
	root.AddCommand(NewCompletionCmd(root))
	root.SetArgs([]string{"completion", "uninstall", "tcsh"})
	root.SetErr(&bytes.Buffer{})
	root.SetOut(&bytes.Buffer{})
	require.Error(t, root.Execute())
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}
