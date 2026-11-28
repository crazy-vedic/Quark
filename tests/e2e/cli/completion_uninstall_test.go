//go:build e2e

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// completionShellEnv pins the shell-detection inputs so the command under test
// does not depend on the shell that runs the suite. Empty values count as unset.
func completionShellEnv(shell string) []string {
	return []string{
		"SHELL=" + shell,
		"ZSH_VERSION=",
		"FISH_VERSION=",
		"ZDOTDIR=",
	}
}

func TestCLI_CompletionUninstallRemovesZshSetupAndKeepsUserRC(t *testing.T) {
	home := t.TempDir()
	rcPath := filepath.Join(home, ".zshrc")
	const userRC = "export EDITOR=vim\n"
	require.NoError(t, os.WriteFile(rcPath, []byte(userRC), 0o600))
	scriptPath := filepath.Join(home, ".zsh", "completions", "_quark")

	_, setupErr, setupCode := runQuarkWithHomeEnv(t, home, completionShellEnv("/bin/zsh"),
		"completion", "zsh", "--setup")
	require.Equal(t, 0, setupCode, "zsh --setup must succeed: %s", setupErr)
	require.FileExists(t, scriptPath)
	installedRC, err := os.ReadFile(rcPath)
	require.NoError(t, err)
	require.Contains(t, string(installedRC), "# >>> quark completion >>>")

	// Per-host zsh dumps are removed with the script; other hosts' dumps stay.
	host, err := os.Hostname()
	require.NoError(t, err)
	host, _, _ = strings.Cut(host, ".")
	ownDump := filepath.Join(home, ".zcompdump-"+host+"-5.9")
	otherDump := filepath.Join(home, ".zcompdump-other-host-5.9")
	require.NoError(t, os.WriteFile(ownDump, []byte("dump"), 0o600))
	require.NoError(t, os.WriteFile(otherDump, []byte("dump"), 0o600))

	out, stderr, code := runQuarkWithHomeEnv(t, home, completionShellEnv("/bin/zsh"),
		"completion", "uninstall", "zsh")

	require.Equal(t, 0, code, "uninstall must succeed: %s", stderr)
	assert.Empty(t, out, "status goes to stderr, not stdout")
	assert.Contains(t, stderr, "Removed zsh completions")
	assert.Contains(t, stderr, "Deleted "+scriptPath)
	assert.Contains(t, stderr, "Removed the completion block from "+rcPath)

	assert.NoFileExists(t, scriptPath)
	assert.NoFileExists(t, ownDump, "the host's own zsh dump must be cleared")
	assert.FileExists(t, otherDump, "dumps from other hosts must be kept")

	got, err := os.ReadFile(rcPath)
	require.NoError(t, err)
	assert.Equal(t, userRC, string(got), "only the marked block is removed")
}

func TestCLI_CompletionUninstallBashWithoutArgumentDetectsShell(t *testing.T) {
	home := t.TempDir()
	bashFile := filepath.Join(home, ".local", "share", "bash-completion", "completions", "quark")

	_, setupErr, setupCode := runQuarkWithHomeEnv(t, home, completionShellEnv("/bin/bash"),
		"completion", "bash", "--setup")
	require.Equal(t, 0, setupCode, "bash --setup must succeed: %s", setupErr)
	require.FileExists(t, bashFile)

	_, stderr, code := runQuarkWithHomeEnv(t, home, completionShellEnv("/bin/bash"),
		"completion", "uninstall")

	require.Equal(t, 0, code, "uninstall must detect bash from SHELL: %s", stderr)
	assert.Contains(t, stderr, "Removed bash completions")
	assert.NoFileExists(t, bashFile)
	rc, err := os.ReadFile(filepath.Join(home, ".bashrc"))
	require.NoError(t, err)
	assert.NotContains(t, string(rc), "# >>> quark completion >>>")
}

func TestCLI_CompletionUninstallReportsNothingInstalled(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		want   string
		shells string
	}{
		{"bash without rc file", []string{"bash"}, "No bash completions installed", "/bin/bash"},
		{"zsh without rc file", []string{"zsh"}, "No zsh completions installed", "/bin/zsh"},
		{"pwsh is an alias for powershell", []string{"pwsh"}, "No powershell completions installed", "/bin/bash"},
		{"powershell name", []string{"powershell"}, "No powershell completions installed", "/bin/bash"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			args := append([]string{"completion", "uninstall"}, tc.args...)

			out, stderr, code := runQuarkWithHomeEnv(t, home, completionShellEnv(tc.shells), args...)

			require.Equal(t, 0, code, "uninstall of a missing setup is not an error: %s", stderr)
			assert.Empty(t, out)
			assert.Contains(t, stderr, tc.want)
		})
	}
}

func TestCLI_CompletionUninstallFailsWhenShellCannotBeDetected(t *testing.T) {
	home := t.TempDir()

	_, stderr, code := runQuarkWithHomeEnv(t, home, completionShellEnv(""), "completion", "uninstall")

	assert.NotEqual(t, 0, code)
	assert.Contains(t, stderr, "could not detect shell")
}

func TestCLI_CompletionUninstallRejectsUnknownShell(t *testing.T) {
	home := t.TempDir()

	_, stderr, code := runQuarkWithHomeEnv(t, home, completionShellEnv("/bin/bash"),
		"completion", "uninstall", "tcsh")

	assert.NotEqual(t, 0, code)
	assert.NotEmpty(t, stderr)
}
