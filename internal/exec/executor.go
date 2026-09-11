package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/crazy-vedic/quark/internal/domain"
)

// VariableResolver resolves environment variables for a collection.
// Returns (collectionEnv, globalEnv) or a resolution error.
// The resolver is called synchronously during Execute, before building the HTTP request.
type VariableResolver func(collectionID string) (colEnv, globalEnv map[string]string, err error)

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
		colEnv, globalEnv, err := e.variableResolver(req.CollectionID)
		if err != nil {
			wrapped := fmt.Errorf("exec: resolve variables: %w", err)
			record(req, nil, wrapped)
			return nil, wrapped
		}
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

	body, err := requestBodyReader(req.Body)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, body)
	if err != nil {
		return nil, err
	}

	// Parse and apply headers stored as a JSON object in the DB.
	if req.Headers != "" && req.Headers != "{}" {
		headers, err := parseHeaders(req.Headers)
		if err != nil {
			// Malformed JSON in the headers column — warn and continue without
			// headers rather than silently sending the request with none applied.
			slog.Warn("exec: failed to parse request headers; sending without headers",
				"request_id", req.ID, "err", err)
		} else {
			for key, values := range headers {
				for _, value := range values {
					value, err := resolveHeaderFile(value)
					if err != nil {
						return nil, fmt.Errorf("exec: header %q: %w", key, err)
					}
					httpReq.Header.Add(key, value)
				}
			}
		}
	}

	if err := applySpecialRequestHeaders(httpReq); err != nil {
		return nil, err
	}

	if err := applyRequestAuth(httpReq, req); err != nil {
		return nil, err
	}

	return httpReq, nil
}

const maxRequestFileSize = 10 << 20

// requestBodyReader expands the exact @path form used by the Body editor.
// Keeping the reference in domain.Request means saved requests remain small
// and continue to follow changes to the referenced file.
func requestBodyReader(body string) (io.Reader, error) {
	if body == "" {
		return nil, nil
	}
	if !strings.HasPrefix(body, "@") || len(body) == 1 {
		return newStringReader(body), nil
	}

	data, err := readRequestFile(strings.TrimPrefix(body, "@"))
	if err != nil {
		return nil, fmt.Errorf("read body file %q: %w", strings.TrimPrefix(body, "@"), err)
	}
	return bytes.NewReader(data), nil
}

func resolveHeaderFile(value string) (string, error) {
	if !strings.HasPrefix(value, "@") || len(value) == 1 {
		return value, nil
	}
	data, err := readRequestFile(strings.TrimPrefix(value, "@"))
	if err != nil {
		return "", fmt.Errorf("read file %q: %w", strings.TrimPrefix(value, "@"), err)
	}
	return string(data), nil
}

func readRequestFile(path string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("file path is empty")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, maxRequestFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxRequestFileSize {
		return nil, fmt.Errorf("file exceeds %d MiB limit", maxRequestFileSize/(1<<20))
	}
	return data, nil
}

// applySpecialRequestHeaders maps headers that net/http treats as Request
// fields. Leaving them in Header alone makes them ineffective or produces a
// different wire request than the user entered.
func applySpecialRequestHeaders(req *http.Request) error {
	if host := req.Header.Get("Host"); host != "" {
		req.Host = host
		req.Header.Del("Host")
	}
	if rawLength := req.Header.Get("Content-Length"); rawLength != "" {
		length, err := strconv.ParseInt(strings.TrimSpace(rawLength), 10, 64)
		if err != nil || length < 0 {
			return fmt.Errorf("exec: invalid Content-Length %q", rawLength)
		}
		req.ContentLength = length
		req.Header.Del("Content-Length")
	}
	if transfer := req.Header.Get("Transfer-Encoding"); transfer != "" {
		var encodings []string
		for _, value := range strings.Split(transfer, ",") {
			value = strings.TrimSpace(strings.ToLower(value))
			if value == "" {
				continue
			}
			if value != "chunked" && value != "identity" {
				return fmt.Errorf("exec: unsupported Transfer-Encoding %q", value)
			}
			encodings = append(encodings, value)
		}
		req.TransferEncoding = encodings
		req.Header.Del("Transfer-Encoding")
	}
	if connection := req.Header.Get("Connection"); strings.EqualFold(strings.TrimSpace(connection), "close") {
		req.Close = true
		req.Header.Del("Connection")
	}
	return nil
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
