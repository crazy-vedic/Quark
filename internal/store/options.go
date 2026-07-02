package store

import (
	"log/slog"
)

type options struct {
	logger     *slog.Logger
	cacheSize  int    // SQLite page cache size; default 128 pages (~512KB)
	backupPath string // directory for auto-backups; empty = ~/.quark/backup/
}

func defaultOptions() options {
	return options{
		logger:    slog.Default(),
		cacheSize: 128,
	}
}

// Option configures a Store. Use the With* functions in this package.
// This interface is sealed: only types in this package can implement it
// (apply is unexported).
type Option interface {
	apply(*options)
}

type loggerOption struct{ l *slog.Logger }

func (o loggerOption) apply(opts *options) { opts.logger = o.l }

// WithLogger sets a custom slog.Logger on the store.
func WithLogger(l *slog.Logger) loggerOption { return loggerOption{l} }

type cacheSizeOption int

func (o cacheSizeOption) apply(opts *options) { opts.cacheSize = int(o) }

// WithCacheSize sets the SQLite page cache size. Must be > 0.
func WithCacheSize(n int) cacheSizeOption { return cacheSizeOption(n) }

type backupOption struct{ path string }

func (o backupOption) apply(opts *options) { opts.backupPath = o.path }

// WithBackup sets the directory for auto-backups.
func WithBackup(path string) backupOption { return backupOption{path} }
