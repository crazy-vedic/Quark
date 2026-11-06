package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/crazy-vedic/quark/internal/domain"
)

func (s *Store) GetScheduledRun(ctx context.Context, id string) (*domain.ScheduledRun, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, request_id, run_at, status, last_error, created_at, updated_at
		 FROM scheduled_runs WHERE id = ?`, id)
	run, err := scanScheduledRun(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: get scheduled run %q: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("store: get scheduled run %q: %w", id, err)
	}
	return run, nil
}

func (s *Store) ListScheduledRuns(ctx context.Context) ([]*domain.ScheduledRun, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, request_id, run_at, status, last_error, created_at, updated_at
		 FROM scheduled_runs
		 ORDER BY run_at ASC, created_at ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("store: list scheduled runs: %w", err)
	}
	defer rows.Close()
	return scanScheduledRuns(rows)
}

func (s *Store) ListDueScheduledRuns(
	ctx context.Context,
	now time.Time,
) ([]*domain.ScheduledRun, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, request_id, run_at, status, last_error, created_at, updated_at
		 FROM scheduled_runs
		 WHERE status = ? AND run_at <= ?
		 ORDER BY run_at ASC, id ASC`,
		domain.ScheduledRunPending,
		now,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list due scheduled runs: %w", err)
	}
	defer rows.Close()
	return scanScheduledRuns(rows)
}

func (s *Store) NextPendingScheduledRun(ctx context.Context) (*domain.ScheduledRun, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, request_id, run_at, status, last_error, created_at, updated_at
		 FROM scheduled_runs
		 WHERE status = ?
		 ORDER BY run_at ASC, id ASC
		 LIMIT 1`,
		domain.ScheduledRunPending,
	)
	run, err := scanScheduledRun(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("store: next pending scheduled run: %w", err)
	}
	return run, nil
}

func (s *Store) SaveScheduledRun(ctx context.Context, run *domain.ScheduledRun) error {
	status := run.Status
	if status == "" {
		status = domain.ScheduledRunPending
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO scheduled_runs (id, request_id, run_at, status, last_error)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   request_id=excluded.request_id,
		   run_at=excluded.run_at,
		   status=excluded.status,
		   last_error=excluded.last_error,
		   updated_at=CURRENT_TIMESTAMP`,
		run.ID,
		run.RequestID,
		run.RunAt,
		status,
		run.LastError,
	)
	if err != nil {
		return fmt.Errorf("store: save scheduled run %q: %w", run.ID, err)
	}
	return nil
}

func (s *Store) DeleteScheduledRun(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM scheduled_runs WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete scheduled run %q: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: delete scheduled run %q: rows affected: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("store: delete scheduled run %q: %w", id, ErrNotFound)
	}
	return nil
}

func scanScheduledRun(row rowScanner) (*domain.ScheduledRun, error) {
	run := &domain.ScheduledRun{}
	if err := row.Scan(
		&run.ID,
		&run.RequestID,
		&run.RunAt,
		&run.Status,
		&run.LastError,
		&run.CreatedAt,
		&run.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return run, nil
}

func scanScheduledRuns(rows *sql.Rows) ([]*domain.ScheduledRun, error) {
	var out []*domain.ScheduledRun
	for rows.Next() {
		run, err := scanScheduledRun(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan scheduled run: %w", err)
		}
		out = append(out, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: scheduled run rows: %w", err)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}
