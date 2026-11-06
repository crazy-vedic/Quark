package store

import (
	"log/slog"
)

type options struct {
	logger     *slog.Logger
	cacheSize  int    // SQLite page cache size; default 128 pages (~512KB)
	backupPath string // directory for auto-backups; empty = ~/.quark/backup/
	backupKeep int    // maximum number of backups to retain
}

func defaultOptions() options {
	return options{
		logger:     slog.Default(),
		cacheSize:  128,
		backupKeep: 10,
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

type backupOption struct {
	path string
	keep int
}

func (o backupOption) apply(opts *options) {
	opts.backupPath = o.path
	if o.keep >= 0 {
		opts.backupKeep = o.keep
	}
}

// WithBackup sets the directory and optional retention limit for auto-backups.
// Omitting keepLast preserves the default of 10. A limit of zero retains none.
func WithBackup(path string, keepLast ...int) backupOption {
	keep := -1
	if len(keepLast) > 0 {
		keep = keepLast[0]
	}
	return backupOption{path: path, keep: keep}
}
