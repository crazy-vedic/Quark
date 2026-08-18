package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/crazy-vedic/quark/internal/domain"
)

// NormalizeName applies Quark's name rules. Slash is a hierarchy separator and
// is therefore repaired before a name is persisted.
func NormalizeName(name string) (string, bool) {
	repaired := strings.ReplaceAll(name, "/", "-")
	return repaired, repaired != name
}

// SaveCollection inserts or replaces a collection record.
// Returns ErrDuplicate if another collection has the same name.
// Auto-creates a default environment for the collection if it doesn't exist.
func (s *Store) SaveCollection(ctx context.Context, c *domain.Collection) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	var repaired bool
	c.Name, repaired = NormalizeName(c.Name)
	if c.ParentID == c.ID && c.ParentID != "" {
		return fmt.Errorf("store: collection cannot parent itself")
	}
	// Names are unique only among siblings. Repair collisions deterministically
	// so imports and bulk writes remain repeatable.
	base := c.Name
	for suffix := 1; repaired; suffix++ {
		var n int
		parentClause := "parent_id IS NULL"
		if c.ParentID != "" {
			parentClause = "parent_id = ?"
		}
		q := `SELECT COUNT(*) FROM collections WHERE name = ? AND id <> ? AND ` + parentClause
		var err error
		if c.ParentID == "" {
			err = s.db.QueryRowContext(ctx, q, c.Name, c.ID).Scan(&n)
		} else {
			err = s.db.QueryRowContext(ctx, q, c.Name, c.ID, c.ParentID).Scan(&n)
		}
		if err != nil {
			return fmt.Errorf("store: check collection name: %w", err)
		}
		if n == 0 {
			break
		}
		suffixNum := suffix + 1
		c.Name = fmt.Sprintf("%s-%d", base, suffixNum)
	}
	parent := sql.NullString{String: c.ParentID, Valid: c.ParentID != ""}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO collections (id, name, description, meta, parent_id)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   name=excluded.name,
		   description=excluded.description,
		   meta=excluded.meta,
		   parent_id=excluded.parent_id,
		   updated_at=CURRENT_TIMESTAMP,
		   version=version+1`,
		c.ID, c.Name, c.Description, c.Meta, parent,
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
		`SELECT id, name, COALESCE(parent_id, ''), description, meta, created_at, updated_at, version
		 FROM collections WHERE id = ?`, id)

	c := &domain.Collection{}
	err := row.Scan(&c.ID, &c.Name, &c.ParentID, &c.Description, &c.Meta,
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
		`SELECT id, name, COALESCE(parent_id, ''), description, meta, created_at, updated_at, version
		 FROM collections
		 ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("store: list collections: %w", err)
	}
	defer rows.Close()

	var result []*domain.Collection
	for rows.Next() {
		c := &domain.Collection{}
		if err := rows.Scan(&c.ID, &c.Name, &c.ParentID, &c.Description, &c.Meta,
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

// ListRootCollections returns root collections sorted by name.
func (s *Store) ListRootCollections(ctx context.Context) ([]*domain.Collection, error) {
	return s.listCollectionsWhere(ctx, "parent_id IS NULL")
}

// ListChildCollections returns direct children sorted by name.
func (s *Store) ListChildCollections(ctx context.Context, parentID string) ([]*domain.Collection, error) {
	return s.listCollectionsWhere(ctx, "parent_id = ?", parentID)
}

func (s *Store) listCollectionsWhere(ctx context.Context, where string, args ...any) ([]*domain.Collection, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, COALESCE(parent_id, ''), description, meta, created_at, updated_at, version FROM collections WHERE `+where+` ORDER BY name`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list collections: %w", err)
	}
	defer rows.Close()
	var out []*domain.Collection
	for rows.Next() {
		c := &domain.Collection{}
		if err := rows.Scan(&c.ID, &c.Name, &c.ParentID, &c.Description, &c.Meta, &c.CreatedAt, &c.UpdatedAt, &c.Version); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// MoveCollection reparents a collection, rejecting missing parents and cycles.
func (s *Store) MoveCollection(ctx context.Context, id, parentID string) error {
	if id == parentID {
		return fmt.Errorf("store: collection cannot parent itself")
	}
	if parentID != "" {
		if _, err := s.GetCollection(ctx, parentID); err != nil {
			return fmt.Errorf("store: parent: %w", err)
		}
	}
	for p := parentID; p != ""; {
		c, err := s.GetCollection(ctx, p)
		if err != nil {
			return err
		}
		if c.ID == id {
			return fmt.Errorf("store: moving collection would create a cycle")
		}
		p = c.ParentID
	}
	_, err := s.db.ExecContext(ctx, `UPDATE collections SET parent_id = ?, updated_at=CURRENT_TIMESTAMP WHERE id = ?`, sql.NullString{String: parentID, Valid: parentID != ""}, id)
	return err
}

// CollectionPath returns the full slash-delimited path for a collection.
func (s *Store) CollectionPath(ctx context.Context, id string) (string, error) {
	var parts []string
	seen := map[string]bool{}
	for id != "" {
		if seen[id] {
			return "", fmt.Errorf("store: collection cycle")
		}
		seen[id] = true
		c, err := s.GetCollection(ctx, id)
		if err != nil {
			return "", err
		}
		parts = append([]string{c.Name}, parts...)
		id = c.ParentID
	}
	return strings.Join(parts, "/"), nil
}

// ResolveCollectionPath resolves a full collection path or a unique suffix.
func (s *Store) ResolveCollectionPath(ctx context.Context, reference string) (*domain.Collection, error) {
	cols, err := s.ListCollections(ctx)
	if err != nil {
		return nil, err
	}
	var matches []*domain.Collection
	for _, c := range cols {
		p, e := s.CollectionPath(ctx, c.ID)
		if e != nil {
			return nil, e
		}
		if p == reference || strings.HasSuffix(p, "/"+reference) {
			matches = append(matches, c)
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("store: collection %q: %w", reference, ErrNotFound)
	}
	if len(matches) > 1 {
		paths := make([]string, 0, len(matches))
		for _, c := range matches {
			p, _ := s.CollectionPath(ctx, c.ID)
			paths = append(paths, p)
		}
		sort.Strings(paths)
		return nil, &AmbiguousPathError{Reference: reference, Matches: paths}
	}
	return matches[0], nil
}

// CountDescendants returns descendant collection and request counts.
func (s *Store) CountDescendants(ctx context.Context, id string) (collections, requests int, err error) {
	row := s.db.QueryRowContext(ctx, `WITH RECURSIVE tree(id) AS (SELECT id FROM collections WHERE id=? UNION ALL SELECT c.id FROM collections c JOIN tree t ON c.parent_id=t.id) SELECT (SELECT COUNT(*)-1 FROM tree), (SELECT COUNT(*) FROM requests WHERE collection_id IN (SELECT id FROM tree))`, id)
	err = row.Scan(&collections, &requests)
	return
}

// isSQLiteUnique reports whether err is a SQLite UNIQUE constraint violation.
func isSQLiteUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
