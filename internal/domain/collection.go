// Package domain holds shared data types used across internal packages.
// It has no dependencies on other internal packages.
package domain

import "time"

// Collection is the aggregate root for grouping HTTP requests.
// Changing field types or removing fields is a breaking change.
type Collection struct {
	ID string
	// ParentID is empty for a root collection.
	ParentID    string
	Name        string
	Description string
	Meta        string // JSON; extensible bag for future use
	// AuthType, AuthConfig and Headers are defaults that requests in this
	// collection and its children inherit. An empty or "inherit" AuthType
	// defers to the parent; "none" stops inheritance with no auth. Headers is
	// a JSON object in the same shape as Request.Headers.
	AuthType   string
	AuthConfig string
	Headers    string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Version    int // optimistic locking hook for V2 sync
}
