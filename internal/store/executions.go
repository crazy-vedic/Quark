package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/crazy-vedic/quark/internal/domain"
)

// SaveExecution inserts an immutable execution record. When the store keeps
// a limited history (WithHistoryKeepLast), older executions of the same
// request are deleted in the same transaction.
func (s *Store) SaveExecution(ctx context.Context, ex *domain.Execution) error {
	headers := ex.ResponseHeaders
	if headers == "" {
		headers = "{}"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: save execution %q: begin: %w", ex.ID, err)
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx,
		`INSERT INTO executions (
			id, request_id, request_snapshot, status_code, response_headers,
			response_body, response_time_ms, started_at, completed_at, error
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ex.ID,
		ex.RequestID,
		ex.RequestSnapshot,
		ex.StatusCode,
		headers,
		ex.ResponseBody,
		ex.ResponseTimeMs,
		ex.StartedAt,
		ex.CompletedAt,
		ex.Error,
	)
	if err != nil {
		return fmt.Errorf("store: save execution %q: %w", ex.ID, err)
	}
	if err := pruneExecutions(ctx, tx, ex.RequestID, s.historyKeep); err != nil {
		return fmt.Errorf("store: save execution %q: %w", ex.ID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: save execution %q: commit: %w", ex.ID, err)
	}
	return nil
}

// pruneExecutions deletes all but the newest keep executions of a request,
// using the same newest-first order as ListExecutionsByRequest. keep <= 0
// deletes nothing.
func pruneExecutions(ctx context.Context, tx *sql.Tx, requestID string, keep int) error {
	if keep <= 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx,
		`DELETE FROM executions
		 WHERE request_id = ?
		   AND id NOT IN (
		     SELECT id FROM executions
		     WHERE request_id = ?
		     ORDER BY completed_at DESC, started_at DESC, id DESC
		     LIMIT ?
		   )`,
		requestID, requestID, keep,
	)
	if err != nil {
		return fmt.Errorf("prune history: %w", err)
	}
	return nil
}

// ListExecutionsByRequest returns executions for a request ordered newest-first.
func (s *Store) ListExecutionsByRequest(
	ctx context.Context,
	requestID string,
) ([]*domain.Execution, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, request_snapshot, request_id, status_code, response_headers,
		        response_body, response_time_ms, started_at, completed_at, error
		 FROM executions
		 WHERE request_id = ?
		 ORDER BY completed_at DESC, started_at DESC, id DESC`,
		requestID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list executions for request %q: %w", requestID, err)
	}
	defer rows.Close()

	var out []*domain.Execution
	for rows.Next() {
		ex := &domain.Execution{}
		var responseBody sql.NullString
		var errorText sql.NullString
		if err := rows.Scan(
			&ex.ID,
			&ex.RequestSnapshot,
			&ex.RequestID,
			&ex.StatusCode,
			&ex.ResponseHeaders,
			&responseBody,
			&ex.ResponseTimeMs,
			&ex.StartedAt,
			&ex.CompletedAt,
			&errorText,
		); err != nil {
			return nil, fmt.Errorf("store: list executions scan: %w", err)
		}
		ex.ResponseBody = responseBody.String
		ex.Error = errorText.String
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
