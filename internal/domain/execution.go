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
	ResponseBody    string
	ResponseTimeMs  int64

	StartedAt   time.Time
	CompletedAt time.Time
	Error       string
}
