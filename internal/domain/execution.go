package domain

import "time"

// Execution is an immutable audit record of a single HTTP dispatch.
// It is NOT relational to Request via FK — if a request is deleted,
// history must survive. The RequestSnapshot captures the full request
// state at the time of execution (including interpolated variables).
type Execution struct {
	ID              string
	RequestSnapshot string // JSON: {method, url, headers, body}
	RequestID       string // soft reference; may be empty if request deleted

	StatusCode      int
	ResponseHeaders string // JSON
	// ResponseBody holds at most the stored prefix of the response body.
	ResponseBody string
	// ResponseSize is the full response body size in bytes, which can be
	// larger than len(ResponseBody) when the stored body was truncated.
	ResponseSize int64
	// ResponseBodyTruncated reports that only a prefix of the body was stored.
	ResponseBodyTruncated bool
	// BodyOmitted reports that ResponseBody was not loaded (list queries skip
	// bodies); fetch the full entry by ID to read it.
	BodyOmitted    bool
	ResponseTimeMs int64

	StartedAt   time.Time
	CompletedAt time.Time
	Error       string
}

// BodySize returns the full response body size, falling back to the stored
// body length for entries recorded without a size.
func (e *Execution) BodySize() int64 {
	if e.ResponseSize > 0 {
		return e.ResponseSize
	}
	return int64(len(e.ResponseBody))
}
