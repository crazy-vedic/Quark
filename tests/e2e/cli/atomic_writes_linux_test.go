//go:build e2e && linux

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/atomicfile"
	"github.com/crazy-vedic/quark/tests/e2e/writewatch"
)

// The SQLite database is written in place by SQLite itself, by design.
const sqliteFiles = `(^|/)quark\.db(-wal|-shm|-journal)?$`

// Runtime counterpart of internal/atomicfile's static guard: run every CLI
// command that writes a file, against files that already exist, and prove
// with inotify that each file was replaced by rename and never edited in place.
func TestCLI_EveryFileWriteIsAtomic(t *testing.T) {
	home, _ := setupHomeWithCollection(t)
	configDir := filepath.Join(home, ".quark")
	configPath := filepath.Join(configDir, "config.toml")
	require.NoError(t, os.WriteFile(
		configPath, []byte("[ui]\ntheme = \"dark\"\neditor = \"nano\"\n"), 0o600,
	))
	caPath := filepath.Join(home, "root.pem")
	require.NoError(t, os.WriteFile(caPath, testCertificatePEM(t), 0o600))

	// Pre-existing rc files, one of them a dotfile-manager style symlink.
	dotfiles := filepath.Join(home, "dotfiles")
	require.NoError(t, os.MkdirAll(dotfiles, 0o700))
	realRC := filepath.Join(dotfiles, "bashrc")
	require.NoError(t, os.WriteFile(realRC, []byte("export A=1\n"), 0o600))
	require.NoError(t, os.Symlink(realRC, filepath.Join(home, ".bashrc")))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".zshrc"), []byte("export B=2\n"), 0o600))

	// Create target directories up front: inotify cannot see writes into a
	// directory made after the watch starts until it is watched itself.
	for _, dir := range []string{
		".local/share/bash-completion/completions", ".zsh/completions", ".config/fish/completions",
	} {
		require.NoError(t, os.MkdirAll(filepath.Join(home, dir), 0o700))
	}

	// Keep every shell path inside the watched home, whatever the CI env says.
	env := []string{
		"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"ZDOTDIR=" + home,
		"QUARK_THEME=",
	}
	run := func(args ...string) {
		t.Helper()
		_, stderr, code := runQuarkWithHomeEnv(t, home, env, args...)
		require.Equal(t, 0, code, "quark %v: %s", args, stderr)
	}

	steps := []struct {
		args    []string
		replace []string // files this step must replace, relative to home
	}{
		{[]string{"theme", "set", "light"}, []string{".quark/config.toml"}},
		{[]string{"keybindings", "set", "quit", "Q"}, []string{".quark/config.toml"}},
		{[]string{"keybindings", "reset"}, []string{".quark/config.toml"}},
		{
			[]string{"cert", "set", "api.example", "--ca-file", caPath},
			[]string{".quark/config.toml"},
		},
		{[]string{"cert", "delete", "api.example"}, []string{".quark/config.toml"}},
		// First run appends the completion block, second run rewrites it.
		{[]string{"completion", "setup", "--shell", "bash"}, []string{
			".local/share/bash-completion/completions/quark", "dotfiles/bashrc",
		}},
		{[]string{"completion", "setup", "--shell", "bash"}, []string{
			".local/share/bash-completion/completions/quark", "dotfiles/bashrc",
		}},
		{
			[]string{"completion", "setup", "--shell", "zsh"},
			[]string{".zsh/completions/_quark", ".zshrc"},
		},
		{
			[]string{"completion", "setup", "--shell", "fish"},
			[]string{".config/fish/completions/quark.fish"},
		},
	}
	for _, step := range steps {
		w := writewatch.Start(t, home, sqliteFiles)
		run(step.args...)
		report := w.Stop()
		assert.Empty(t, report.InPlace, "quark %v wrote files in place: %s", step.args, report)
		for _, want := range step.replace {
			assert.Contains(t, report.Replaced, want,
				"quark %v must replace %s atomically: %s", step.args, want, report)
		}
	}

	// The symlinked rc file must still be a symlink to the edited file.
	info, err := os.Lstat(filepath.Join(home, ".bashrc"))
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, ".bashrc must stay a symlink")
	raw, err := os.ReadFile(filepath.Join(dotfiles, "bashrc"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), "export A=1\n")
	assert.Contains(t, string(raw), "quark completion")
}

// Controls: the watcher must flag in-place edits and must recognise
// atomicfile's temp-and-rename, or the test above proves nothing.
func TestCLI_WriteWatchControls(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("a"), 0o600))

	w := writewatch.Start(t, home)
	require.NoError(t, os.WriteFile(path, []byte("b"), 0o600))
	report := w.Stop()
	assert.Equal(t, []string{"config.toml"}, report.InPlace, "in-place write")

	w = writewatch.Start(t, home)
	f, err := os.Create(path) // truncate, write nothing
	require.NoError(t, err)
	require.NoError(t, f.Close())
	report = w.Stop()
	assert.Equal(t, []string{"config.toml"}, report.InPlace, "truncate without write")

	w = writewatch.Start(t, home)
	require.NoError(t, atomicfile.WriteFile(path, []byte("c"), 0o600))
	report = w.Stop()
	assert.Empty(t, report.InPlace, "atomicfile write: %s", report)
	assert.Equal(t, []string{"config.toml"}, report.Replaced, "atomicfile write")
}
