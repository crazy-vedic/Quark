//go:build e2e

// Package cli provides CLI-level end-to-end tests for quark env commands.
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

// setupHomeWithCollection creates a home directory with a quark DB at the default
// location (home/.quark/quark.db), creates a collection, and returns the home path
// and the collection ID. The DB is pre-seeded with a named env in addition to the
// auto-created default.
func setupHomeWithCollection(t *testing.T) (home, colID string) {
	t.Helper()
	home = t.TempDir()
	quarkDir := filepath.Join(home, ".quark")
	dbPath := filepath.Join(quarkDir, "quark.db")

	require.NoError(t, os.MkdirAll(quarkDir, 0o700))

	st, err := store.New(dbPath, store.WithCacheSize(100))
	require.NoError(t, err)
	defer st.Close()

	colID = uuid.New().String()
	col := &domain.Collection{ID: colID, Name: "API"}
	require.NoError(t, st.SaveCollection(context.Background(), col))

	// Seed a named environment in addition to the auto-created default.
	named := &domain.Environment{
		ID:           "env-dev",
		CollectionID: colID,
		Name:         "dev",
		Data:         `{"base_url":"http://dev.local"}`,
	}
	require.NoError(t, st.SaveEnvironment(context.Background(), named))
	return home, colID
}

// --- env list ---

func TestCLI_Env_List(t *testing.T) {
	home, colID := setupHomeWithCollection(t)

	out, _, code := runQuarkWithHome(t, home, "env", "list", colID)
	require.Equal(t, 0, code, "env list must succeed: %s", out)
	assert.Contains(t, out, "default")
	assert.Contains(t, out, "dev")
	assert.Contains(t, out, "global") // envList always shows global too
	assert.Contains(t, out, "1 vars") // dev env has 1 var (base_url)
}

func TestCLI_Env_List_InvalidCollection(t *testing.T) {
	home, _ := setupHomeWithCollection(t)

	// Invalid collection just returns an empty list (no error).
	out, _, code := runQuarkWithHome(t, home, "env", "list", "not-a-col")
	assert.Equal(t, 0, code, "env list with invalid collection returns empty list")
	assert.Contains(t, out, "global")
	assert.NotContains(t, out, "dev")
	assert.NotContains(t, out, "default")
}

func TestCLI_Env_List_NoArgs(t *testing.T) {
	home, _ := setupHomeWithCollection(t)

	// Create a second collection with an env.
	quarkDir := filepath.Join(home, ".quark")
	dbPath := filepath.Join(quarkDir, "quark.db")
	st, err := store.New(dbPath, store.WithCacheSize(100))
	require.NoError(t, err)
	col2 := &domain.Collection{ID: "col2", Name: "Web"}
	require.NoError(t, st.SaveCollection(context.Background(), col2))
	st.Close()

	out, _, code := runQuarkWithHome(t, home, "env", "list")
	require.Equal(t, 0, code, "env list without args must succeed: %s", out)
	assert.Contains(t, out, "global")
	assert.Contains(t, out, "API")
	assert.Contains(t, out, "Web")
	assert.Contains(t, out, "dev")
	assert.Contains(t, out, "default")
}

// --- env set-global ---

func TestCLI_Env_SetGlobal(t *testing.T) {
	home, colID := setupHomeWithCollection(t)

	out, _, code := runQuarkWithHome(t, home, "env", "set-global", "api_key", "secret123")
	require.Equal(t, 0, code, "env set-global must succeed: %s", out)
	assert.Contains(t, out, "api_key", "output must mention the key")

	// Verify via list: var count should have increased.
	out, _, code = runQuarkWithHome(t, home, "env", "list", colID)
	require.Equal(t, 0, code)
	assert.Contains(t, out, "global (1 vars)", "global env should have 1 var")
}

func TestCLI_Env_SetGlobal_Multiple(t *testing.T) {
	home, colID := setupHomeWithCollection(t)

	_, _, code := runQuarkWithHome(t, home, "env", "set-global", "k1", "v1")
	require.Equal(t, 0, code)
	_, _, code = runQuarkWithHome(t, home, "env", "set-global", "k2", "v2")
	require.Equal(t, 0, code)

	out, _, code := runQuarkWithHome(t, home, "env", "list", colID)
	require.Equal(t, 0, code)
	assert.Contains(t, out, "global (2 vars)", "global env should have 2 vars")
}

// --- env set ---

func TestCLI_Env_Set_CollectionDefault(t *testing.T) {
	home, colID := setupHomeWithCollection(t)

	out, _, code := runQuarkWithHome(t, home, "env", "set", colID, "default", "host", "api.local")
	require.Equal(t, 0, code, "env set must succeed: %s", out)
	assert.Contains(t, out, "host", "output must mention the key")

	// Verify via list: var count should have increased.
	out, _, code = runQuarkWithHome(t, home, "env", "list", colID)
	require.Equal(t, 0, code)
	assert.Contains(t, out, "default (1 vars)", "default env should have 1 var")
}

