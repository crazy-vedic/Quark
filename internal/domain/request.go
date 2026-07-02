package domain

import "time"

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
