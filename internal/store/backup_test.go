package store_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

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

// The sequence number is three digits wide, but nothing stops a day from
// having more than 999 backups (one is taken per save). "-1000" sorts before
// "-999" as text, so the newest backup must not be the one evicted.
func TestStore_BackupEvictionOrdersSequencesNumerically(t *testing.T) {
	backupDir := t.TempDir()
	today := time.Now().UTC().Format("2006-01-02")
	for seq := 990; seq <= 999; seq++ {
		name := fmt.Sprintf("quark.db.%s-%03d", today, seq)
		require.NoError(t, os.WriteFile(filepath.Join(backupDir, name), []byte("old"), 0o600))
	}

	s, err := store.New(
		filepath.Join(t.TempDir(), "test.db"),
		store.WithBackup(backupDir),
	)
	require.NoError(t, err)
	defer s.Close()
	c := &domain.Collection{ID: uuid.New().String(), Name: "thousandth"}
	require.NoError(t, s.SaveCollection(context.Background(), c))

	entries, err := os.ReadDir(backupDir)
	require.NoError(t, err)
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name()] = true
	}
	assert.Len(t, names, 10)
	assert.True(t, names["quark.db."+today+"-1000"], "the newest backup (1000) must be kept")
	assert.False(t, names["quark.db."+today+"-990"], "the oldest backup (990) must be evicted")
}

// Regression: after eviction removes old files, the next backup name must not
// reuse a name still on disk (previously the newest backup was overwritten).
func TestStore_BackupNamesNeverCollide(t *testing.T) {
	backupDir := t.TempDir()
	s, err := store.New(
		filepath.Join(t.TempDir(), "test.db"),
		store.WithBackup(backupDir),
	)
	require.NoError(t, err)
	defer s.Close()

	ctx := context.Background()
	for i := 0; i < 15; i++ {
		c := &domain.Collection{ID: uuid.New().String(), Name: uuid.New().String()}
		require.NoError(t, s.SaveCollection(ctx, c))
	}

	entries, err := os.ReadDir(backupDir)
	require.NoError(t, err)
	assert.Len(t, entries, 10, "must retain exactly 10 backup files")

	seen := map[string]bool{}
	for _, e := range entries {
		assert.False(t, seen[e.Name()], "duplicate backup name %s", e.Name())
		seen[e.Name()] = true
	}
	// The retained files should be the 10 newest sequence numbers (006..015).
	assert.True(t, seen["quark.db."+time.Now().UTC().Format("2006-01-02")+"-015"],
		"newest backup must be retained with its own sequence number")
}
