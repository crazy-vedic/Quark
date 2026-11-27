package domain

import (
	"errors"
	"fmt"
	"time"
)

// Request is a child entity of Collection representing a single HTTP request.
type Request struct {
	ID           string
	CollectionID string
	Name         string
	Method       string // GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS
	URL          string
	Headers      string // JSON object: {"Key": "Value"}
	AuthType     string
	AuthConfig   string // JSON object keyed by auth type needs
	Body         string
	SortOrder    int
	// Disabled marks a request the user switched off. It stays in its
	// collection, but `quark run`, scheduled runs and the TUI refuse to send
	// it. The zero value is an enabled request.
	Disabled  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ErrRequestDisabled is returned when a disabled request is run or scheduled.
var ErrRequestDisabled = errors.New("request is disabled")

// CheckEnabled returns an error wrapping ErrRequestDisabled when r is disabled.
func (r *Request) CheckEnabled() error {
	if r != nil && r.Disabled {
		return fmt.Errorf("%w: %q (enable it in the TUI first)", ErrRequestDisabled, r.Name)
	}
	return nil
}
