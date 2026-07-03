# HTTP Execution

The execution engine lives in `internal/exec/`. It dispatches HTTP requests, handles variable interpolation, applies auth, runs plugin hooks, streams large responses, and records execution history.

## Executor

```go
type Executor struct {
    client            *http.Client
    timeout           time.Duration
    maxResponseSize   int
    preRequestHooks   []PreRequestHook
    postResponseHooks []PostResponseHook
    variableResolver  VariableResolver
    executionWriter   ExecutionWriter
}
```

Constructed via `exec.New(transport, opts...)`:

| Option | Purpose |
|---|---|
| `WithTimeout(d)` | Per-request context timeout (default from config: 30s) |
| `WithMaxResponseSize(n)` | Threshold for streaming to temp file |
| `WithVariableResolver(fn)` | Env var lookup for `{{VAR}}` substitution |
| `WithExecutionWriter(w)` | Persist execution history |
| `WithPreRequestHooks(hooks)` | Pre-request chain |
| `WithPostResponseHooks(hooks)` | Post-response chain |
| `WithLogger(l)` | slog logger |

Production uses a custom `http.Transport` with `ResponseHeaderTimeout` matching config timeout. Tests inject `httptest` transports.

## Execute Pipeline

`Executor.Execute(ctx, req) (*ExecuteResult, error)` runs in order:

1. **Pre-request hooks** — registration order; any error aborts (recorded in history)
2. **Variable substitution** — if `VariableResolver` configured
3. **Build HTTP request** — validate URL scheme, apply headers + auth
4. **HTTP dispatch** — `client.Do` with per-request `context.WithTimeout`
5. **Read response** — in-memory or stream to temp file if over size text/plain threshold
6. **Post-response hooks** — errors logged via `slog.Warn`, chain continues
7. **Record execution** — best-effort history write

### Error Semantics

**HTTP 4xx/5xx are NOT Go errors.** They are valid responses — check `ExecuteResult.StatusCode`.

Errors returned only for:

- Network failures
- Timeouts (`exec.ErrTimeout`)
- Context cancellation (`exec.ErrRequestCancelled`)
- Invalid URL (`exec.ErrInvalidURL`)
- Unmatched variable (`exec.ErrUnresolvedVariable`)
- Pre-request hook failures

`classifyError` maps `context.Canceled` / `DeadlineExceeded` to sentinels.

### Execution History

`recordExecution` uses `context.WithoutCancel(ctx)` so history is written even when the request context is cancelled. Stores full request snapshot + response metadata.

## URL Validation

`buildHTTPRequest` rejects non-HTTP schemes:

```go
// Only http and https allowed — rejects file://, gopher://, etc.
```

Malformed headers JSON → warn and send without custom headers (degraded, not fatal).

## Response Handling

Small bodies: read into memory in `ExecuteResult.Body`.

Large bodies (over `maxResponseSize`):

- Streamed to temp file in `/tmp` (chmod `0600`)
- `ExecuteResult.TempPath` set
- Caller **must** call `ExecuteResult.Cleanup()` to remove temp file
- TUI cleans up old temp files on new response (BUG-007)

## Variable Interpolation (`variables.go`)

Postman-style syntax:

| Pattern | Meaning |
|---|---|
| `{{VAR}}` | Named variable |
| `{{1}}` | Positional variable (1-indexed) |
| `{{1\|merchant_id}}` | Positional with named fallback |

Applied to URL, headers, and body.

### Resolution Precedence

1. Positional overrides (if any)
2. Named variables from positional map
3. Collection environment (active env vars)
4. Global environment vars

`ResolveEnvVars(ctx, st, activeEnvID, collectionID)` implements the chain:

- Active env (from `collection_active_env`) if set
- Else `default` env for collection
- Else first collection environment
- Global env as fallback for vars not in collection env

Unresolved variables → `ErrUnresolvedVariable`.

## Authentication (`auth.go`)

Applied after variable interpolation, before dispatch:

| AuthType | Behavior |
|---|---|
| Bearer | `Authorization: Bearer <token>` |
| Basic | `Authorization: Basic <base64(user:pass)>` |
| APIKey | Header or query param per `AuthConfig.In` |

Auth config stored as JSON in `Request.AuthConfig`.

## Plugin Hooks (`hooks.go`)

```go
type PreRequestHook interface {
    BeforeRequest(ctx context.Context, req *domain.Request) (*domain.Request, error)
}
type PostResponseHook interface {
    AfterResponse(ctx context.Context, req *domain.Request, result *ExecuteResult) (*ExecuteResult, error)
}
```

Adapted from `internal/plugin` registry in `main.go`. Registry is empty today.

## ExecuteResult (`result.go`)

Key fields:

```go
type ExecuteResult struct {
    StatusCode      int
    Headers         http.Header
    Body            []byte
    TempPath        string  // non-empty if body streamed to file
    ResponseTime    time.Duration
    ContentType     string
}
```

Methods: `Cleanup()` removes temp file if set.

## Sentinel Errors

```go
exec.ErrTimeout
exec.ErrRequestCancelled
exec.ErrInvalidURL
exec.ErrUnresolvedVariable
```

Always use `errors.Is` for checks.

## CLI Integration

`quark run` (`internal/cli/run.go`):

1. Resolve collection/request by path
2. Call `executor.Execute(ctx, req)`
3. Print status, headers, body (or temp file notice)
4. Exit non-zero on execution error (not on 4xx/5xx)

## TUI Integration

TUI calls `RequestExecutor.Execute` via the narrow interface in `internal/tui/interfaces.go`. Results rendered in response pane with syntax highlighting for JSON.

See also: [domain-and-store.md](domain-and-store.md), [config-and-data.md](config-and-data.md), [caveats.md](caveats.md).
