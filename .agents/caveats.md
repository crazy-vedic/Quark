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

`--debug` writes keystroke and diagnostic logs to `/tmp/quark_debug_logs/debug.log`
(existing files are archived as `debug_<epoch>.log` in the same directory).
Help text and implementation both use this path.

## Security

### Plaintext credentials

Bearer tokens, Basic passwords, API keys, and Authorization headers are stored **unencrypted** in SQLite (`requests.auth_config`, headers JSON). Backups are full DB copies.

Protection is filesystem permissions only:

- Config dir: `0700`
- Backup files: `0600`
- Response temp files: `0600` in `/tmp`

Do not add features that log or transmit credentials. Do not weaken directory permissions.

### Response temp files

Large HTTP response bodies stream to `/tmp/quark_*` temp files for in-process
display/memory offload. Must call `ExecuteResult.Cleanup()` when finished rendering.
TUI cleans up on new response (BUG-007). Orphan temp files after crashes are harmless.

**SQLite persistence is independent of temp files.** For saved requests (`req.ID != ""`),
`Executor.Execute` writes the full response to the `executions` table synchronously
*before* returning to CLI/TUI (re-reading the temp path when streamed). Mid-download
crashes cannot produce a history row. Unsaved TUI sends (`req.ID == ""`) skip history.

## Scheduling

**Not a daemon.** `quark schedule add` only writes a DB row. Nothing fires automatically unless:

- `quark schedule run-due` is invoked (cron, systemd timer, CI), or
- TUI in-app timer checks due runs

Design implication: do not assume background scheduling exists in CLI-only deployments.

## Database

### Single connection

`db.SetMaxOpenConns(1)` — all DB access serialized within one process. Required to preserve SQLite PRAGMA settings across queries.

`PRAGMA busy_timeout = 5000` waits up to 5 seconds on `SQLITE_BUSY` when another
process holds the lock. WAL + busy_timeout mitigate multi-process contention but
do not eliminate `database is locked` under heavy concurrent writers.

### Migrations are append-only

Never reorder or remove entries in `internal/store/migrations.go`. Add new migrations at the end with incrementing version numbers. Existing user DBs depend on version ordering.

## Environment / CLI Gaps

- `quark env active` persists the active collection environment for TUI and `quark run`
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

Regression tests live mainly in `internal/tui/update_test.go` (overflow in `view_test.go`).
Do not reintroduce these:

| ID | Summary | Key file |
|---|---|---|
| BUG-001 | Clear `editingURL` on import modal Escape | `update_test.go` |
| BUG-002 | Never render binary content raw in terminal | `view.go` |
| BUG-003 | No double "invalid URL:" error prefix | `executor.go` |
| BUG-004 | Stub keys must give user feedback | `update_test.go` |
| BUG-007 | Clean up old streamed response temp file | `view.go`, `update_test.go` |
| BUG-008 | Distinguish "not searched" vs "no results"; Esc cancels search | `update.go`, `view.go` |
| BUG-009 | `q` in help mode closes help, not app | `update_test.go` |
| BUG-010 | Brief tmux/screen Ctrl+w warning on startup | `model.go` |
| BUG-011 | Sidebar scroll offset for long collection lists | `model.go` |

### Visual overflow

If the TUI status bar shows `Visual Overflow; Please check --debug logs`, capture
`/tmp/quark_debug_logs/debug.log` (run with `--debug`) and report it. Detection
covers both height and width (`lipgloss.Height` / `lipgloss.Width` vs the
terminal). Realtime auto-layout fix is not attempted on every frame; prevention
(truncate, clip, density tiers, absurd-size screen) is preferred.

Density tiers (auto from size, or force with `--dim`):
- **wide** (≥80×18): side-by-side; sidebar shrink ladder
- **narrow**: stacked panes
- **tiny**: single focused pane
- **absurd** (&lt;24×8): “Terminal too small” (or forced `--dim=absurd`)

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
