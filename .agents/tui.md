# TUI

The interactive terminal UI is built with bubbletea (Elm architecture: Model / Update / View) in `internal/tui/`.

## Core Files

| File | Role |
|---|---|
| `model.go` | State struct, initialization, mode constants |
| `update.go` | Message handling, state transitions |
| `view.go` | Rendering (lipgloss styling) |
| `request_editors.go` | URL, method, headers, body editing |
| `auth_editor.go` | Auth type/config editing |
| `env_mode.go` | Environment variable management |
| `list_viewport.go` | Scrollable list component |
| `interfaces.go` | Narrow interfaces for executor, searcher, importer |
| `test_api.go` | Test-only helpers exported for harness |
| `mouse.go` | Normal-mode mouse hit-testing and handlers |
| `layout.go` | Shared pane geometry for view + mouse |
| `overflow_debug.go` | Visual overflow detection + `--debug` logging |

## Mouse Support

Enabled via `tea.WithMouseCellMotion()` in `launchTUI`. Mouse is active only in
**normal mode** (overlay modes stay keyboard-only).

| Area | Mouse behavior |
|---|---|
| Sidebar | Click select/expand; wheel scroll |
| Request pane | Method badge, URL, send, editors, body wheel |
| Response pane | Tab bar clicks (Body/Headers/Raw); wheel cycles execution history; history popup row clicks when viewing older runs |

## Model Structure

The TUI model holds:

- **Navigation:** selected collection, request, sidebar scroll offset
- **Panes:** sidebar (collections/requests), request editor, response viewer
- **Modes:** normal, search, help, import, env, schedule, auth editing
- **Execution state:** in-flight request, last result, temp file path
- **UI state:** error messages, tmux warning flag, search query/results

Constructed via `tui.New(tui.Deps{...})` — all dependencies injected from `launchTUI` in `main.go`.

## Dependencies (`tui.Deps`)

The store satisfies multiple interfaces:

```go
Lister, Reader, Writer, ColWriter          // collections + requests
ExecutionReader                             // history
EnvReader, EnvWriter, ActiveEnvStore       // environments
Scheduler                                   // scheduled runs
Executor        RequestExecutor              // HTTP dispatch
Searcher        RequestSearcher              // fuzzy search
Importer        CurlImporter                 // cURL parse
Config          config.Config
Resolver        *keybindings.Resolver
Ctx             context.Context              // signal-aware
DebugLog        *os.File
ConfigDir       string
```

## Modes

| Mode | Enter | Exit | Purpose |
|---|---|---|---|
| Normal | (default) | — | Browse, edit, run requests |
| Search | `s` | Esc | Fuzzy search across requests |
| Help | `?` | `q` or Esc | Keybinding reference |
| Import | import key | Esc | Paste cURL command |
| Env | `e` | Esc | Manage environment variables |
| Schedule | schedule key | Esc | View/manage scheduled runs |
| Auth editor | auth key | Esc | Configure request auth |

Exact keys are configurable — see keybindings section.

## Keybindings (`internal/keybindings/`)

| File | Purpose |
|---|---|
| `defaults.go` | Default action→key map |
| `actions.go` | Action name constants |
| `entries.go` | Grouped listing for help/CLI |
| `resolver.go` | Resolve key press → action |
| `hints.go` | Footer hint strings |

User overrides stored in `config.toml` `[keybindings]` section. Managed via `quark keybindings` CLI commands (not in-TUI).

`keybindings.NewResolver(cfg.Keybindings)` passed to TUI at launch.

## Response Rendering

- JSON bodies: syntax-highlighted via `internal/highlight` (Chroma → ANSI)
- Large/streamed bodies: read from temp file path
- Binary content: must NOT render raw (BUG-002) — shows placeholder instead
- Status line: method, URL, status code, timing

## Execution Flow

1. User triggers run (Enter or run keybinding)
2. Model sends execute message
3. `RequestExecutor.Execute(ctx, req)` called asynchronously (tea.Cmd)
4. Result message updates response pane
5. Old temp file cleaned up on new response (BUG-007)

Context from `Deps.Ctx` propagates cancellation on SIGINT.

## Schedule in TUI

TUI has an in-app timer that checks for due scheduled runs (unlike CLI which requires `schedule run-due`). Missed runs may show retry warnings.

## External Editor

Config `ui.editor` (default `$EDITOR`, fallback `vim`) for editing request body in external editor.

## Test Harness (`internal/tuitest/harness.go`)

Used by `tests/e2e/tui/` for model-level tests:

- Temp store setup and request seeding
- Fake and real executors
- Model construction with defaults
- Key/message driving helpers
- Rendered view assertions

Production TUI code stays in `internal/tui/`; harness is separate to keep production package focused.

## Regression Tests (BUG-NNN)

Known UX bugs tracked as **BUG-NNN** IDs (not literal TODO/FIXME comments).
Most live in `internal/tui/update_test.go`; overflow height checks are in `view_height_test.go`.

| ID | Issue | Source ref |
|---|---|---|
| BUG-001 | `editingURL` not cleared on import modal Escape | `update_test.go` |
| BUG-002 | Binary content must not render raw | `view.go` |
| BUG-003 | Double "invalid URL:" prefix | `executor.go`, `update_test.go` |
| BUG-004 | Stub keys showed no feedback | `update_test.go` |
| BUG-007 | Old streamed temp file not cleaned up | `view.go`, `update_test.go` |
| BUG-008 | "No results" before search; search cancel on Esc | `update.go`, `view.go`, `update_test.go` |
| BUG-009 | `q` in help mode closes help (not quit) | `update_test.go` |
| BUG-010 | tmux/GNU screen Ctrl+w warning on startup | `model.go`, `view.go` |
| BUG-011 | Sidebar scroll offset for long lists | `model.go` |

When fixing TUI bugs, add/update tests with the BUG-NNN reference (prefer `update_test.go`).

## Visual Overflow Reporting

If a normal-mode render still exceeds terminal height after clipping, the status bar shows
`Visual Overflow; Please check --debug logs` and a detailed `VISUAL OVERFLOW` block is
written to `/tmp/quark_debug_logs/debug.log` (when `--debug` is set). There is no automatic
layout rewrite — report occurrences with the debug log attached.

## TUI vs CLI Gaps

- Postman import: CLI only (`import-postman`)
- Keybinding changes: CLI only (`keybindings set`)
- `env active`: CLI persists active env used by both TUI and `quark run`

See also: [cli-commands.md](cli-commands.md), [http-execution.md](http-execution.md), [testing.md](testing.md), [caveats.md](caveats.md).
