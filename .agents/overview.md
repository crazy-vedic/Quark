# Overview

## Mission

Quark is a **local-first, keyboard-driven HTTP client**. It is a terminal-native TUI that keeps everything in a local SQLite database, with a scriptable CLI for automation and CI. No cloud, no accounts, no telemetry — only the HTTP requests the user explicitly makes.

Think Postman or Insomnia, but entirely in the terminal.

## Module & Stack

| Item | Value |
|---|---|
| Go module | `github.com/crazy-vedic/quark` |
| Go version | 1.26.0 |
| License | MIT |
| Binary name | `quark` |
| Entrypoint | `cmd/quark/main.go` |

### Key Dependencies

| Package | Purpose |
|---|---|
| `charmbracelet/bubbletea` + `bubbles` + `lipgloss` | TUI framework |
| `spf13/cobra` | CLI command tree |
| `modernc.org/sqlite` | CGO-free SQLite driver |
| `alecthomas/chroma/v2` | Syntax highlighting for response bodies |
| `BurntSushi/toml` | Config file parsing |
| `google/uuid` | ID generation |
| `mattn/go-isatty` | TTY detection |

Test-only: `stretchr/testify`, `go.uber.org/mock`, `go.uber.org/goleak`.

## Repository Layout

```
quark/
├── cmd/quark/           # Single binary entrypoint (DI wiring only)
├── internal/            # All application code (~17 packages)
│   ├── domain/          # Shared data types (zero internal deps)
│   ├── store/           # SQLite repository
│   ├── exec/            # HTTP execution engine
│   ├── tui/             # Bubbletea TUI
│   ├── cli/             # Non-TUI subcommand handlers
│   ├── curl/            # cURL parser/importer
│   ├── postman/         # Postman v2.1 importer
│   ├── search/          # Fuzzy search
│   ├── keybindings/     # Dynamic keybinding resolution
│   ├── config/          # config.toml loading
│   ├── plugin/          # Pre/post request hook interfaces
│   ├── highlight/       # Chroma ANSI highlighting
│   ├── schedule/        # Schedule time parsing
│   └── tuitest/         # TUI test harness (used by e2e)
├── tests/e2e/           # Black-box CLI + model-level TUI tests
├── scripts/             # coverage-report.sh + regression test
├── .github/workflows/   # CI + release automation
├── Makefile             # Build, test, lint targets
├── install.sh           # Unix installer (curl \| bash)
├── install.ps1          # Windows/PowerShell installer
└── README.md            # User-facing docs
```

Gitignored runtime dirs: `bin/`, `.quark/` (local DB when using default config).

## Build & Run

```bash
make build       # → bin/quark
make run         # go run ./cmd/quark (pass ARGS="...")
make install     # go install to GOPATH/bin
make clean       # Remove bin/ and go cache
```

Install from source requires Go 1.26+ and a 256-color terminal.

One-liner install (macOS/Linux/Git Bash):

```bash
curl -sL https://raw.githubusercontent.com/crazy-vedic/Quark/main/install.sh | bash
```

Private repos: set `GITHUB_TOKEN` or `GITHUB_ACCESS_TOKEN` for the install script.

## User-Facing Docs

- `README.md` — installation, quick start, Postman migration, development commands
- `tests/e2e/README.md` — e2e test structure and harness layout
- `.github/pull_request_template.md` — PR checklist including agent doc updates

## Agent Docs

This directory (`.agents/`) is the agent knowledge base. Always start with [INDEX.md](INDEX.md).
