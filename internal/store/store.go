package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	_ "modernc.org/sqlite" // CGO-free SQLite driver
)

// EnvDBTimeout is the context timeout for environment DB operations
// in the TUI. Set at 5 seconds to allow for slow filesystems while
// keeping the UI responsive.
const EnvDBTimeout = 5 * time.Second

// Store is the SQLite-backed repository for Quark's data.
// Construct with New; inject via CollectionLister, RequestReader, RequestWriter interfaces.
type Store struct {
	db         *sql.DB
	logger     *slog.Logger
	cacheSize  int
	backupPath string
	dbPath     string
}

// Compile-time interface compliance checks.
// If *Store or *Transaction ever stops implementing any of these, THIS FILE fails to compile.
var (
	_ CollectionLister       = (*Store)(nil)
	_ CollectionWriter       = (*Store)(nil)
	_ RequestReader          = (*Store)(nil)
	_ RequestWriter          = (*Store)(nil)
	_ EnvironmentReader      = (*Store)(nil)
	_ EnvironmentWriter      = (*Store)(nil)
	_ ActiveEnvironmentStore = (*Store)(nil)
	_ ExecutionReader        = (*Store)(nil)
	_ ExecutionWriter        = (*Store)(nil)
	_ ScheduledRunReader     = (*Store)(nil)
	_ ScheduledRunWriter     = (*Store)(nil)
	_ ScheduledRunStore      = (*Store)(nil)
	_ TransactionalWriter    = (*Transaction)(nil)
)

// New opens (or creates) the SQLite database at path, applies migrations,
// and enables WAL mode.
func New(path string, opts ...Option) (*Store, error) {
	o := defaultOptions()
	for _, opt := range opts {
		opt.apply(&o)
	}
	if o.cacheSize <= 0 {
		return nil, fmt.Errorf("store: WithCacheSize must be > 0, got %d", o.cacheSize)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: open %q: %w", path, err)
	}
	// Single connection so PRAGMA settings are not reset between pool connections.
	db.SetMaxOpenConns(1)

	s := &Store{
		db:         db,
		logger:     o.logger,
		cacheSize:  o.cacheSize,
		backupPath: o.backupPath,
		dbPath:     path,
	}

	if err := s.init(); err != nil {
		_ = db.Close()
		return nil, err
	}

	// Initialize global and default environments after migrations.
	if err := s.ensureEnvSetup(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}

	return s, nil
}

// DB returns the underlying *sql.DB for use in tests only.
// Do not call from production code outside this package.
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the underlying database connection.
func (s *Store) Close() error { return s.db.Close() }

// init enables WAL mode, enforces FK constraints, applies cache size, and runs migrations.
func (s *Store) init() error {
	// SQLite does not enforce FK constraints by default — must enable per-connection.
	if _, err := s.db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("store: enable foreign keys: %w", err)
	}
	if _, err := s.db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return fmt.Errorf("store: enable WAL: %w", err)
	}
	// Wait up to 5s on SQLITE_BUSY when another process holds the lock.
	if _, err := s.db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		return fmt.Errorf("store: set busy_timeout: %w", err)
	}
	// Negative value = number of KiB; positive = number of pages.
	// We use pages (positive) as set by WithCacheSize.
	if _, err := s.db.Exec(fmt.Sprintf("PRAGMA cache_size = %d", s.cacheSize)); err != nil {
		return fmt.Errorf("store: set cache size: %w", err)
	}
	return s.migrate()
}
