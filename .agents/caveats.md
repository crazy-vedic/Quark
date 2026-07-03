# Caveats

Consolidated gotchas agents must not rediscover. Read this before making changes to config paths, security, scheduling, or TUI behavior.

## Documentation Mismatches

### Config directory default

| Source | Says |
|---|---|
| README | `~/.quark/quark.db` |
| `--config` flag default | `~/.quark` (home dir) |
| `config.Load` | Uses whatever `--config` points to |

Default is stable across directories; use `--config` to override.

### Debug log path

| Source | Says |
|---|---|
| `--debug` help text | `~/.quark/debug.log` |
| Actual implementation (`openDebugLog`) | `/tmp/quark_debug_logs/debug.log` |

Fixing help text or implementation should align these — check both when changing.

## Security

### Plaintext credentials

Bearer tokens, Basic passwords, API keys, and Authorization headers are stored **unencrypted** in SQLite (`requests.auth_config`, headers JSON). Backups are full DB copies.

Protection is filesystem permissions only:

- Config dir: `0700`
- Backup files: `0600`
- Response temp files: `0600` in `/tmp`

Do not add features that log or transmit credentials. Do not weaken directory permissions.

### Response temp files

Large HTTP response bodies stream to `/tmp/quark_*` temp files. Must call `ExecuteResult.Cleanup()`. TUI cleans up on new response (BUG-007) but crashes may leave orphans.

## Scheduling

**Not a daemon.** `quark schedule add` only writes a DB row. Nothing fires automatically unless:

- `quark schedule run-due` is invoked (cron, systemd timer, CI), or
- TUI in-app timer checks due runs

Design implication: do not assume background scheduling exists in CLI-only deployments.

## Database

### Single connection

`db.SetMaxOpenConns(1)` — all DB access serialized within one process. Required to preserve SQLite PRAGMA settings across queries.

Multiple concurrent `quark` processes on the same `quark.db` can cause `database is locked` errors. WAL mode mitigates but does not eliminate this.

### Migrations are append-only

Never reorder or remove entries in `internal/store/migrations.go`. Add new migrations at the end with incrementing version numbers. Existing user DBs depend on version ordering.

## Plugin System

- Hook interfaces frozen at v1.0 — adding methods is breaking
- Registry constructed in `main.go` but **no plugins registered**
- Lua "V2" runtime mentioned in docs but not implemented
- Do not assume hooks exist when testing default behavior

## Environment / CLI Gaps

- `quark env active` prints that it applies to TUI — verify actual persistence behavior before changing
- CLI `run` resolves env vars via store chain, not OS environment
- Global env (`env set-global`) works from CLI; collection env vars require collection ID

## Import Limitations

### cURL

- No shell expansion (`$VAR`, `$(cmd)`)
- No `@filename` body file reading
- Unsupported flags silently skipped or warned

### Postman

- OAuth1, OAuth2, AWS SigV4 auth → warnings, not imported
- Pre-request/test scripts not executed
- Some body modes partially supported (formdata, urlencoded)
- Variables preserved as `{{VAR}}` — require environment setup post-import

## HTTP Execution

- 4xx/5xx responses are **success** from `Execute()` — check `StatusCode`
- Only `http`/`https` schemes allowed
- Malformed headers JSON → warn and proceed without headers
- Post-response hook errors → logged, not returned

## TUI BUG-NNN Registry

Regression tests in `internal/tui/bugfix_test.go`. Do not reintroduce these:

| ID | Summary | Key file |
|---|---|---|
| BUG-001 | Clear `editingURL` on import modal Escape | `bugfix_test.go` |
| BUG-002 | Never render binary content raw in terminal | `view.go:778` |
| BUG-003 | No double "invalid URL:" error prefix | `executor.go:99` |
| BUG-004 | Stub keys must give user feedback | `bugfix_test.go` |
| BUG-007 | Clean up old streamed response temp file | `view.go`, `bugfix_test.go` |
| BUG-008 | Distinguish "not searched" vs "no results"; Esc cancels search | `update.go`, `view.go` |
| BUG-009 | `q` in help mode closes help, not app | `bugfix_test.go` |
| BUG-010 | Brief tmux/screen Ctrl+w warning on startup | `model.go:432` |
| BUG-011 | Sidebar scroll offset for long collection lists | `model.go:211` |

## Terminal Requirements

- 256-color terminal expected for syntax highlighting and lipgloss themes
- tmux/GNU screen may intercept Ctrl+w (BUG-010 warning)
- Bubbletea alt-screen mode — ensure terminal restoration on crash (handled by bubbletea v1.3.10+)

## Release Semver

CI validates that PR `Release: vX.Y.Z` is exactly **+1** from the latest GitHub release (patch, minor, or major — but only one increment). Invalid jumps are rejected with guidance.

## Code Conventions

- Wire concrete types only in `cmd/quark/main.go`
- Define interfaces at consumer, not provider
- Use `errors.Is` with sentinel errors, not string matching
- golangci-lint must pass (see [ci-and-release.md](ci-and-release.md))
- Interface max 5 methods (interfacebloat linter)

See also: [config-and-data.md](config-and-data.md), [tui.md](tui.md), [INDEX.md](INDEX.md).
