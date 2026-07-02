// Package domain holds shared data types used across internal packages.
// It has no dependencies on other internal packages.
package domain

import "time"

// Collection is the aggregate root for grouping HTTP requests.
// Changing field types or removing fields is a breaking change.
type Collection struct {
	ID          string
	Name        string
	Description string
	Meta        string // JSON; extensible bag for future use
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Version     int // optimistic locking hook for V2 sync
}
