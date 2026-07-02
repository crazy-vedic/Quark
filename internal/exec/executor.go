package exec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/crazy-vedic/quark/internal/domain"
)

// VariableResolver resolves environment variables for a collection.
// Returns (collectionEnv, globalEnv) or nil for each.
// The resolver is called synchronously during Execute, before building the HTTP request.
type VariableResolver func(collectionID string) (colEnv, globalEnv map[string]string)

// Executor dispatches HTTP requests and returns structured results.
type Executor struct {
	client            *http.Client // constructed once in New(); transport is shared
	timeout           time.Duration
	maxResponseSize   int
	logger            *slog.Logger
	preRequestHooks   []PreRequestHook
	postResponseHooks []PostResponseHook
	variableResolver  VariableResolver
	executionWriter   ExecutionWriter
}

// New constructs an Executor with the given transport and options.
// Pass http.DefaultTransport in production; inject httptest transports in tests.
func New(transport http.RoundTripper, opts ...Option) *Executor {
	o := defaultOptions()
	for _, opt := range opts {
		opt.apply(&o)
	}
	return &Executor{
		client:            &http.Client{Transport: transport},
		timeout:           o.timeout,
		maxResponseSize:   o.maxResponseSize,
		logger:            o.logger,
		preRequestHooks:   o.preRequestHooks,
		postResponseHooks: o.postResponseHooks,
		variableResolver:  o.variableResolver,
		executionWriter:   o.executionWriter,
	}
}

// Execute dispatches the request and returns a structured result.
// HTTP 4xx/5xx are not Go errors — they are valid responses. Check StatusCode.
// Returns nil, error only for network errors, timeouts, and context cancellation.
//
// Hook dispatch order:
//  1. Pre-request hooks fire in registration order. Any hook error aborts the request.
//  2. Variable substitution (if VariableResolver is configured).
//  3. HTTP dispatch.
//  4. Post-response hooks fire in registration order. Hook errors are logged via
//     slog.Warn; the next hook still fires regardless.
func (e *Executor) Execute(ctx context.Context, req *domain.Request) (*ExecuteResult, error) {
	startedAt := time.Now()
	record := func(snapshot *domain.Request, result *ExecuteResult, execErr error) {
		e.recordExecution(ctx, snapshot, result, execErr, startedAt, time.Now())
	}

	// 1. Pre-request hook chain.
	for _, h := range e.preRequestHooks {
		var err error
		req, err = h.BeforeRequest(ctx, req)
		if err != nil {
			wrapped := fmt.Errorf("exec: pre-request hook: %w", err)
			record(req, nil, wrapped)
			return nil, wrapped
		}
	}

	// 2. Variable substitution (if resolver is configured).
	if e.variableResolver != nil {
		colEnv, globalEnv := e.variableResolver(req.CollectionID)
		interpolated, err := InterpolateRequest(req, colEnv, globalEnv)
		if err != nil {
			if errors.Is(err, ErrUnresolvedVariable) {
				wrapped := fmt.Errorf("exec: %w", err)
				record(req, nil, wrapped)
				return nil, wrapped
			}
			wrapped := fmt.Errorf("exec: variable substitution: %w", err)
			record(req, nil, wrapped)
			return nil, wrapped
		}
		req = interpolated
	}

	httpReq, err := buildHTTPRequest(ctx, req)
	if err != nil {
		// BUG-003: buildHTTPRequest already wraps ErrInvalidURL; don't double-wrap.
		// errors.Is(err, ErrInvalidURL) still works via chain.
		wrapped := fmt.Errorf("exec: build request: %w", err)
		record(req, nil, wrapped)
		return nil, wrapped
	}

	// Apply per-request timeout on top of any deadline already in ctx.
	timeoutCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	httpReq = httpReq.WithContext(timeoutCtx)

	// 3. HTTP dispatch.
	start := time.Now()
	resp, err := e.client.Do(httpReq)
	elapsed := time.Since(start)

	if err != nil {
		classified := e.classifyError(err)
		record(req, nil, classified)
		return nil, classified
	}
	defer resp.Body.Close()

	result, err := e.readResponse(resp, elapsed)
	if err != nil {
		record(req, nil, err)
		return nil, err
	}

	// 4. Post-response hook chain. Errors are warnings — next hook still fires.
	for _, h := range e.postResponseHooks {
		if herr := h.AfterResponse(ctx, req, result); herr != nil {
			slog.Warn("exec: post-response hook error", "err", herr)
		}
	}

	record(req, result, nil)
	return result, nil
}

