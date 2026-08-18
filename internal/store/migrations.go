package store

import (
	"context"
	"fmt"
	"log/slog"
)

// schema is the initial v1 schema for all tables.
const schema = `
CREATE TABLE IF NOT EXISTS schema_versions (
    version INTEGER PRIMARY KEY,
    applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS collections (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT,
    meta        TEXT DEFAULT '{}',
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    version     INTEGER DEFAULT 1
);

CREATE TABLE IF NOT EXISTS requests (
    id            TEXT PRIMARY KEY,
    collection_id TEXT NOT NULL,
    name          TEXT NOT NULL,
    method        TEXT NOT NULL CHECK(method IN ('GET','POST','PUT','DELETE','PATCH','HEAD','OPTIONS')),
    url           TEXT NOT NULL,
    headers       TEXT DEFAULT '{}',
    body          TEXT,
    sort_order    INTEGER DEFAULT 0,
    enabled       BOOLEAN DEFAULT 1,
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (collection_id) REFERENCES collections(id) ON DELETE CASCADE,
    UNIQUE(collection_id, name)
);

CREATE INDEX IF NOT EXISTS idx_requests_collection ON requests(collection_id, sort_order);
`

// migration represents a single schema migration.
type migration struct {
	version int
	name    string
	upSQL   string
	downSQL string
}

// migrations holds all schema migrations in order.
// Add new migrations at the end of the slice.
// DO NOT remove or reorder existing migrations.
var migrations = []migration{
	{
		version: 1,
		name:    "initial_schema",
		upSQL:   schema,
	},
	{
		version: 2,
		name:    "add_environments",
		upSQL: `
CREATE TABLE IF NOT EXISTS environments (
    id            TEXT PRIMARY KEY,
    collection_id TEXT,
    name          TEXT NOT NULL,
    data          TEXT NOT NULL DEFAULT '{}',
    sort_order    INTEGER DEFAULT 0,
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (collection_id) REFERENCES collections(id) ON DELETE CASCADE,
    UNIQUE(collection_id, name)
);
CREATE INDEX IF NOT EXISTS idx_envs_collection ON environments(collection_id, sort_order);
`,
		downSQL: `
DROP INDEX IF EXISTS idx_envs_collection;
DROP TABLE IF EXISTS environments;
`,
	},
	{
		version: 3,
		name:    "add_active_env",
		upSQL: `
CREATE TABLE IF NOT EXISTS collection_active_env (
    collection_id TEXT PRIMARY KEY,
    env_id        TEXT NOT NULL,
    updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (collection_id) REFERENCES collections(id) ON DELETE CASCADE,
    FOREIGN KEY (env_id) REFERENCES environments(id) ON DELETE CASCADE
);
`,
		downSQL: `
DROP TABLE IF EXISTS collection_active_env;
`,
	},
	{
		version: 4,
		name:    "add_executions",
		upSQL: `
CREATE TABLE IF NOT EXISTS executions (
    id               TEXT PRIMARY KEY,
    request_id       TEXT NOT NULL,
    request_snapshot TEXT NOT NULL,
    status_code      INTEGER NOT NULL DEFAULT 0,
    response_headers TEXT NOT NULL DEFAULT '{}',
    response_body    TEXT NOT NULL DEFAULT '',
    response_time_ms INTEGER NOT NULL DEFAULT 0,
    started_at       DATETIME NOT NULL,
    completed_at     DATETIME NOT NULL,
    error            TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_executions_request_completed
ON executions(request_id, completed_at DESC, started_at DESC, id DESC);
`,
		downSQL: `
DROP INDEX IF EXISTS idx_executions_request_completed;
DROP TABLE IF EXISTS executions;
`,
	},
	{
		version: 5,
		name:    "add_request_auth",
		upSQL: `
ALTER TABLE requests ADD COLUMN auth_type TEXT NOT NULL DEFAULT '';
ALTER TABLE requests ADD COLUMN auth_config TEXT NOT NULL DEFAULT '{}';
`,
	},
	{
		version: 6,
		name:    "add_scheduled_runs",
		upSQL: `
CREATE TABLE IF NOT EXISTS scheduled_runs (
    id          TEXT PRIMARY KEY,
    request_id  TEXT NOT NULL,
    run_at      DATETIME NOT NULL,
    status      TEXT NOT NULL DEFAULT 'pending',
    last_error  TEXT NOT NULL DEFAULT '',
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (request_id) REFERENCES requests(id) ON DELETE CASCADE,
    CHECK(status IN ('pending','running','completed','failed','cancelled'))
);
CREATE INDEX IF NOT EXISTS idx_scheduled_runs_due
ON scheduled_runs(status, run_at ASC, id ASC);
`,
		downSQL: `
DROP INDEX IF EXISTS idx_scheduled_runs_due;
DROP TABLE IF EXISTS scheduled_runs;
`,
	},
	{
		version: 7,
		name:    "repair_empty_request_ids",
		upSQL: `
PRAGMA defer_foreign_keys = ON;

CREATE TEMP TABLE request_id_repairs (
    old_id TEXT PRIMARY KEY,
    new_id TEXT NOT NULL
);

INSERT INTO request_id_repairs (old_id, new_id)
SELECT id, lower(hex(randomblob(16)))
FROM requests
WHERE id = '';

UPDATE executions
SET request_id = (SELECT new_id FROM request_id_repairs WHERE old_id = executions.request_id)
WHERE request_id IN (SELECT old_id FROM request_id_repairs);

UPDATE scheduled_runs
SET request_id = (SELECT new_id FROM request_id_repairs WHERE old_id = scheduled_runs.request_id)
WHERE request_id IN (SELECT old_id FROM request_id_repairs);

UPDATE requests
SET id = (SELECT new_id FROM request_id_repairs WHERE old_id = requests.id)
WHERE id IN (SELECT old_id FROM request_id_repairs);

DROP TABLE request_id_repairs;
`,
	},
	{
		version: 8,
		name:    "nested_collections",
		upSQL: `
ALTER TABLE collections ADD COLUMN parent_id TEXT REFERENCES collections(id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS idx_collections_parent ON collections(parent_id, name);
CREATE UNIQUE INDEX IF NOT EXISTS idx_collections_sibling_name
ON collections(COALESCE(parent_id, ''), name);
`,
		downSQL: `DROP INDEX IF EXISTS idx_collections_sibling_name; DROP INDEX IF EXISTS idx_collections_parent;`,
	},
}

