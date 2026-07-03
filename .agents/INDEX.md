# Quark Agent Index

**Start here.** Load only the topic files you need for your task.

## Summary

Quark is a **local-first, keyboard-driven HTTP client** (Postman/Insomnia alternative) built in Go 1.26. A single binary (`quark`) provides a bubbletea TUI (default) and a cobra CLI for automation. All data lives in local SQLite (`modernc.org/sqlite`, CGO-free). There are no cloud services — only user-initiated HTTP(S) outbound traffic.

Quark is **not** a web app, API server, or multi-user system. It is a terminal tool with one DI seam at `cmd/quark/main.go`.

## Critical Rules

1. **Wire concrete types only in `cmd/quark/main.go`** — all other packages consume narrow interfaces defined at the consumer side.
2. **Migrations are append-only** — never reorder or remove entries in `internal/store/migrations.go`; add new migrations at the end.
3. **HTTP 4xx/5xx are not Go errors** — only network failures, timeouts, and cancellation return errors from `exec.Executor.Execute`.
4. **Config dir default is `./.quark`** (CWD-relative), not `~/.quark` as README suggests — always respect `--config`.
5. **Credentials are stored plaintext** in SQLite; security relies on filesystem permissions (`0700` dir, `0600` files).

## Task Router

| If you are… | Load these files |
|---|---|
| New to the repo | [overview.md](overview.md), [architecture.md](architecture.md) |
| Adding/fixing a CLI command | [entrypoints.md](entrypoints.md), [cli-commands.md](cli-commands.md), [architecture.md](architecture.md) |
| Working on TUI / keybindings | [tui.md](tui.md), [caveats.md](caveats.md), [testing.md](testing.md) |
| Changing HTTP execution / auth / vars | [http-execution.md](http-execution.md), [domain-and-store.md](domain-and-store.md) |
| Changing DB schema / persistence | [domain-and-store.md](domain-and-store.md), [testing.md](testing.md), [caveats.md](caveats.md) |
| Import (cURL / Postman) | [imports.md](imports.md), [domain-and-store.md](domain-and-store.md) |
| Config / data paths / security | [config-and-data.md](config-and-data.md), [caveats.md](caveats.md) |
| Writing or fixing tests | [testing.md](testing.md) + topic file for the area under test |
| CI, coverage, or release PR | [ci-and-release.md](ci-and-release.md) |
| Debugging known UX bugs | [caveats.md](caveats.md), [tui.md](tui.md) |

## File Manifest

| File | Description | Key source paths |
|---|---|---|
| [overview.md](overview.md) | Mission, stack, repo layout, build/install | `README.md`, `go.mod`, `Makefile` |
| [architecture.md](architecture.md) | DI seam, interface segregation, package map | `cmd/quark/main.go`, `internal/tui/interfaces.go`, `internal/store/store.go` |
| [entrypoints.md](entrypoints.md) | Binary entry, lazy runtime, TUI launch, signals | `cmd/quark/main.go` |
| [cli-commands.md](cli-commands.md) | Full CLI subcommand reference | `cmd/quark/main.go`, `internal/cli/` |
| [domain-and-store.md](domain-and-store.md) | Domain types, schema, migrations, backups | `internal/domain/`, `internal/store/` |
| [http-execution.md](http-execution.md) | Executor pipeline, variables, auth, streaming | `internal/exec/` |
| [tui.md](tui.md) | Model/Update/View, modes, keybindings, test harness | `internal/tui/`, `internal/keybindings/`, `internal/tuitest/` |
| [imports.md](imports.md) | cURL and Postman import behavior and limits | `internal/curl/`, `internal/postman/`, `internal/cli/import*.go` |
| [config-and-data.md](config-and-data.md) | config.toml, data dirs, env vars, security | `internal/config/config.go` |
| [testing.md](testing.md) | Unit/integration/e2e, Makefile targets, build tags | `Makefile`, `tests/e2e/` |
| [ci-and-release.md](ci-and-release.md) | CI jobs, semver release, PR checklist | `.github/workflows/`, `.golangci.yaml` |
| [caveats.md](caveats.md) | Gotchas, doc mismatches, BUG-NNN refs | Various — see file |
