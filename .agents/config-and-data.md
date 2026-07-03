# Config & Data

## Config Directory

Controlled by `--config` flag (default: `~/.quark`).

Use `--config` to override the config/data directory location. See [caveats.md](caveats.md).

Created with mode `0700` (owner-only) because it holds credentials.

### Contents

```
<config-dir>/
├── config.toml       # Optional user configuration
├── quark.db          # SQLite database (WAL mode)
├── quark.log         # Application log (default path)
├── backup/           # Auto-backup copies of quark.db
│   └── quark.db.YYYY-MM-DD-NNN
└── debug.log         # (NOT used — debug goes to /tmp; see below)
```

## config.toml (`internal/config/config.go`)

Optional. All fields have defaults via `config.Default(configDir)`.

Loaded with merge semantics: only explicitly-set TOML fields override defaults (avoids bool zero-value clobbering for `FollowRedirects`, `AutoBackup`).

### Sections

#### `[ui]`

| Field | Default | Values |
|---|---|---|
| `theme` | `auto` | `auto`, `dark`, `light`, `transparent` |
| `default_method` | `GET` | HTTP method for new requests |
| `editor` | `$EDITOR` or `vim` | External editor for body editing |

#### `[http]`

| Field | Default | Description |
|---|---|---|
| `timeout` | `30s` | Request timeout (Go duration syntax) |
| `follow_redirects` | `true` | Follow HTTP redirects |
| `max_redirects` | `10` | Max redirect hops |

#### `[logging]`

| Field | Default | Description |
|---|---|---|
| `level` | `info` | `debug`, `info`, `warn`, `error` |
| `file` | `<config-dir>/quark.log` | Log file path |
| `max_size` | `10MB` | Log rotation size |

#### `[backup]`

| Field | Default | Description |
|---|---|---|
| `auto_backup` | `true` | Backup DB before writes |
| `keep_last` | `10` | Retained backup count |

#### `[keybindings]`

User keybinding overrides. Managed via `quark keybindings set/reset`. Saved by `config.SaveKeybindings` which preserves other config sections.

### Config API

```go
config.Load(configDir) (Config, error)       // missing file → defaults, not error
config.Default(configDir) Config
config.SaveKeybindings(configDir, binds) error
cfg.Timeout() time.Duration
cfg.BackupDir(dataDir) string                // → dataDir/backup
```

Load failure (unreadable file) logs `slog.Warn` and falls back to defaults in `main.go`.

## Environment Variables

| Variable | Used by | Purpose |
|---|---|---|
| `$EDITOR` | `config.Default` | External editor fallback |
| `$GITHUB_TOKEN` | `install.sh` | Private repo download auth |
| `$GITHUB_ACCESS_TOKEN` | `install.sh` | Alternative to GITHUB_TOKEN |

No other env vars configure runtime behavior. HTTP env vars for requests come from Quark's environment system (SQLite), not OS env — except CLI runs resolve via the store's env chain.

## Debug Logging

`--debug` flag enables keystroke logging:

- **Actual path:** `/tmp/quark_debug_logs/debug.log`
- Archives previous log as `debug_<epoch>.log`
- **Help text says:** `~/.quark/debug.log` (incorrect)

Debug logging is best-effort — failures silently disable logging.

## Security Model

### Credential Storage

Bearer tokens, Basic auth passwords, and API keys are stored **plaintext** in SQLite:

- `requests.auth_config` JSON column
- Request headers may contain Authorization values
- Backups are full DB copies including credentials

Protection relies on:

- Config dir `0700`
- Backup files `0600`
- Response temp files in `/tmp` chmod `0600`

There is no encryption at rest.

### URL Scheme Restriction

Only `http://` and `https://` URLs are allowed for execution. Prevents SSRF-style attacks via `file://`, etc.

### Single DB Connection

`SetMaxOpenConns(1)` serializes DB access within one process. Multiple concurrent `quark` processes on the same DB file can contend (WAL helps but doesn't eliminate conflicts).

## Data Lifecycle

| Event | Effect |
|---|---|
| First run | Creates config dir, DB, migrations, global + default envs |
| Request run | Optional backup, execution history recorded |
| Request delete | Execution history retained (no FK) |
| Collection delete | Cascades to requests, environments, scheduled runs |
| Config change | Keybindings saved atomically; other sections preserved |

See also: [entrypoints.md](entrypoints.md), [domain-and-store.md](domain-and-store.md), [caveats.md](caveats.md).