// buildHTTPRequest constructs a *http.Request from a domain.Request.
func buildHTTPRequest(ctx context.Context, req *domain.Request) (*http.Request, error) {
	// Validate scheme before handing to http.Client — reject file://, gopher://, etc.
	parsed, err := url.Parse(req.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		scheme := ""
		if parsed != nil {
			scheme = parsed.Scheme
		}
		return nil, fmt.Errorf(
			"%w: scheme %q is not allowed (must be http or https)",
			ErrInvalidURL,
			scheme,
		)
	}

	var body io.Reader
	if req.Body != "" {
		body = newStringReader(req.Body)
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, body)
	if err != nil {
		return nil, err
	}

	// Parse and apply headers stored as a flat map (JSON object in DB).
	if req.Headers != "" && req.Headers != "{}" {
		headers, err := parseHeaders(req.Headers)
		if err != nil {
			// Malformed JSON in the headers column — warn and continue without
			// headers rather than silently sending the request with none applied.
			slog.Warn("exec: failed to parse request headers; sending without headers",
				"request_id", req.ID, "err", err)
		} else {
			for k, v := range headers {
				httpReq.Header.Set(k, v)
			}
		}
	}

	if err := applyRequestAuth(httpReq, req); err != nil {
		return nil, err
	}

	return httpReq, nil
}

// readResponse reads the response body, streaming to /tmp if over threshold.
func (e *Executor) readResponse(
	resp *http.Response,
	elapsed time.Duration,
) (*ExecuteResult, error) {
	result := &ExecuteResult{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Headers:    map[string][]string(resp.Header),
		Duration:   elapsed,
	}

	lr := &io.LimitedReader{R: resp.Body, N: int64(e.maxResponseSize) + 1}
	body, err := io.ReadAll(lr)
	if err != nil {
		return nil, fmt.Errorf("exec: read body: %w", err)
	}

	result.Size = int64(len(body))

	if int(lr.N) == 0 {
		// Body exceeded threshold — write to temp file (lr.N == 0 because we
		// set it to maxResponseSize+1 and it was fully consumed).
		f, err := os.CreateTemp("", "quark-response-*.tmp")
		if err != nil {
			return nil, fmt.Errorf("exec: create temp file: %w", err)
		}
		// 0600: response bodies may contain sensitive data (tokens, PII).
		// Restrict immediately after creation before writing any content.
		if err := os.Chmod(f.Name(), 0o600); err != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
			return nil, fmt.Errorf("exec: chmod temp file: %w", err)
		}
		defer f.Close()

		// Write what we already read + remainder.
		n1, err := f.Write(body)
		if err != nil {
			return nil, fmt.Errorf("exec: write temp: %w", err)
		}
		n2, err := io.Copy(f, resp.Body)
		if err != nil {
			return nil, fmt.Errorf("exec: copy temp: %w", err)
		}
		result.Size = int64(n1) + n2
		result.TempPath = f.Name()
		result.Body = nil
	} else {
		result.Body = body
	}

	return result, nil
}

// classifyError maps network/context errors to Quark sentinel errors.
func (e *Executor) classifyError(err error) error {
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("exec: %w: %w", ErrRequestCancelled, err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("exec: %w: %w", ErrTimeout, err)
	}
	return fmt.Errorf("exec: %w", err)
}
