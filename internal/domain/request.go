package domain

import (
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
	Enabled      bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// CopyName returns the name for a duplicate of a request called name: the first
// of "<name> copy", "<name> copy 2", "<name> copy 3", ... that taken reports as
// free.
func CopyName(name string, taken func(string) bool) string {
	candidate := name + " copy"
	for n := 2; taken(candidate); n++ {
		candidate = fmt.Sprintf("%s copy %d", name, n)
	}
	return candidate
}
