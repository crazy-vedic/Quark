package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	backupPrefix = "quark.db."
)

// backup creates a timestamped copy of the database file to s.backupPath.
// Backup files are named: quark.db.YYYY-MM-DD-NNN (NNN = 3-digit sequence).
// Eviction retains the last 10 backups, sorted by filename lexicographically.
// NOT by os.Stat mtime — clock skew and timezone changes corrupt mtime ordering.
func (s *Store) backup() error {
	// 0700: backup dir contains DB copies with stored credentials — owner-only.
	if err := os.MkdirAll(s.backupPath, 0o700); err != nil {
		return fmt.Errorf("backup: mkdir %q: %w", s.backupPath, err)
	}

	name, err := s.nextBackupName()
	if err != nil {
		return err
	}

	dst := filepath.Join(s.backupPath, name)
	if err := s.snapshotTo(dst); err != nil {
		return fmt.Errorf("backup: snapshot to %q: %w", dst, err)
	}

	return s.evictOldBackups()
}

// nextBackupName returns the next backup filename in the form quark.db.YYYY-MM-DD-NNN.
// The sequence is one past the highest existing sequence for today, not a count of
// files: eviction removes old files, so a count would repeat a name still on disk.
func (s *Store) nextBackupName() (string, error) {
	today := time.Now().UTC().Format("2006-01-02")
	prefix := backupPrefix + today + "-"

	entries, err := os.ReadDir(s.backupPath)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("backup: read dir: %w", err)
	}

	maxSeq := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		if n, err := strconv.Atoi(name[len(prefix):]); err == nil && n > maxSeq {
			maxSeq = n
		}
	}

	return fmt.Sprintf("%s%s-%03d", backupPrefix, today, maxSeq+1), nil
}

// evictOldBackups deletes the oldest backups (by filename) to honor the configured retention limit.
func (s *Store) evictOldBackups() error {
	entries, err := os.ReadDir(s.backupPath)
	if err != nil {
		return fmt.Errorf("backup: evict read dir: %w", err)
	}

	// Collect only our backup files.
	var names []string
	for _, e := range entries {
		if !e.IsDir() && len(e.Name()) > len(backupPrefix) &&
			e.Name()[:len(backupPrefix)] == backupPrefix {
			names = append(names, e.Name())
		}
	}

	if len(names) <= s.backupKeep {
		return nil
	}

	// Sort ascending (oldest first): by ISO 8601 date, then by sequence
	// number compared as a number. Plain text order would put "-1000" before
	// "-999" and evict the newest backup.
	sort.Slice(names, func(i, j int) bool { return backupNameLess(names[i], names[j]) })

	// Evict the oldest (first in sorted order).
	toDelete := names[:len(names)-s.backupKeep]
	for _, n := range toDelete {
		path := filepath.Join(s.backupPath, n)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("backup: evict %q: %w", path, err)
		}
	}
	return nil
}

// snapshotTo writes a consistent copy of the database to dst with
// VACUUM INTO. Copying the main file is not enough: in WAL mode recent
// commits live in the -wal file, so a plain copy can be stale or torn.
func (s *Store) snapshotTo(dst string) error {
	// Create the file first so it is owner-only from the start: backups hold
	// the full DB including stored credentials. VACUUM INTO accepts an
	// existing empty file.
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(context.Background(), `VACUUM INTO ?`, dst); err != nil {
		_ = os.Remove(dst)
		return err
	}
	// The previous io.Copy path fsynced the destination; keep that guarantee.
	if err := syncFile(dst); err != nil {
		_ = os.Remove(dst)
		return err
	}
	return nil
}

// syncFile flushes the file at path to stable storage.
func syncFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// backupNameLess orders backup filenames (quark.db.YYYY-MM-DD-N) from oldest
// to newest. Names that do not match the pattern fall back to text order.
func backupNameLess(a, b string) bool {
	dateA, seqA, okA := splitBackupName(a)
	dateB, seqB, okB := splitBackupName(b)
	if !okA || !okB {
		return a < b
	}
	if dateA != dateB {
		return dateA < dateB
	}
	return seqA < seqB
}

func splitBackupName(name string) (date string, seq int, ok bool) {
	rest := strings.TrimPrefix(name, backupPrefix)
	i := strings.LastIndex(rest, "-")
	if i < 0 {
		return "", 0, false
	}
	seq, err := strconv.Atoi(rest[i+1:])
	if err != nil {
		return "", 0, false
	}
	return rest[:i], seq, true
}
