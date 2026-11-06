# E2E Tests

Run all e2e tests with: `go test -tags e2e ./tests/e2e/...`

## Structure

```
tests/e2e/
├── cli/
│   └── keybindings_test.go      # CLI keybindings e2e tests (package: cli)
├── tui/
│   ├── harness_smoke_test.go    # Harness wiring smoke test
│   └── e2e_*_test.go            # TUI flow/integration tests (package: tui_test)
└── README.md                     # This file
```

## Harness layout

`internal/tuitest/` is the reusable model-level TUI harness used by `tests/e2e/tui/`.
It centralizes:

- temp store setup and request seeding
- fake and real executors
- model construction with sensible defaults
- key/message driving helpers
- rendered-view assertions

`internal/tui/` now stays focused on production code plus colocated unit tests.
