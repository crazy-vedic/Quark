// Package exec implements HTTP request execution for Quark.
// Accept http.RoundTripper (not *http.Client) so tests can inject a transport.
package exec

import (
	"errors"
	"os"
	"time"
)

// Sentinel errors — use errors.Is to classify at the call site.
var (
	ErrTimeout          = errors.New("request timed out")
	ErrRequestCancelled = errors.New("request cancelled")
	ErrInvalidURL       = errors.New("invalid URL")
)

// ExecuteResult holds the outcome of a single HTTP dispatch.
// error is always its own return value — never part of this struct.
//
// Lifecycle: if TempPath is non-empty, the caller owns the temp file.
// Call Cleanup() when finished rendering the response to remove it.
type ExecuteResult struct {
	StatusCode int
	Status     string // e.g. "200 OK"
	// Headers is in canonical MIME title-case (http.CanonicalHeaderKey).
	// Use http.Header(r.Headers).Get("content-type") for case-insensitive access.
	// Do not use direct map key lookup.
	Headers  map[string][]string
	Body     []byte // nil if body was streamed to TempPath
	TempPath string // non-empty if body > streaming threshold; call Cleanup() when done
	Duration time.Duration
	Size     int64
}

// IsStreamed reports whether the response body was written to a temp file.
func (r *ExecuteResult) IsStreamed() bool { return r.TempPath != "" }

// Cleanup removes the temp file if the response was streamed to disk.
// Safe to call even when TempPath is empty. Callers must call this when
// they are finished reading the response.
func (r *ExecuteResult) Cleanup() error {
	if r.TempPath == "" {
		return nil
	}
	if err := os.Remove(r.TempPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	r.TempPath = ""
	return nil
}
