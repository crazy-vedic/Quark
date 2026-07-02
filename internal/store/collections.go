package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/crazy-vedic/quark/internal/domain"
)

// SaveCollection inserts or replaces a collection record.
// Returns ErrDuplicate if another collection has the same name.
// Auto-creates a default environment for the collection if it doesn't exist.
func (s *Store) SaveCollection(ctx context.Context, c *domain.Collection) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO collections (id, name, description, meta)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   name=excluded.name,
		   description=excluded.description,
		   meta=excluded.meta,
		   updated_at=CURRENT_TIMESTAMP,
		   version=version+1`,
		c.ID, c.Name, c.Description, c.Meta,
	)
	if err != nil {
		if isSQLiteUnique(err) {
			return fmt.Errorf("store: save collection %q: %w", c.Name, ErrDuplicate)
		}
		return fmt.Errorf("store: save collection %q: %w", c.Name, err)
	}

	// Auto-create default environment for new collections.
	// Silently ignore duplicate errors (collection already had a default env).
	_, _ = s.CreateDefaultEnvironment(ctx, c.ID)

	if s.backupPath != "" {
		if berr := s.backup(); berr != nil {
			s.logger.Warn("store: backup failed", "err", berr)
		}
	}
	return nil
}

// GetCollection returns the collection with the given ID.
// Returns nil, ErrNotFound if no collection exists with that ID.
func (s *Store) GetCollection(ctx context.Context, id string) (*domain.Collection, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, description, meta, created_at, updated_at, version
		 FROM collections WHERE id = ?`, id)

	c := &domain.Collection{}
	err := row.Scan(&c.ID, &c.Name, &c.Description, &c.Meta,
		&c.CreatedAt, &c.UpdatedAt, &c.Version)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: get collection %q: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("store: get collection %q: %w", id, err)
	}
	return c, nil
}

// DeleteCollection deletes a collection and cascades to its requests.
func (s *Store) DeleteCollection(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM collections WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete collection %q: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: delete collection %q: rows affected: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("store: delete collection %q: %w", id, ErrNotFound)
	}
	return nil
}

// ListCollections returns all collections sorted alphabetically by name.
// Returns nil, nil when no collections exist (not an empty slice).
// Returns nil, err on failure — never returns partial results alongside error.
func (s *Store) ListCollections(ctx context.Context) ([]*domain.Collection, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, description, meta, created_at, updated_at, version
		 FROM collections
		 ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("store: list collections: %w", err)
	}
	defer rows.Close()

	var result []*domain.Collection
	for rows.Next() {
		c := &domain.Collection{}
		if err := rows.Scan(&c.ID, &c.Name, &c.Description, &c.Meta,
			&c.CreatedAt, &c.UpdatedAt, &c.Version); err != nil {
			return nil, fmt.Errorf("store: list collections scan: %w", err)
		}
		result = append(result, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list collections rows: %w", err)
	}

	// Return nil (not empty slice) when no results — per contract.
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

// isSQLiteUnique reports whether err is a SQLite UNIQUE constraint violation.
func isSQLiteUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
