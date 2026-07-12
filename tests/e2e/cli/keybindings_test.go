//go:build e2e

// Package cli provides CLI-level end-to-end tests for the quark binary.
// Run with: go test -tags e2e ./tests/e2e/cli/...
package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// binaryPath is the path to the quark binary.
var binaryPath = func() string {
	if p := os.Getenv("QUARK_BINARY"); p != "" {
		return p
	}
	// Compute absolute path from project root (assumes tests run from repo).
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	// tests/e2e/cli/ -> project root
	root := filepath.Join(wd, "..", "..", "..")
	return filepath.Join(root, "bin", "quark")
}()

func runQuark(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	dir := t.TempDir()
	return runQuarkWithHome(t, dir, args...)
}

func runQuarkWithHome(t *testing.T, home string, args ...string) (string, string, int) {
	return runQuarkWithHomeEnv(t, home, nil, args...)
}

func runQuarkWithHomeEnv(
	t *testing.T,
	home string,
	extraEnv []string,
	args ...string,
) (string, string, int) {
	t.Helper()
	cmd := exec.Command(binaryPath, args...)
	cmd.Dir = home
	env := append(os.Environ(), "HOME="+home)
	env = append(env, extraEnv...)
	cmd.Env = env
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("failed to run quark: %v", err)
		}
	}
	return stdoutBuf.String(), stderrBuf.String(), exitCode
}

// --- keybindings list ---

func TestCLI_Keybindings_List_Defaults(t *testing.T) {
	out, _, code := runQuark(t, "keybindings", "list")
	require.Equal(t, 0, code, "exit code must be 0")
	assert.Contains(t, out, "quit")
	assert.Contains(t, out, "q")
	assert.Contains(t, out, "help")
	assert.Contains(t, out, "search")
	assert.Contains(t, out, "send_request")
	assert.Contains(t, out, "sidebar_down")
}

// --- keybindings set + list roundtrip ---

func TestCLI_Keybindings_SetAndList(t *testing.T) {
	dir := t.TempDir()

	// Set a custom binding.
	out, _, code := runQuarkWithHome(t, dir, "keybindings", "set", "quit", "Q")
	require.Equal(t, 0, code, "set must succeed: %s", out)
	assert.Contains(t, out, "quit = Q", "output must show the new binding")

	// List must show the custom binding.
	out, _, code = runQuarkWithHome(t, dir, "keybindings", "list")
	require.Equal(t, 0, code)
	assert.Contains(t, out, "quit")
	assert.Contains(t, out, "Q")
	assert.NotContains(t, out, "quit.*q", "old binding should be replaced")
}

// --- keybindings reset ---

func TestCLI_Keybindings_Reset(t *testing.T) {
	dir := t.TempDir()

	// Set a custom binding.
	_, _, code := runQuarkWithHome(t, dir, "keybindings", "set", "quit", "Q")
	require.Equal(t, 0, code)

	// Reset.
	out, _, code := runQuarkWithHome(t, dir, "keybindings", "reset")
	require.Equal(t, 0, code, "reset must succeed: %s", out)
	assert.Contains(t, out, "reset", "output must confirm reset")

	// List must show defaults again.
	out, _, code = runQuarkWithHome(t, dir, "keybindings", "list")
	require.Equal(t, 0, code)
	assert.Contains(t, out, "quit")
	assert.Contains(t, out, "q")
}

// --- keybindings set conflict detection ---

func TestCLI_Keybindings_Set_Conflict(t *testing.T) {
	// Set sidebar_down to "q" which conflicts with quit.
	_, _, code := runQuark(t, "keybindings", "set", "sidebar_down", "q")
	assert.NotEqual(t, 0, code, "set with conflict must fail")
}

// --- keybindings set invalid action ---

func TestCLI_Keybindings_Set_InvalidAction(t *testing.T) {
	_, stderr, code := runQuark(t, "keybindings", "set", "not_an_action", "x")
	assert.NotEqual(t, 0, code, "set with invalid action must fail")
	assert.Contains(t, strings.ToLower(stderr), "unknown")
}

// --- keybindings set modifier key ---

func TestCLI_Keybindings_Set_ModifierKey(t *testing.T) {
	dir := t.TempDir()

	out, _, code := runQuarkWithHome(t, dir, "keybindings", "set", "send_request", "ctrl+s")
	require.Equal(t, 0, code, "set ctrl+s must succeed: %s", out)
	assert.Contains(t, out, "send_request = ctrl+s")

	out, _, code = runQuarkWithHome(t, dir, "keybindings", "list")
	require.Equal(t, 0, code)
	assert.Contains(t, out, "ctrl+s")
}

// --- config file roundtrip ---

func TestCLI_Keybindings_ConfigRoundtrip(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".quark", "config.toml")

	// Set multiple bindings.
	_, _, code := runQuarkWithHome(t, dir, "keybindings", "set", "quit", "Q")
	require.Equal(t, 0, code)
	_, _, code = runQuarkWithHome(t, dir, "keybindings", "set", "search", "!")
	require.Equal(t, 0, code)
	_, _, code = runQuarkWithHome(t, dir, "keybindings", "set", "send_request", "ctrl+s")
	require.Equal(t, 0, code)

	// Read config file directly.
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "quit = \"Q\"")
	assert.Contains(t, content, "search = \"!\"")
	assert.Contains(t, content, "send_request = \"ctrl+s\"")

	// List via CLI must match.
	out, _, code := runQuarkWithHome(t, dir, "keybindings", "list")
	require.Equal(t, 0, code)
	assert.Contains(t, out, "quit")
	assert.Contains(t, out, "Q")
	assert.Contains(t, out, "search")
	assert.Contains(t, out, "!")
	assert.Contains(t, out, "send_request")
	assert.Contains(t, out, "ctrl+s")
}
