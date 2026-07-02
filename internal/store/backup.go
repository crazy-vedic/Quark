package store

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	backupKeep   = 10
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
	if err := copyFile(s.dbPath, dst); err != nil {
		return fmt.Errorf("backup: copy to %q: %w", dst, err)
	}

	return s.evictOldBackups()
}

// nextBackupName returns the next backup filename in the form quark.db.YYYY-MM-DD-NNN.
func (s *Store) nextBackupName() (string, error) {
	today := time.Now().UTC().Format("2006-01-02")
	prefix := backupPrefix + today + "-"

	entries, err := os.ReadDir(s.backupPath)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("backup: read dir: %w", err)
	}

	// Count how many backups already exist for today.
	seq := 1
	for _, e := range entries {
		if len(e.Name()) > len(prefix) && e.Name()[:len(prefix)] == prefix {
			seq++
		}
	}

	return fmt.Sprintf("%s%s-%03d", backupPrefix, today, seq), nil
}

// evictOldBackups deletes the oldest backups (by filename) to keep only backupKeep.
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

	if len(names) <= backupKeep {
		return nil
	}

	// Sort ascending (oldest first) — lexicographic order matches chronological
	// because the date prefix is ISO 8601.
	sort.Strings(names)

	// Evict the oldest (first in sorted order).
	toDelete := names[:len(names)-backupKeep]
	for _, n := range toDelete {
		path := filepath.Join(s.backupPath, n)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("backup: evict %q: %w", path, err)
		}
	}
	return nil
}

// copyFile copies src to dst using io.Copy.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	// 0600: backup files contain the full DB including stored credentials.
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
