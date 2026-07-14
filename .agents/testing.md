# Testing

## Test Layers

| Layer | Location | Build tag | Command |
|---|---|---|---|
| Unit | Colocated `*_test.go` in all packages | (none) | `make ut` |
| Integration | e.g. `internal/integration_test.go` | `integration` | `make integration` |
| E2E TUI | `tests/e2e/tui/` | `e2e` | `make e2e-tui` |
| E2E CLI | `tests/e2e/cli/` | `e2e` | `make e2e-cli` |
| All | — | — | `make test` |

All unit tests run with `-race` detector.

## Makefile Targets

```bash
make ut              # Unit tests (-race, coverage)
make e2e-tui         # TUI model-level e2e
make e2e-cli         # CLI black-box e2e (builds coverage binary first)
make e2e             # e2e-tui + e2e-cli
make integration     # Integration tests
make test            # ut + e2e + integration
make build-cover     # Coverage-instrumented binary for CLI e2e
make lint            # golangci-lint
make lint-ci         # CI lint with timing instrumentation
make show-slow       # Top 10 slowest packages from test log
make test-coverage-report  # Regression test for coverage script
```

Pass extra args: `make ut ARGS="-v -run TestFoo"`.

Test output logged to `test-output.log` (used by `show-slow`).

Uses `gotestsum` if installed, else plain `go test`.

## Unit Tests

- Colocated with source: `internal/*/**_test.go`
- Naming: one `{file}_test.go` per source file (e.g. `view.go` → `view_test.go`). Do not split by helper/feature into `view_height_test.go`, `truncate_test.go`, etc.
- Race detector enabled (`-race`)
- Parallelism: `-p $(nproc)`
- Coverage: `-coverpkg=./...`

### Notable Patterns

- **Table-driven tests** throughout
- **`go.uber.org/goleak`** — goroutine leak detection in select tests
- **`go.uber.org/mock`** — generated mocks (e.g. `internal/search/mocks/mock_store.go`)
- **Compile-time interface checks** in `internal/store/store.go`, `internal/tui/interfaces_test.go`

## Integration Tests

Build tag: `integration`

```bash
go test -tags integration ./...
```

Cross-package tests that need real SQLite or multi-component wiring. Example: `internal/integration_test.go`.

## E2E TUI Tests

Location: `tests/e2e/tui/` (package `tui_test`)

Build tag: `e2e`

Harness: `internal/tuitest/harness.go`

```bash
go test -tags e2e ./tests/e2e/tui/...
```

### Harness Capabilities

- Temp store setup and request seeding
- Fake and real executors
- Model construction with sensible defaults
- Key/message driving helpers (`SendKey`, `SendMsg`)
- Rendered view assertions (check lipgloss output)
- **Frame overflow guard** — after every `Update` / `UpdateWithCmd` / `AssertView*`,
  the harness fails if `View()` is wider or taller than the terminal, or if the
  `Visual Overflow` status banner is showing. Opt out only for intentional
  overflow tests with `tuitest.AllowFrameOverflow(t)`.

Test files follow `e2e_*_test.go` naming:

- `e2e_journey_test.go` — full user journeys
- `e2e_auth_test.go`, `e2e_env_test.go`, `e2e_schedule_test.go`, etc.
- `harness_smoke_test.go` — harness wiring verification

### TUI Regression Tests

`internal/tui/update_test.go` — BUG-NNN regression tests (not e2e tag, run with unit tests).
Overflow height coverage is in `internal/tui/view_test.go`.
Unit tests use the same overflow guard via `callUpdate` → `tuitest.Update`.

## E2E CLI Tests

Location: `tests/e2e/cli/` (package `cli`)

Build tag: `e2e`

Runs the **actual compiled binary** with coverage instrumentation:

```bash
make e2e-cli   # builds bin/quark-cover, runs tests with GOCOVERDIR
```

### Coverage Binary Flow

1. `make build-cover` → `bin/quark-cover` (built with `-cover -coverpkg=./...`)
2. `e2e-cli-prep` copies to `bin/quark` (path tests expect)
3. Tests run with `GOCOVERDIR=bin/cover-cli`
4. `go tool covdata textfmt` converts binary coverage to text

CI may download a pre-built coverage binary artifact instead of rebuilding.

Test files: `env_test.go`, `schedule_test.go`, `keybindings_test.go`, `completion_test.go`.

## Scripts

`scripts/coverage-report.sh` — coverage diff reporting for CI.

`scripts/coverage-report_test.sh` — regression test (`make test-coverage-report`).

## Writing Tests — Guidelines

1. **Unit first** — test logic in the package where it lives
2. **Use harness for TUI flows** — don't shell out to bubbletea in unit tests
3. **CLI e2e for command contracts** — flags, output format, exit codes
4. **Avoid trivial tests** — assert meaningful behavior, not getters
5. **Race-safe** — tests must pass with `-race`
6. **No flaky timing** — use injectable `time.Now` (see `cli.NewScheduleCmd(st, e, time.Now)`)

## Test Tags Summary

```go
//go:build e2e        // tests/e2e/**
//go:build integration // internal/integration_test.go
// (no tag)            // everything else = unit tests
```

## Install Script Test

`internal/installtest/install_script_test.go` — tests `install.sh` behavior.

See also: [tui.md](tui.md), [ci-and-release.md](ci-and-release.md), [caveats.md](caveats.md).
