//go:build e2e

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Backups are numbered per day and the number is not capped at three digits.
// Eviction must compare the number as a number: "-1000" is newer than "-999".
func TestCLI_BackupEvictionKeepsNewestBeyondSequence999(t *testing.T) {
	home, _ := setupHomeWithCollection(t)
	backupDir := filepath.Join(home, ".quark", "backup")
	require.NoError(t, os.MkdirAll(backupDir, 0o700))

	today := time.Now().UTC().Format("2006-01-02")
	name := func(seq int) string { return fmt.Sprintf("quark.db.%s-%d", today, seq) }

	// Ten existing backups, the default retention: sequences 991 to 1000.
	for seq := 991; seq <= 1000; seq++ {
		require.NoError(t, os.WriteFile(filepath.Join(backupDir, name(seq)), []byte("old"), 0o600))
	}

	// Creating a collection takes backup 1001 and evicts exactly one backup.
	out, errOut, code := runQuarkWithHome(t, home, "collection", "create", "Second")
	require.Equal(t, 0, code, "collection create failed: %s %s", out, errOut)

	assert.NoFileExists(t, filepath.Join(backupDir, name(991)), "the oldest backup is evicted")
	for _, seq := range []int{992, 999, 1000, 1001} {
		assert.FileExists(t, filepath.Join(backupDir, name(seq)), "backup %d must be kept", seq)
	}
	entries, err := os.ReadDir(backupDir)
	require.NoError(t, err)
	assert.Len(t, entries, 10)
}
