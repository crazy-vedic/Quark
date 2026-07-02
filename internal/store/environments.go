package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/crazy-vedic/quark/internal/domain"
)

// SaveEnvironment inserts or updates an environment record.
func (s *Store) SaveEnvironment(ctx context.Context, env *domain.Environment) error {
	if env.ID == "" {
		env.ID = uuid.New().String()
	}
	var colID sql.NullString
	if env.CollectionID != "" {
		colID = sql.NullString{String: env.CollectionID, Valid: true}
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO environments (id, collection_id, name, data, sort_order)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   collection_id=excluded.collection_id,
		   name=excluded.name,
		   data=excluded.data,
		   sort_order=excluded.sort_order,
		   updated_at=CURRENT_TIMESTAMP`,
		env.ID, colID, env.Name, env.Data, env.SortOrder,
	)
	if err != nil {
		if isSQLiteUnique(err) {
			return fmt.Errorf("store: save environment %q: %w", env.Name, ErrDuplicate)
		}
		return fmt.Errorf("store: save environment %q: %w", env.Name, err)
	}
	return nil
}

// GetEnvironment returns the environment with the given ID.
func (s *Store) GetEnvironment(ctx context.Context, id string) (*domain.Environment, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, collection_id, name, data, sort_order, created_at, updated_at
		 FROM environments WHERE id = ?`, id)

	return scanEnvironment(row)
}

// GetGlobalEnvironment returns the global environment (collection_id IS NULL).
func (s *Store) GetGlobalEnvironment(ctx context.Context) (*domain.Environment, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, collection_id, name, data, sort_order, created_at, updated_at
		 FROM environments WHERE collection_id IS NULL`)

	env, err := scanEnvironment(row)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("store: global environment: %w", ErrNotFound)
		}
		return nil, err
	}
	return env, nil
}

// GetEnvironmentByName returns an environment for a collection by name.
// collectionID can be empty for global environment.
func (s *Store) GetEnvironmentByName(
	ctx context.Context,
	collectionID, name string,
) (*domain.Environment, error) {
	var row *sql.Row
	if collectionID == "" {
		row = s.db.QueryRowContext(ctx,
			`SELECT id, collection_id, name, data, sort_order, created_at, updated_at
			 FROM environments WHERE collection_id IS NULL AND name = ?`,
			name)
	} else {
		row = s.db.QueryRowContext(ctx,
			`SELECT id, collection_id, name, data, sort_order, created_at, updated_at
			 FROM environments WHERE collection_id = ? AND name = ?`,
			collectionID, name)
	}

	return scanEnvironment(row)
}

// ListEnvironments returns all environments for a collection.
// If collectionID is empty, returns global environments.
// Ordered by sort_order ASC, then name ASC.
func (s *Store) ListEnvironments(
	ctx context.Context,
	collectionID string,
) ([]*domain.Environment, error) {
	var rows *sql.Rows
	var err error

	if collectionID == "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, collection_id, name, data, sort_order, created_at, updated_at
			 FROM environments WHERE collection_id IS NULL
			 ORDER BY sort_order ASC, name ASC`)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, collection_id, name, data, sort_order, created_at, updated_at
			 FROM environments WHERE collection_id = ?
			 ORDER BY sort_order ASC, name ASC`,
			collectionID)
	}
	if err != nil {
		return nil, fmt.Errorf("store: list environments: %w", err)
	}
	defer rows.Close()

	var result []*domain.Environment
	for rows.Next() {
		env, err := scanEnvironment(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, env)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list environments rows: %w", err)
	}

	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

// ListCollectionEnvironments returns all environments for a collection.
// Convenience alias for ListEnvironments with a non-empty collectionID.
func (s *Store) ListCollectionEnvironments(
	ctx context.Context,
	collectionID string,
) ([]*domain.Environment, error) {
	return s.ListEnvironments(ctx, collectionID)
}

// ListAllEnvironments returns all non-global environments across all collections
// in a single query. Results are ordered by collection_id, sort_order, name.
func (s *Store) ListAllEnvironments(ctx context.Context) ([]*domain.Environment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, collection_id, name, data, sort_order, created_at, updated_at
		 FROM environments WHERE collection_id IS NOT NULL
		 ORDER BY collection_id, sort_order, name`)
	if err != nil {
		return nil, fmt.Errorf("store: list all environments: %w", err)
	}
	defer rows.Close()

	var result []*domain.Environment
	for rows.Next() {
		env, err := scanEnvironment(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, env)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list all environments rows: %w", err)
	}

	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

// DeleteEnvironment deletes an environment by ID.
func (s *Store) DeleteEnvironment(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM environments WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete environment %q: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: delete environment %q: rows affected: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("store: delete environment %q: %w", id, ErrNotFound)
	}
	return nil
}

// CreateDefaultEnvironment creates a default environment for a collection.
// Returns ErrDuplicate if the collection already has a default environment.
func (s *Store) CreateDefaultEnvironment(
	ctx context.Context,
	collectionID string,
) (*domain.Environment, error) {
	env := &domain.Environment{
		ID:           fmt.Sprintf("default-%s", collectionID),
		CollectionID: collectionID,
		Name:         "default",
		Data:         "{}",
		SortOrder:    0,
	}
	if err := s.SaveEnvironment(ctx, env); err != nil {
		return nil, err
	}
	return env, nil
}

// rowScanner abstracts *sql.Row and *sql.Rows so the scan logic can be shared.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanEnvironment reads a single environment from a row scanner.
func scanEnvironment(row rowScanner) (*domain.Environment, error) {
	e := &domain.Environment{}
	var collectionID sql.NullString
	err := row.Scan(&e.ID, &collectionID, &e.Name, &e.Data, &e.SortOrder,
		&e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: scan environment: %w", err)
	}
	if collectionID.Valid {
		e.CollectionID = collectionID.String
	}
	return e, nil
}

// SetActiveEnvironment persists the active environment for a collection.
func (s *Store) SetActiveEnvironment(ctx context.Context, collectionID, envID string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO collection_active_env (collection_id, env_id) VALUES (?, ?)
		 ON CONFLICT(collection_id) DO UPDATE SET env_id=excluded.env_id, updated_at=CURRENT_TIMESTAMP`,
		collectionID, envID,
	)
	if err != nil {
		return fmt.Errorf("store: set active env: %w", err)
	}
	return nil
}

// GetActiveEnvironment returns the active environment ID for a collection, or "" if none.
func (s *Store) GetActiveEnvironment(ctx context.Context, collectionID string) (string, error) {
	var envID string
	err := s.db.QueryRowContext(ctx,
		`SELECT env_id FROM collection_active_env WHERE collection_id = ?`, collectionID,
	).Scan(&envID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("store: get active env: %w", err)
	}
	return envID, nil
}
