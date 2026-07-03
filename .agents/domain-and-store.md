# Domain & Store

## Domain Types (`internal/domain/`)

The domain package has **zero internal dependencies** — only stdlib. All packages import from here.

### Request (`request.go`)

```go
type Request struct {
    ID, CollectionID, Name string
    Method                   string  // GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS
    URL                      string
    Headers                  string  // JSON object
    AuthType                 string
    AuthConfig               string  // JSON AuthConfig
    Body                     string
    SortOrder                int
    Enabled                  bool
    CreatedAt, UpdatedAt     time.Time
}
```

Unique constraint: `(collection_id, name)`.

### Collection (`collection.go`)

```go
type Collection struct {
    ID, Name, Description string
    Meta                  string  // JSON, default "{}"
    CreatedAt, UpdatedAt  time.Time
    Version               int
}
```

Name is globally unique.

### Environment (`environment.go`)

```go
type Environment struct {
    ID, Name       string
    CollectionID   string  // empty/null = global environment
    Data           string  // JSON map of key→value
    SortOrder      int
    CreatedAt, UpdatedAt time.Time
}
```

- **Global env:** `collection_id IS NULL`, fixed ID `"global"`, name `"global"`
- **Per-collection:** each collection gets a `"default"` env on init

`Environment.Vars()` parses the JSON data map.

### Execution (`execution.go`)

Immutable audit record of a request run:

```go
type Execution struct {
    ID               string
    RequestID        string
    RequestSnapshot  string  // full JSON snapshot of Request at run time
    StatusCode       int
    ResponseHeaders  string  // JSON
    ResponseBody     string
    ResponseTimeMs   int
    StartedAt, CompletedAt time.Time
    Error            string
}
```

**Not FK-linked to Request** — history survives request deletion. Indexed by `(request_id, completed_at DESC)`.

### ScheduledRun (`scheduled_run.go`)

```go
type ScheduledRun struct {
    ID, RequestID string
    RunAt         time.Time
    Status        string  // pending, running, completed, failed, cancelled
    LastError     string
    CreatedAt, UpdatedAt time.Time
}
```

Indexed by `(status, run_at ASC)` for due-run queries.

### Auth (`request_auth.go`)

```go
type AuthConfig struct {
    Token    string  // Bearer
    Username, Password string  // Basic
    Key, Value string  // API key
    In       string  // "header" or "query" for API key
}
```

Auth types: `AuthTypeNone`, `AuthTypeBearer`, `AuthTypeBasic`, `AuthTypeAPIKey`.

Helpers: `NormalizeAuthType`, `ParseAuthConfig`, `MustAuthConfigJSON`.

## Store (`internal/store/`)

SQLite-backed repository using `modernc.org/sqlite` (CGO-free).

### Construction

```go
st, err := store.New(path, opts...)
```

Options:

- `WithBackup(backupDir)` — enable auto-backup before writes
- `WithCacheSize(n)` — SQLite cache (must be > 0)
- `WithLogger(slog.Logger)`

On open:

1. `SetMaxOpenConns(1)` — single connection preserves PRAGMA settings
2. PRAGMAs: `foreign_keys=ON`, `journal_mode=WAL`, `cache_size`
3. Run pending migrations
4. `ensureEnvSetup` — create global + default environments

Config directory and backup files use restrictive permissions (`0700` dir, `0600` backup files).

### Migrations (`migrations.go`)

**Append-only.** Comment in source: "DO NOT remove or reorder existing migrations."

| Version | Name | Adds |
|---|---|---|
| 1 | `initial_schema` | `schema_versions`, `collections`, `requests` |
| 2 | `add_environments` | `environments` table |
| 3 | `add_active_env` | `collection_active_env` table |
| 4 | `add_executions` | `executions` table |
| 5 | `add_request_auth` | `auth_type`, `auth_config` columns on requests |
| 6 | `add_scheduled_runs` | `scheduled_runs` table |

Migrations are idempotent and applied in transactions. To add schema changes: append a new `migration{version: 7, ...}` entry.

### Key Store Files

| File | Purpose |
|---|---|
| `collections.go` | Collection CRUD |
| `requests.go` | Request CRUD, list by collection |
| `environments.go` | Environment CRUD, active env |
| `executions.go` | Execution history read/write |
| `scheduled_runs.go` | Schedule CRUD, list due runs |
| `transaction.go` | `BeginTransaction` → `TransactionalWriter` |
| `backup.go` | Timestamped DB copies, retention |
| `interfaces.go` | Store-facing interface definitions |
| `errors.go` | Sentinel errors |

### Sentinel Errors

```go
store.ErrNotFound    // entity not found
store.ErrDuplicate   // unique constraint violation
```

Use `errors.Is` for classification.

### Backups (`backup.go`)

When auto-backup enabled (default):

- Copies `quark.db` → `backup/quark.db.YYYY-MM-DD-NNN`
- Keeps last N backups (default 10 from config)
- Sorted lexicographically (not mtime — survives clock skew)
- Files chmod `0600`

### Transactions

```go
tx, err := st.BeginTransaction(ctx)
// tx satisfies TransactionalWriter (same write interfaces as Store)
err = tx.Commit()  // or implicit rollback on error
```

Used for atomic multi-entity writes (Postman import).

### Active Environment

`collection_active_env` maps `collection_id → env_id`. Read via `GetActiveEnvironment`. Used by variable resolver and TUI env mode.

### Timeouts

`store.EnvDBTimeout = 5 * time.Second` — used by TUI and variable resolver for env DB lookups.

## Interface Summary

Store implements these consumer-facing interfaces (see `store.go` compile-time checks):

- `CollectionLister`, `CollectionWriter`
- `RequestReader`, `RequestWriter`
- `EnvironmentReader`, `EnvironmentWriter`
- `ActiveEnvironmentStore`
- `ExecutionReader`, `ExecutionWriter`
- `ScheduledRunReader`, `ScheduledRunWriter`, `ScheduledRunStore`
- `TransactionalWriter` (on `*Transaction`)

CLI and TUI packages define their own narrower subsets (e.g. `cli.RunStore`).

See also: [http-execution.md](http-execution.md), [architecture.md](architecture.md), [caveats.md](caveats.md).
