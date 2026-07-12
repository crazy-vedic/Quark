# CLI Commands

All subcommands are registered in `cmd/quark/main.go` and implemented in `internal/cli/`. CLI handlers are thin — they parse args, call store/exec, and print output. No business logic beyond orchestration.

Persistent flags (all subcommands): `--debug`, `--config <dir>`.

## Command Tree

```
quark                          # Default: launch TUI
quark tui                      # Launch TUI explicitly
quark collection list
quark collection create <name>
quark request list [--collection <id>]
quark run <Collection/Request>
quark search <query>
quark schedule add <Collection/Request> --at <time>
quark schedule list
quark schedule run-due
quark import curl <curl-command> [--collection <id>] [--name <name>]
quark import-postman <file|dir> [--collection-name <name>] [--on-duplicate <action>]
quark env list [<collection-id>]
quark env create <collection-id> <name>
quark env set <collection-id> <env-name> <key> <value>
quark env set-global <key> <value>
quark env delete <collection-id> <env-name>
quark env active <collection-id> <env-name>
quark keybindings list
quark keybindings set <action> <key>
quark keybindings reset
quark warp-completion           # Warp terminal shell completion plugin
```

## collection

**Source:** `internal/cli/collection.go`

| Subcommand | Args | Description |
|---|---|---|
| `list` | — | List all collections (ID, name) |
| `create` | `<name>` | Create a new collection |

## request

**Source:** `internal/cli/request.go`

| Subcommand | Flags | Description |
|---|---|---|
| `list` | `--collection <id>` | List requests in a collection |

Shell completion available for `--collection` via `CompleteCollectionIDs`.

## run

**Source:** `internal/cli/run.go`

```
quark run <Collection/Request>
```

Executes a saved request by path (collection name / request name). Uses `exec.Executor.Execute`. Prints status, headers, body (or temp file path for large bodies).

Shell completion via `CompleteRequestPaths`.

## search

**Source:** `internal/cli/search.go`

```
quark search <query>
```

Fuzzy search across all collections. Uses `internal/search.Searcher`.

## schedule

**Source:** `internal/cli/schedule.go`

| Subcommand | Args/Flags | Description |
|---|---|---|
| `add` | `<Collection/Request> --at <time>` | Schedule a future run |
| `list` | — | List scheduled runs |
| `run-due` | — | Execute all pending runs due now |

**`--at` formats** (parsed by `internal/schedule/parse.go`):

- Go duration: `30s`, `10m`, `1h`
- Relative: `in 10m`, `in 2h`
- RFC3339: `2026-07-03T15:00:00Z`
- Local datetime: `2026-07-03 15:00`

**Important:** Scheduling is **not a daemon**. `schedule add` only persists a row. Due runs execute when:

- `quark schedule run-due` is invoked (e.g. via cron), or
- The TUI's in-app timer fires

## import

**Source:** `internal/cli/importcmd.go`

```
quark import curl <curl-command> [--collection <id>] [--name <name>]
```

Without `--collection` and `--name`, performs a **dry run** (prints parsed method/URL/warnings, does not save).

Flags:

| Flag | Required to save | Description |
|---|---|---|
| `--collection` | Yes | Target collection ID |
| `--name` | Yes | Request name |

## import-postman

**Source:** `internal/cli/import_postman.go`

```
quark import-postman <collection.json|directory>
```

Imports Postman Collection v2.1 JSON or a bulk export directory (collections + environment files).

| Flag | Default | Description |
|---|---|---|
| `--collection-name`, `-n` | (from file) | Override imported collection name |
| `--on-duplicate` | `duplicate` | Action when name exists: `replace`, `duplicate`, `merge`, `skip` |

Interactive prompts may appear for duplicate handling when run in a TTY. Uses transactions for atomic imports.

See [imports.md](imports.md) for mapping limits.

## env

**Source:** `internal/cli/env.go`

| Subcommand | Args | Description |
|---|---|---|
| `list` | `[<collection-id>]` | List environments (all or per collection) |
| `create` | `<collection-id> <name>` | Create environment for collection |
| `set` | `<collection-id> <env-name> <key> <value>` | Set key in collection environment |
| `set-global` | `<key> <value>` | Set key in global environment |
| `delete` | `<collection-id> <env-name>` | Delete environment |
| `active` | `<collection-id> <env-name>` | Set active environment (persists for TUI and `quark run`) |

**Note:** `env active` writes to `collection_active_env`. CLI `run` and the TUI both resolve variables via the active env → `default` → first env chain.

Every collection gets a `default` environment on store init. A `global` environment always exists.

## keybindings

**Source:** `internal/cli/keybindings.go`

Does **not** require store initialization — reads/writes `config.toml` directly.

| Subcommand | Args | Description |
|---|---|---|
| `list` | — | Show current keybindings grouped by category |
| `set` | `<action> <key>` | Set binding (validates action exists, checks conflicts) |
| `reset` | — | Reset all to defaults |

Persists via `config.SaveKeybindings` (preserves other config sections).

## warp-completion

**Source:** `internal/cli/warp_completion.go`

Generates Warp terminal shell completion plugin JSON. No store access.

## Shell Completion

Several commands register `ValidArgsFunction` for cobra completion:

- `CompleteCollectionIDs` — collection list
- `CompleteRequestPaths` — collection/request path
- `CompleteCollectionThenEnvironment` — collection then environment name

Used by lazy wrappers in `main.go`.

## CLI vs TUI

| Feature | CLI | TUI |
|---|---|---|
| Run requests | `quark run` | Enter / shortcut |
| Manage collections | `collection` commands | Sidebar |
| Environments | `env` commands | Env mode (`e`) |
| Import cURL | `import curl` | Import modal |
| Import Postman | `import-postman` | Not available in TUI |
| Search | `search` | Search mode (`s`) |
| Schedule | `schedule` commands | Schedule mode |
| Keybindings | `keybindings` commands | Not configurable in TUI (use CLI) |

See also: [entrypoints.md](entrypoints.md), [imports.md](imports.md), [config-and-data.md](config-and-data.md).
