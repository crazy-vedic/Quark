//go:build e2e

// Package cli provides CLI-level end-to-end tests for quark completions.
// Run with: go test -tags e2e ./tests/e2e/cli/...
package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

func setupHomeWithCompletionFixtures(t *testing.T) (home, colID string) {
	t.Helper()
	home, colID = setupHomeWithCollection(t)

	dbPath := filepath.Join(home, ".quark", "quark.db")
	st, err := store.New(dbPath, store.WithCacheSize(100))
	require.NoError(t, err)
	defer st.Close()

	reqs := []*domain.Request{
		{
			ID:           uuid.New().String(),
			CollectionID: colID,
			Name:         "Create Payment",
			Method:       "POST",
			URL:          "https://example.test/payments",
		},
		{
			ID:           uuid.New().String(),
			CollectionID: colID,
			Name:         "List Payments",
			Method:       "GET",
			URL:          "https://example.test/payments",
		},
	}
	for _, req := range reqs {
		require.NoError(t, st.SaveRequest(context.Background(), req))
	}

	return home, colID
}

func TestCLI_Completion_RunSuggestsStoredRequestPaths(t *testing.T) {
	home, _ := setupHomeWithCompletionFixtures(t)

	out, stderr, code := runQuarkWithHome(t, home, "__complete", "run", "API/Cr")
	require.Equal(t, 0, code, "completion must succeed: %s", stderr)
	assert.Contains(t, out, "API/Create Payment\tPOST")
	assert.NotContains(t, out, "API/List Payments\tGET")
	assert.Contains(t, out, ":4")
}

func TestCLI_Completion_RunSuggestsCollectionPrefixesWithoutSpace(t *testing.T) {
	home, _ := setupHomeWithCompletionFixtures(t)

	out, stderr, code := runQuarkWithHome(t, home, "__complete", "run", "A")
	require.Equal(t, 0, code, "completion must succeed: %s", stderr)
	assert.Contains(t, out, "API/\tcollection")
	assert.Contains(t, out, ":6")
}

func TestCLI_Completion_ScheduleAddSuggestsStoredRequestPaths(t *testing.T) {
	home, _ := setupHomeWithCompletionFixtures(t)

	out, stderr, code := runQuarkWithHome(t, home, "__complete", "schedule", "add", "API/Cr")
	require.Equal(t, 0, code, "completion must succeed: %s", stderr)
	assert.Contains(t, out, "API/Create Payment\tPOST")
	assert.NotContains(t, out, "API/List Payments\tGET")
	assert.Contains(t, out, ":4")
}

func TestCLI_Completion_EnvDeleteSuggestsCollectionThenEnvironment(t *testing.T) {
	home, colID := setupHomeWithCompletionFixtures(t)

	out, stderr, code := runQuarkWithHome(t, home, "__complete", "env", "delete", "A")
	require.Equal(t, 0, code, "completion must succeed: %s", stderr)
	assert.Contains(t, out, colID+"\tAPI")
	assert.Contains(t, out, ":4")

	out, stderr, code = runQuarkWithHome(
		t,
		home,
		"__complete",
		"env",
		"delete",
		colID,
		"dev",
	)
	require.Equal(t, 0, code, "completion must succeed: %s", stderr)
	assert.Contains(t, out, "dev")
	assert.NotContains(t, out, "default")
	assert.Contains(t, out, ":4")
}

func TestCLI_Completion_RequestListCollectionFlag(t *testing.T) {
	home, colID := setupHomeWithCompletionFixtures(t)

	out, stderr, code := runQuarkWithHome(
		t,
		home,
		"__complete",
		"request",
		"list",
		"--collection",
		"A",
	)
	require.Equal(t, 0, code, "completion must succeed: %s", stderr)
	assert.Contains(t, out, colID+"\tAPI")
	assert.Contains(t, out, ":4")
}

func TestCLI_CompletionWarpPluginIsGenerated(t *testing.T) {
	out, stderr, code := runQuark(t, "__warp_completion_plugin")
	require.Equal(t, 0, code, "warp plugin generation must succeed: %s", stderr)
	assert.Contains(t, out, "export function activate(warp)")
	assert.Contains(t, out, "warp.completions.registerCommandSignature")
	assert.Contains(t, out, `"name": "quark"`)
	assert.Contains(t, out, `"name": "completion"`)
	assert.Contains(t, out, "quark __complete")
	assert.Contains(t, out, `"quarkCobraGenerator"`)
	assert.Contains(t, out, `"prefix": [`)
	assert.Contains(t, out, `"run"`)
	assert.Contains(t, out, `"schedule"`)
	assert.Contains(t, out, `"add"`)
}

func TestCLI_CompletionScriptsAreGenerated(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			out, stderr, code := runQuark(t, "completion", shell)
			require.Equal(t, 0, code, "%s completion must succeed: %s", shell, stderr)
			assert.NotEmpty(t, out)
			assert.Contains(t, out, "quark")
		})
	}
}

func TestCLI_CompletionSetupInstallsWithoutPrintingScript(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	out, stderr, code := runQuarkWithHome(t, home, "completion", "bash", "--setup")
	require.Equal(t, 0, code, "bash --setup must succeed: %s", stderr)
	assert.Empty(t, out)
	assert.Contains(t, stderr, "Enabled bash completions")

	completionFile := filepath.Join(
		home,
		".local",
		"share",
		"bash-completion",
		"completions",
		"quark",
	)
	content, err := os.ReadFile(completionFile)
	require.NoError(t, err)
	assert.Contains(t, string(content), "quark")
}

func TestCLI_CompletionSetupSubcommandDetectsShell(t *testing.T) {
	home := t.TempDir()

	out, stderr, code := runQuarkWithHomeEnv(
		t,
		home,
		[]string{"SHELL=/bin/bash"},
		"completion", "setup",
	)
	require.Equal(t, 0, code, "completion setup must succeed: %s", stderr)
	assert.Empty(t, out)
	assert.Contains(t, stderr, "Enabled bash completions")
}