// migrate runs all pending migrations in order.
// It is idempotent: calling it multiple times is safe.
func (s *Store) migrate() error {
	// Ensure schema_versions table exists (part of migration v1 logic).
	if err := s.createSchemaVersionsTable(); err != nil {
		return err
	}

	// Get current schema version.
	currentVersion, err := s.currentSchemaVersion()
	if err != nil {
		return err
	}

	// Apply pending migrations.
	for _, m := range migrations {
		if m.version > currentVersion {
			if err := s.applyMigration(m); err != nil {
				return fmt.Errorf("store: migrate v%d %s: %w", m.version, m.name, err)
			}
		}
	}
	// Keep the legacy repair safe to rerun. This also handles databases where
	// an older migration was manually rolled back or repaired data was added
	// after the migration record was written.
	if err := s.repairEmptyRequestIDs(); err != nil {
		return err
	}

	return nil
}

func (s *Store) repairEmptyRequestIDs() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`PRAGMA defer_foreign_keys = ON; CREATE TEMP TABLE IF NOT EXISTS request_id_repairs (old_id TEXT PRIMARY KEY, new_id TEXT NOT NULL); DELETE FROM request_id_repairs; INSERT INTO request_id_repairs (old_id, new_id) SELECT id, lower(hex(randomblob(16))) FROM requests WHERE id = ''; UPDATE executions SET request_id = (SELECT new_id FROM request_id_repairs WHERE old_id = executions.request_id) WHERE request_id IN (SELECT old_id FROM request_id_repairs); UPDATE scheduled_runs SET request_id = (SELECT new_id FROM request_id_repairs WHERE old_id = scheduled_runs.request_id) WHERE request_id IN (SELECT old_id FROM request_id_repairs); UPDATE requests SET id = (SELECT new_id FROM request_id_repairs WHERE old_id = requests.id) WHERE id IN (SELECT old_id FROM request_id_repairs); DROP TABLE request_id_repairs;`); err != nil {
		return err
	}
	return tx.Commit()
}

