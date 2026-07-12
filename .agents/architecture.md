# Architecture

## Design Principles

1. **Single DI seam** — `cmd/quark/main.go` is the only place concrete types are constructed and wired. The file header states this explicitly.
2. **Interface segregation** — consumers define narrow interfaces; `*store.Store` satisfies many of them via compile-time checks.
3. **Lazy initialization** — store, config, and executor are created on first command use so `--config` is honored before DB open.
4. **Local-first** — no external services; SQLite is the sole persistence layer.

## Dependency Flow

```
cmd/quark/main.go
    ├── internal/tui          (bubbletea Model — default action)
    ├── internal/cli          (cobra subcommand handlers)
    ├── internal/store        (SQLite repository)
    ├── internal/exec         (HTTP executor)
    ├── internal/config       (config.toml)
    ├── internal/curl         (cURL import)
    ├── internal/search       (fuzzy search)
    └── internal/keybindings  (TUI key resolution)
```

Both TUI and CLI depend on `store` and `exec`. Import and search are CLI/TUI-adjacent adapters.

## The DI Seam (`cmd/quark/main.go`)

`runtimeOnce()` builds a `runtime` struct once:

```go
type runtime struct {
    st       *store.Store
    cfg      config.Config
    executor *exec.Executor
    importer *curl.Importer
    searcher *search.Searcher
}
```

Wiring steps inside `runtimeOnce()`:

1. `config.Load(configDir)` — defaults on missing/invalid file
2. `os.MkdirAll(configDir, 0700)`
3. `store.New(dbPath, storeOpts...)` — optional auto-backup
4. `exec.New(transport, opts...)` — timeout, variable resolver, execution writer
5. `curl.NewImporter()`, `search.New(st)`

`launchTUI()` passes `st` as multiple interface types via `tui.Deps`:

```go
tui.Deps{
    Lister: st, Reader: st, Writer: st, ColWriter: st,
    ExecutionReader: st, EnvReader: st, EnvWriter: st,
    ActiveEnvStore: st, Scheduler: st,
    Executor: executor, Searcher: searcher, Importer: importer,
    Config: cfg, Resolver: keybindings.NewResolver(cfg.Keybindings),
    Ctx: ctx, DebugLog: debugLog, ConfigDir: configDir,
}
```

## Interface Segregation Pattern

Interfaces are defined **at the consumer**, not the provider.

Example from `internal/tui/interfaces.go`:

```go
type RequestExecutor interface {
    Execute(ctx context.Context, req *domain.Request) (*exec.ExecuteResult, error)
}
```

`*exec.Executor` satisfies this structurally. Same pattern in `internal/cli/` (e.g. `RunStore`, `EnvStore`, `ImportPostmanStore`).

`internal/store/store.go` has compile-time checks for ~13 interfaces:

```go
var (
    _ CollectionLister       = (*Store)(nil)
    _ CollectionWriter       = (*Store)(nil)
    _ RequestReader          = (*Store)(nil)
    _ RequestWriter          = (*Store)(nil)
    _ EnvironmentReader      = (*Store)(nil)
    _ EnvironmentWriter      = (*Store)(nil)
    _ ActiveEnvironmentStore = (*Store)(nil)
    _ ExecutionReader        = (*Store)(nil)
    _ ExecutionWriter        = (*Store)(nil)
    _ ScheduledRunReader     = (*Store)(nil)
    _ ScheduledRunWriter     = (*Store)(nil)
    _ ScheduledRunStore      = (*Store)(nil)
    _ TransactionalWriter    = (*Transaction)(nil)
)
```

If `*Store` stops implementing any interface, **this file fails to compile**.

## Package Responsibilities

| Package | Responsibility | Depends on |
|---|---|---|
| `domain` | Shared entities: Request, Collection, Environment, Execution, ScheduledRun, AuthConfig | Nothing (stdlib only) |
| `store` | SQLite CRUD, migrations, backups, transactions | `domain` |
| `exec` | HTTP dispatch, variable interpolation, auth, hooks, history | `domain` |
| `tui` | Bubbletea Model/Update/View, editors, modes | `domain`, `exec`, `search`, `curl`, `keybindings`, `config` |
| `cli` | Thin cobra command handlers (no business logic) | `domain`, `store`, `exec`, `curl`, `postman`, `config` |
| `curl` | Parse cURL commands → structured request | `domain` |
| `postman` | Parse Postman Collection v2.1, map auth/env | `domain` |
| `search` | Fuzzy search over requests | `domain`, store interface |
| `keybindings` | Defaults, resolver, action→key mapping, hints | — |
| `config` | Load/merge/save `config.toml` | `keybindings` |
| `highlight` | Chroma-based ANSI syntax highlighting | — |
| `schedule` | Parse schedule time expressions | — |
| `tuitest` | Model-level TUI test harness | `tui`, `store`, `exec` |

## Exec Hooks

`internal/exec` defines optional `PreRequestHook` / `PostResponseHook` interfaces and
`WithPreRequestHooks` / `WithPostResponseHooks` options. Production `main.go` does not
register any hooks today; the extension point remains for future wiring in the DI seam.

## Transactions

`store.BeginTransaction(ctx)` returns a `TransactionalWriter` for atomic multi-write operations (e.g. Postman bulk import). The transaction type also satisfies the same writer interfaces.

## Signal Handling

Root context from `signal.NotifyContext(SIGINT, SIGTERM)` is passed to:

- cobra commands via `root.SetContext(ctx)`
- TUI via `tui.Deps.Ctx` — cancels in-flight HTTP on interrupt

Bubbletea v1.3.10 restores the terminal on panic; no explicit recovery needed in `launchTUI`.

## Adding New Features — Checklist

1. Define narrow interface at the **consumer** package if needed.
2. Implement on `*store.Store`, `*exec.Executor`, or a new type.
3. Wire in `cmd/quark/main.go` only.
4. Add tests colocated in the package; e2e if user-facing flow.
5. If schema changes: append migration in `internal/store/migrations.go`.

See also: [entrypoints.md](entrypoints.md), [domain-and-store.md](domain-and-store.md).
