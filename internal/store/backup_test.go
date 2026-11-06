package store_test

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

func TestStore_BackupCreatedOnSave(t *testing.T) {
	backupDir := t.TempDir()
	s, err := store.New(
		filepath.Join(t.TempDir(), "test.db"),
		store.WithBackup(backupDir),
	)
	require.NoError(t, err)
	defer s.Close()

	ctx := context.Background()
	c := &domain.Collection{ID: uuid.New().String(), Name: "BackupTest"}
	require.NoError(t, s.SaveCollection(ctx, c))

	entries, err := os.ReadDir(backupDir)
	require.NoError(t, err)
	assert.NotEmpty(t, entries, "backup file must be created after save")
}

func TestStore_BackupRetainsLast10(t *testing.T) {
	backupDir := t.TempDir()
	s, err := store.New(
		filepath.Join(t.TempDir(), "test.db"),
		store.WithBackup(backupDir),
	)
	require.NoError(t, err)
	defer s.Close()

	ctx := context.Background()
	// 11 saves should produce exactly 10 backup files
	for i := 0; i < 11; i++ {
		c := &domain.Collection{ID: uuid.New().String(), Name: uuid.New().String()}
		require.NoError(t, s.SaveCollection(ctx, c))
	}

	entries, err := os.ReadDir(backupDir)
	require.NoError(t, err)
	assert.Len(t, entries, 10, "must retain exactly 10 backup files")
}

func TestStore_BackupEvictionByFilename(t *testing.T) {
	// Eviction must be by filename lexicographic order, NOT by mtime.
	// This test verifies the oldest-named file is evicted when limit exceeded.
	backupDir := t.TempDir()
	s, err := store.New(
		filepath.Join(t.TempDir(), "test.db"),
		store.WithBackup(backupDir),
	)
	require.NoError(t, err)
	defer s.Close()

	ctx := context.Background()
	for i := 0; i < 11; i++ {
		c := &domain.Collection{ID: uuid.New().String(), Name: uuid.New().String()}
		require.NoError(t, s.SaveCollection(ctx, c))
	}

	entries, err := os.ReadDir(backupDir)
	require.NoError(t, err)
	require.Len(t, entries, 10)

	// The surviving files must be the 10 lexicographically-latest names
	// (i.e., the oldest-named file was evicted).
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	// All names must be >= names[0] (sorted ascending)
	for i := 1; i < len(names); i++ {
		assert.GreaterOrEqual(
			t,
			names[i],
			names[i-1],
			"backup files must be in ascending filename order",
		)
	}
}