// createSchemaVersionsTable creates the migration tracking table.
func (s *Store) createSchemaVersionsTable() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_versions (
			version INTEGER PRIMARY KEY,
			applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("store: create schema_versions table: %w", err)
	}
	return nil
}

// currentSchemaVersion returns the highest applied migration version.
// Returns 0 if no migrations have been applied (fresh database).
func (s *Store) currentSchemaVersion() (int, error) {
	var version int
	err := s.db.QueryRow(`
		SELECT COALESCE(MAX(version), 0) FROM schema_versions
	`).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("store: get current schema version: %w", err)
	}
	return version, nil
}

// applyMigration runs a single migration within a transaction.
func (s *Store) applyMigration(m migration) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// Execute migration SQL.
	if _, err := tx.Exec(m.upSQL); err != nil {
		return fmt.Errorf("execute SQL: %w", err)
	}

	// Record migration as applied.
	_, err = tx.Exec(
		`INSERT INTO schema_versions (version) VALUES (?)`,
		m.version,
	)
	if err != nil {
		return fmt.Errorf("record version: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// initGlobalEnvironment creates the global environment if it doesn't exist.
func (s *Store) initGlobalEnvironment(ctx context.Context) error {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM environments WHERE collection_id IS NULL`,
	).Scan(&count)
	if err != nil {
		return fmt.Errorf("store: check global environment: %w", err)
	}
	if count > 0 {
		return nil // already exists
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO environments (id, collection_id, name, data, sort_order)
		 VALUES (?, NULL, ?, ?, ?)`,
		"global", "global", "{}", 0,
	)
	if err != nil {
		return fmt.Errorf("store: create global environment: %w", err)
	}
	if s.logger != nil {
		s.logger.Info("store: created global environment")
	}
	return nil
}

// initDefaultEnvironments creates default environments for all collections
// that don't have one.
func (s *Store) initDefaultEnvironments(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id FROM collections c
		WHERE NOT EXISTS (
			SELECT 1 FROM environments e
			WHERE e.collection_id = c.id AND e.name = 'default'
		)
	`)
	if err != nil {
		return fmt.Errorf("store: find collections without default env: %w", err)
	}
	defer rows.Close()

	var collectionIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("store: scan collection id: %w", err)
		}
		collectionIDs = append(collectionIDs, id)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: rows error: %w", err)
	}

	for _, colID := range collectionIDs {
		_, err := s.db.ExecContext(ctx,
			`INSERT INTO environments (id, collection_id, name, data, sort_order)
			 VALUES (?, ?, ?, ?, ?)`,
			fmt.Sprintf("default-%s", colID), colID, "default", "{}", 0,
		)
		if err != nil {
			return fmt.Errorf("store: create default environment for %s: %w", colID, err)
		}
	}

	if s.logger != nil && len(collectionIDs) > 0 {
		s.logger.Info("store: created default environments", slog.Int("count", len(collectionIDs)))
	}
	return nil
}

// ensureEnvSetup initializes global and default environments.
func (s *Store) ensureEnvSetup(ctx context.Context) error {
	if err := s.initGlobalEnvironment(ctx); err != nil {
		return err
	}
	if err := s.initDefaultEnvironments(ctx); err != nil {
		return err
	}
	return nil
}