func TestCLI_Env_Set_NamedEnv(t *testing.T) {
	home, colID := setupHomeWithCollection(t)

	out, _, code := runQuarkWithHome(t, home, "env", "set", colID, "dev", "host", "dev.local")
	require.Equal(t, 0, code, "env set (named env) must succeed: %s", out)

	out, _, code = runQuarkWithHome(t, home, "env", "list", colID)
	require.Equal(t, 0, code)
	assert.Contains(t, out, "dev (2 vars)", "dev env should have 2 vars")
}

func TestCLI_Env_Set_InvalidCollection(t *testing.T) {
	home, _ := setupHomeWithCollection(t)

	_, _, code := runQuarkWithHome(t, home, "env", "set", "no-such-col", "default", "x", "y")
	assert.NotEqual(t, 0, code, "env set with invalid collection must fail")
}

func TestCLI_Env_Set_InvalidEnv(t *testing.T) {
	home, colID := setupHomeWithCollection(t)

	_, _, code := runQuarkWithHome(t, home, "env", "set", colID, "nonexistent", "x", "y")
	assert.NotEqual(t, 0, code, "env set with nonexistent env name must fail")
}

// --- env delete (deletes the entire environment, not a key) ---

func TestCLI_Env_Delete(t *testing.T) {
	home, colID := setupHomeWithCollection(t)

	out, _, code := runQuarkWithHome(t, home, "env", "delete", colID, "dev")
	require.Equal(t, 0, code, "env delete must succeed: %s", out)
	assert.Contains(t, out, "dev", "output must mention the deleted env")

	out, _, code = runQuarkWithHome(t, home, "env", "list", colID)
	require.Equal(t, 0, code)
	assert.NotContains(t, out, "dev")
	assert.Contains(t, out, "default")
}

func TestCLI_Env_Delete_DefaultEnv(t *testing.T) {
	home, colID := setupHomeWithCollection(t)

	_, _, code := runQuarkWithHome(t, home, "env", "delete", colID, "default")
	assert.NotEqual(t, 0, code, "deleting the default environment must fail")
}

func TestCLI_Env_Delete_InvalidEnv(t *testing.T) {
	home, colID := setupHomeWithCollection(t)

	_, _, code := runQuarkWithHome(t, home, "env", "delete", colID, "no-such-env")
	assert.NotEqual(t, 0, code, "env delete with nonexistent env name must fail")
}

// --- env active (not implemented as a separate CLI subcommand) ---

// Skipping: the active command is TUI-only; there is no CLI subcommand for it.

// --- env create ---

func TestCLI_Env_Create(t *testing.T) {
	home, colID := setupHomeWithCollection(t)

	out, _, code := runQuarkWithHome(t, home, "env", "create", colID, "staging")
	require.Equal(t, 0, code, "env create must succeed: %s", out)
	assert.Contains(t, out, "staging", "output must mention the new env name")

	out, _, code = runQuarkWithHome(t, home, "env", "list", colID)
	require.Equal(t, 0, code)
	assert.Contains(t, out, "staging")
}

func TestCLI_Env_Create_Duplicate(t *testing.T) {
	home, colID := setupHomeWithCollection(t)

	_, _, code := runQuarkWithHome(t, home, "env", "create", colID, "dev")
	assert.NotEqual(t, 0, code, "env create duplicate name must fail")
}

// --- roundtrip: create + set + list ---

func TestCLI_Env_CreateSetListRoundtrip(t *testing.T) {
	home, colID := setupHomeWithCollection(t)

	_, _, code := runQuarkWithHome(t, home, "env", "create", colID, "prod")
	require.Equal(t, 0, code)

	_, _, code = runQuarkWithHome(
		t,
		home,
		"env",
		"set",
		colID,
		"prod",
		"url",
		"https://prod.example.com",
	)
	require.Equal(t, 0, code)

	out, _, code := runQuarkWithHome(t, home, "env", "list", colID)
	require.Equal(t, 0, code)
	assert.Contains(t, out, "prod")
	assert.Contains(t, out, "prod (1 vars)")
}

// --- env set-global roundtrip ---

func TestCLI_Env_SetGlobalRoundtrip(t *testing.T) {
	home, colID := setupHomeWithCollection(t)

	_, _, code := runQuarkWithHome(t, home, "env", "set-global", "token", "abc123")
	require.Equal(t, 0, code)

	out, _, code := runQuarkWithHome(t, home, "env", "list", colID)
	require.Equal(t, 0, code)
	assert.Contains(t, out, "global (1 vars)")

	// Overwrite with new value.
	_, _, code = runQuarkWithHome(t, home, "env", "set-global", "token", "xyz789")
	require.Equal(t, 0, code)

	out, _, code = runQuarkWithHome(t, home, "env", "list", colID)
	require.Equal(t, 0, code)
	assert.Contains(t, out, "global (1 vars)")
}
