# Entrypoints

## Single Binary

There is **one** entrypoint: `cmd/quark/main.go`.

```
main() → run() → root.Execute()
```

On error, `run()` returns an error printed as `quark: %v\n` to stderr with exit code 1.

No other `cmd/` packages exist.

## Root Command

```go
&cobra.Command{
    Use:   "quark",
    Short: "A keyboard-driven TUI HTTP client",
    Long:  "Quark — local-first, keyboard-driven HTTP client. No cloud dependencies.",
}
```

### Persistent Flags

| Flag | Default | Effect |
|---|---|---|
| `--debug` | `false` | Enables keystroke debug logging via `cli.DebugLogger` |
| `--config` | `./.quark` | Directory for `config.toml`, `quark.db`, backups, logs |

**Caveat:** `--debug` help text says `~/.quark/debug.log` but the actual path is `/tmp/quark_debug_logs/debug.log`. See [caveats.md](caveats.md).

### Default Action

When no subcommand is given, Quark launches the TUI:

```go
root.Args = cobra.NoArgs  // rejects unknown positional args
root.RunE = func(...) { return launchTUI(...) }
```

Equivalent to `quark tui`.

## Lazy Runtime Initialization

Store and dependencies are **not** opened at startup. `runtimeOnce()` initializes on first use:

```go
var rt *runtime
runtimeOnce := func() (*runtime, error) {
    if rt != nil { return rt, nil }
    // config.Load → MkdirAll → store.New → exec.New → curl/search
    rt = &runtime{...}
    return rt, nil
}
```

Why: `--config` must be parsed before opening the DB. Also, `quark --help` should not touch the database.

Commands that need the store use `lazy*` wrapper functions in `main.go` that call `runtimeOnce()` before delegating to `internal/cli`.

**Exception:** `keybindings` subcommands load config directly via `configDir` without opening the store.

## Signal-Aware Context

```go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()
root.SetContext(ctx)
```

This context is passed to:

- All cobra subcommands
- `tui.Deps.Ctx` — TUI goroutines and HTTP requests cancel on Ctrl+C / SIGTERM

## Debug Logging

When `--debug` is set, `PersistentPreRun` opens the debug log:

```go
func openDebugLog() *os.File {
    // /tmp/quark_debug_logs/debug.log
    // Archives previous log as debug_<epoch>.log
    // First line: epoch timestamp
    // Returns nil on error (best-effort)
}
```

`cli.NewDebugLogger(debugLog)` is passed to TUI. When file is nil, logging is a no-op (zero overhead).

## TUI Launch (`launchTUI`)

```go
model := tui.New(tui.Deps{...})
p := tea.NewProgram(model, tea.WithAltScreen())
_, err := p.Run()
```

- `tea.ErrProgramKilled` is ignored (normal kill path)
- Other errors wrapped as `tui: %w`

The `Store` satisfies ~11 narrow interfaces in `tui.Deps`. See [architecture.md](architecture.md).

## Variable Resolver Wiring

```go
func makeVariableResolver(st *store.Store) exec.VariableResolver {
    return func(collectionID string) (colEnv, globalEnv map[string]string) {
        ctx, cancel := context.WithTimeout(context.Background(), store.EnvDBTimeout)
        defer cancel()
        activeEnvID, _ := st.GetActiveEnvironment(ctx, collectionID)
        return exec.ResolveEnvVars(ctx, st, activeEnvID, collectionID)
    }
}
```

## Plugin Hook Adapters

```go
pluginPreHooks(r *plugin.Registry) []exec.PreRequestHook
pluginPostHooks(r *plugin.Registry) []exec.PostResponseHook
```

Convert `plugin.*Hook` slices to `exec.*Hook` slices. Registry is empty in production today.

## Subcommand Registration

Registered in `main.go` (all lazy except warp-completion):

| Registration | Needs runtime? |
|---|---|
| `cli.NewWarpCompletionPluginCmd()` | No |
| `lazyCollectionCmd(runtimeOnce)` | Yes |
| `lazyRequestCmd(runtimeOnce)` | Yes |
| `lazyRunCmd(runtimeOnce)` | Yes |
| `lazySearchCmd(runtimeOnce)` | Yes |
| `lazyScheduleCmd(runtimeOnce)` | Yes |
| `lazyImportCmd(runtimeOnce)` | Yes |
| `lazyImportPostmanCmd(runtimeOnce)` | Yes |
| `lazyEnvCmd(runtimeOnce)` | Yes |
| `lazyKeybindingsCmd()` | Config only |
| `tui` command | Yes |

Lazy wrappers delegate to `internal/cli.New*Cmd()` factories. See [cli-commands.md](cli-commands.md) for full command tree.

## Build Metadata

Version info is injected at link time via Makefile `LDFLAGS`:

```
-X main.Version=...
-X main.Commit=...
-X main.BuildTime=...
```

Used by release builds; defaults to git describe or `"dev"`.

See also: [cli-commands.md](cli-commands.md), [config-and-data.md](config-and-data.md).
