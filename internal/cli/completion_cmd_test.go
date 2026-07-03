package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectShell(t *testing.T) {
	t.Setenv("SHELL", "")
	t.Setenv("FISH_VERSION", "")
	t.Setenv("ZSH_VERSION", "")

	t.Setenv("FISH_VERSION", "3.6")
	shell, err := detectShell()
	require.NoError(t, err)
	assert.Equal(t, "fish", shell)
	t.Setenv("FISH_VERSION", "")

	t.Setenv("ZSH_VERSION", "5.9")
	shell, err = detectShell()
	require.NoError(t, err)
	assert.Equal(t, "zsh", shell)
	t.Setenv("ZSH_VERSION", "")

	t.Setenv("SHELL", "/bin/bash")
	shell, err = detectShell()
	require.NoError(t, err)
	assert.Equal(t, "bash", shell)

	t.Setenv("SHELL", "/usr/bin/pwsh")
	shell, err = detectShell()
	require.NoError(t, err)
	assert.Equal(t, "powershell", shell)

	t.Setenv("SHELL", "/bin/false")
	_, err = detectShell()
	require.Error(t, err)
}

func TestAppendOrUpdateBlockCreatesAndUpdates(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, ".bashrc")
	marker := completionMarkerStart
	block := marker + "\necho quark\n" + completionMarkerEnd

	created, err := appendOrUpdateBlock(target, marker, block)
	require.NoError(t, err)
	assert.True(t, created)

	content, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Contains(t, string(content), "echo quark")

	updatedBlock := marker + "\necho quark-updated\n" + completionMarkerEnd
	created, err = appendOrUpdateBlock(target, marker, updatedBlock)
	require.NoError(t, err)
	assert.False(t, created)

	content, err = os.ReadFile(target)
	require.NoError(t, err)
	assert.Contains(t, string(content), "echo quark-updated")
	assert.NotContains(t, string(content), "echo quark\n")
}

func TestInstallShellCompletionBash(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))

	root := testCompletionRoot(t)
	var status bytes.Buffer
	require.NoError(t, installShellCompletion(root, "bash", &status))

	completionFile := filepath.Join(home, ".local", "share", "bash-completion", "completions", "quark")
	content, err := os.ReadFile(completionFile)
	require.NoError(t, err)
	assert.Contains(t, string(content), "quark")

	rcContent, err := os.ReadFile(filepath.Join(home, ".bashrc"))
	require.NoError(t, err)
	assert.Contains(t, string(rcContent), completionMarkerStart)
	assert.Contains(t, string(rcContent), completionFile)
	assert.Contains(t, status.String(), "Enabled bash completions")

	setupBin, err := resolveSetupExecutable()
	require.NoError(t, err)
	if !binaryOnPathPointsToSelf(setupBin, "quark") {
		assert.Contains(t, string(rcContent), "complete -o default -F __start_quark")
		assert.Contains(t, string(rcContent), setupBin)
		assert.Contains(t, status.String(), "Registered completions for")
	}
}

func TestInstallShellCompletionBashSkipsExtraRegistrationWhenOnPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))

	setupBin, err := resolveSetupExecutable()
	require.NoError(t, err)

	binDir := filepath.Join(home, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	require.NoError(t, os.Symlink(setupBin, filepath.Join(binDir, "quark")))
	t.Setenv("PATH", binDir)

	require.True(t, binaryOnPathPointsToSelf(setupBin, "quark"))

	root := testCompletionRoot(t)
	var status bytes.Buffer
	require.NoError(t, installShellCompletion(root, "bash", &status))

	rcContent, err := os.ReadFile(filepath.Join(home, ".bashrc"))
	require.NoError(t, err)
	assert.NotContains(t, string(rcContent), "complete -o default -F __start_quark "+setupBin)
	assert.NotContains(t, status.String(), "Registered completions for")
}

func TestBinaryOnPathPointsToSelf(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "quark-bin")
	require.NoError(t, os.WriteFile(target, []byte{}, 0o755))

	binDir := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	link := filepath.Join(binDir, "quark")
	require.NoError(t, os.Symlink(target, link))

	t.Setenv("PATH", binDir)
	assert.True(t, binaryOnPathPointsToSelf(target, "quark"))
	assert.False(t, binaryOnPathPointsToSelf(filepath.Join(dir, "other"), "quark"))
}

func TestExtraPathRegistrationBlockBash(t *testing.T) {
	block := extraPathRegistrationBlock("bash", "/tmp/quark", "quark")
	assert.Contains(t, block, "/tmp/quark")
	assert.Contains(t, block, "__start_quark")

	dir := t.TempDir()
	target := filepath.Join(dir, "quark-bin")
	require.NoError(t, os.WriteFile(target, []byte{}, 0o755))
	binDir := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	require.NoError(t, os.Symlink(target, filepath.Join(binDir, "quark")))
	t.Setenv("PATH", binDir)

	assert.Empty(t, extraPathRegistrationBlock("bash", target, "quark"))
}

func TestInstallShellCompletionFish(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	root := testCompletionRoot(t)
	var status bytes.Buffer
	require.NoError(t, installShellCompletion(root, "fish", &status))

	completionFile := filepath.Join(home, ".config", "fish", "completions", "quark.fish")
	_, err := os.Stat(completionFile)
	require.NoError(t, err)
	assert.Contains(t, status.String(), "Installed fish completions")
}

func TestShellCompletionSetupDoesNotPrintScript(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))

	root := testCompletionRoot(t)
	root.AddCommand(NewCompletionCmd(root))
	root.SetArgs([]string{"completion", "bash", "--setup"})

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	require.NoError(t, root.Execute())

	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "Enabled bash completions")
}

func TestCompletionCmdShowsHelpWithoutArgs(t *testing.T) {
	root := testCompletionRoot(t)
	root.AddCommand(NewCompletionCmd(root))
	root.SetArgs([]string{"completion"})

	var stdout bytes.Buffer
	root.SetOut(&stdout)
	require.NoError(t, root.Execute())
	assert.Contains(t, stdout.String(), "Quick start")
	assert.Contains(t, stdout.String(), "completion setup")
}

func testCompletionRoot(t *testing.T) *cobra.Command {
	t.Helper()
	return &cobra.Command{
		Use:   "quark",
		Short: "test root",
	}
}

func TestCompletionPathsBashUsesXDGDataHome(t *testing.T) {
	home := t.TempDir()
	paths, err := completionPaths("bash", home, "quark")
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(paths.completionFile, filepath.Join("bash-completion", "completions", "quark")))
}
