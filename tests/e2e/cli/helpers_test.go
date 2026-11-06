//go:build e2e

package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
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
