package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/crazy-vedic/quark/internal/domain"
)

// SaveExecution inserts an immutable execution record.
func (s *Store) SaveExecution(ctx context.Context, ex *domain.Execution) error {
	headers := ex.ResponseHeaders
	if headers == "" {
		headers = "{}"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO executions (
			id, request_id, request_snapshot, status_code, response_headers,
			response_body, response_size, response_body_truncated,
			response_time_ms, started_at, completed_at, error
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ex.ID,
		ex.RequestID,
		ex.RequestSnapshot,
		ex.StatusCode,
		headers,
		ex.ResponseBody,
		ex.BodySize(),
		ex.ResponseBodyTruncated,
		ex.ResponseTimeMs,
		ex.StartedAt,
		ex.CompletedAt,
		ex.Error,
	)
	if err != nil {
		return fmt.Errorf("store: save execution %q: %w", ex.ID, err)
	}
	return nil
}

// ListExecutionsByRequest returns at most limit executions for a request
// ordered newest-first. A limit of zero or less returns every execution.
// Response bodies are not loaded: each entry has BodyOmitted set, and
// GetExecution returns one entry with its body.
func (s *Store) ListExecutionsByRequest(
	ctx context.Context,
	requestID string,
	limit int,
) ([]*domain.Execution, error) {
	if limit <= 0 {
		limit = -1 // SQLite: a negative LIMIT means no limit.
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, request_snapshot, request_id, status_code, response_headers,
		        '', response_size, response_body_truncated, response_time_ms,
		        started_at, completed_at, error
		 FROM executions
		 WHERE request_id = ?
		 ORDER BY completed_at DESC, started_at DESC, id DESC
		 LIMIT ?`,
		requestID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list executions for request %q: %w", requestID, err)
	}
	defer rows.Close()

	var out []*domain.Execution
	for rows.Next() {
		ex, err := scanExecution(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list executions scan: %w", err)
		}
		ex.BodyOmitted = true
		out = append(out, ex)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list executions rows: %w", err)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// GetExecution returns one execution, including its stored response body.
// Returns ErrNotFound when no execution has the ID.
func (s *Store) GetExecution(ctx context.Context, id string) (*domain.Execution, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, request_snapshot, request_id, status_code, response_headers,
		        response_body, response_size, response_body_truncated, response_time_ms,
		        started_at, completed_at, error
		 FROM executions
		 WHERE id = ?`,
		id,
	)
	ex, err := scanExecution(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("store: get execution %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("store: get execution %q: %w", id, err)
	}
	return ex, nil
}

func scanExecution(row rowScanner) (*domain.Execution, error) {
	ex := &domain.Execution{}
	var responseBody sql.NullString
	var errorText sql.NullString
	if err := row.Scan(
		&ex.ID,
		&ex.RequestSnapshot,
		&ex.RequestID,
		&ex.StatusCode,
		&ex.ResponseHeaders,
		&responseBody,
		&ex.ResponseSize,
		&ex.ResponseBodyTruncated,
		&ex.ResponseTimeMs,
		&ex.StartedAt,
		&ex.CompletedAt,
		&errorText,
	); err != nil {
		return nil, err
	}
	ex.ResponseBody = responseBody.String
	ex.Error = errorText.String
	return ex, nil
}
