package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/crazy-vedic/quark/internal/domain"
)

// GetRequest returns the request with the given ID.
// Returns nil, ErrNotFound if no request exists.
func (s *Store) GetRequest(ctx context.Context, id string) (*domain.Request, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, collection_id, name, method, url, headers, auth_type, auth_config, body,
		        sort_order, enabled, created_at, updated_at
		 FROM requests WHERE id = ?`, id)

	r := &domain.Request{}
	var body sql.NullString
	err := row.Scan(&r.ID, &r.CollectionID, &r.Name, &r.Method, &r.URL,
		&r.Headers, &r.AuthType, &r.AuthConfig, &body, &r.SortOrder, &r.Enabled,
		&r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: get request %q: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("store: get request %q: %w", id, err)
	}
	r.Body = body.String
	return r, nil
}

// ListRequests returns all requests in a collection ordered by
// sort_order ASC, created_at ASC, id ASC (deterministic tiebreaker).
// Returns nil, nil when the collection exists but has no requests.
// Returns nil, ErrNotFound if collectionID does not exist.
func (s *Store) ListRequests(ctx context.Context, collectionID string) ([]*domain.Request, error) {
	// First confirm the collection exists.
	var exists int
	row := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM collections WHERE id = ?`, collectionID)
	if err := row.Scan(&exists); err != nil {
		return nil, fmt.Errorf("store: list requests check collection %q: %w", collectionID, err)
	}
	if exists == 0 {
		return nil, fmt.Errorf("store: list requests: collection %q: %w", collectionID, ErrNotFound)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, collection_id, name, method, url, headers, auth_type, auth_config, body,
		        sort_order, enabled, created_at, updated_at
		 FROM requests
		 WHERE collection_id = ?
		 ORDER BY sort_order ASC, created_at ASC, id ASC`, collectionID)
	if err != nil {
		return nil, fmt.Errorf("store: list requests for collection %q: %w", collectionID, err)
	}
	defer rows.Close()

	var result []*domain.Request
	for rows.Next() {
		r := &domain.Request{}
		var body sql.NullString
		if err := rows.Scan(&r.ID, &r.CollectionID, &r.Name, &r.Method, &r.URL,
			&r.Headers, &r.AuthType, &r.AuthConfig, &body, &r.SortOrder, &r.Enabled,
			&r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("store: list requests scan: %w", err)
		}
		r.Body = body.String
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list requests rows: %w", err)
	}

	// Return nil (not empty slice) for empty collection — per contract.
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

// SaveRequest inserts or updates a request.
func (s *Store) SaveRequest(ctx context.Context, req *domain.Request) error {
	if req.ID == "" {
		req.ID = uuid.New().String()
	}
	headers := req.Headers
	if headers == "" {
		headers = "{}"
	}
	authConfig := req.AuthConfig
	if authConfig == "" {
		authConfig = "{}"
	}
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO requests (id, collection_id, name, method, url, headers, auth_type, auth_config, body, sort_order, enabled)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   name=excluded.name,
		   method=excluded.method,
		   url=excluded.url,
		   headers=excluded.headers,
		   auth_type=excluded.auth_type,
		   auth_config=excluded.auth_config,
		   body=excluded.body,
		   sort_order=excluded.sort_order,
		   enabled=excluded.enabled,
		   updated_at=CURRENT_TIMESTAMP`,
		req.ID,
		req.CollectionID,
		req.Name,
		req.Method,
		req.URL,
		headers,
		req.AuthType,
		authConfig,
		req.Body,
		req.SortOrder,
		req.Enabled,
	)
	if err != nil {
		return fmt.Errorf("store: save request %q: %w", req.ID, err)
	}

	if s.backupPath != "" {
		if berr := s.backup(); berr != nil {
			s.logger.Warn("store: backup failed", "err", berr)
		}
	}
	return nil
}

// DeleteRequest deletes a request by ID.
func (s *Store) DeleteRequest(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM requests WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete request %q: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: delete request %q: rows affected: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("store: delete request %q: %w", id, ErrNotFound)
	}
	return nil
}
