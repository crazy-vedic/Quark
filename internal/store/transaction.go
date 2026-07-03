package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"

	"github.com/crazy-vedic/quark/internal/domain"
)

// Transaction is a database transaction that implements CollectionWriter and
// RequestWriter. All operations are buffered until Commit() is called.
type Transaction struct {
	tx         *sql.Tx
	backupPath string
	logger     interface{ Warn(string, ...any) }
}

// BeginTransaction starts a new database transaction.
func (s *Store) BeginTransaction(ctx context.Context) (TransactionalWriter, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("store: begin transaction: %w", err)
	}
	return &Transaction{
		tx:         tx,
		backupPath: s.backupPath,
		logger:     s.logger,
	}, nil
}

// SaveCollection inserts or replaces a collection record within the transaction.
func (t *Transaction) SaveCollection(ctx context.Context, c *domain.Collection) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	_, err := t.tx.ExecContext(ctx,
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
	return nil
}

// DeleteCollection deletes a collection within the transaction.
func (t *Transaction) DeleteCollection(ctx context.Context, id string) error {
	_, err := t.tx.ExecContext(ctx, `DELETE FROM collections WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete collection %q: %w", id, err)
	}
	return nil
}

// SaveRequest inserts or updates a request within the transaction.
func (t *Transaction) SaveRequest(ctx context.Context, req *domain.Request) error {
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
	_, err := t.tx.ExecContext(
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
	return nil
}

// DeleteRequest deletes a request within the transaction.
func (t *Transaction) DeleteRequest(ctx context.Context, id string) error {
	_, err := t.tx.ExecContext(ctx, `DELETE FROM requests WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete request %q: %w", id, err)
	}
	return nil
}

// SaveEnvironment inserts or updates an environment within the transaction.
func (t *Transaction) SaveEnvironment(ctx context.Context, env *domain.Environment) error {
	if env.ID == "" {
		env.ID = uuid.New().String()
	}
	_, err := t.tx.ExecContext(ctx,
		`INSERT INTO environments (id, collection_id, name, data, sort_order)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   collection_id=excluded.collection_id,
		   name=excluded.name,
		   data=excluded.data,
		   sort_order=excluded.sort_order,
		   updated_at=CURRENT_TIMESTAMP`,
		env.ID, env.CollectionID, env.Name, env.Data, env.SortOrder,
	)
	if err != nil {
		if isSQLiteUnique(err) {
			return fmt.Errorf("store: save environment %q: %w", env.Name, ErrDuplicate)
		}
		return fmt.Errorf("store: save environment %q: %w", env.Name, err)
	}
	return nil
}

// DeleteEnvironment deletes an environment within the transaction.
func (t *Transaction) DeleteEnvironment(ctx context.Context, id string) error {
	_, err := t.tx.ExecContext(ctx, `DELETE FROM environments WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete environment %q: %w", id, err)
	}
	return nil
}

// CreateDefaultEnvironment creates a default environment for a collection within the transaction.
func (t *Transaction) CreateDefaultEnvironment(
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
	if err := t.SaveEnvironment(ctx, env); err != nil {
		return nil, err
	}
	return env, nil
}

// Commit persists all buffered operations.
func (t *Transaction) Commit() error {
	if err := t.tx.Commit(); err != nil {
		return fmt.Errorf("store: commit transaction: %w", err)
	}
	// Backup is deferred to the main store; transactions don't trigger backup.
	return nil
}

// Rollback discards all buffered operations.
func (t *Transaction) Rollback() error {
	if err := t.tx.Rollback(); err != nil {
		return fmt.Errorf("store: rollback transaction: %w", err)
	}
	return nil
}
