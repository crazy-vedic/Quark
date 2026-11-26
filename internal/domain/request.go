package domain

import (
	"errors"
	"fmt"
	"time"
)

// ErrInvalidMethod reports an HTTP method that is not a valid token.
var ErrInvalidMethod = errors.New("invalid HTTP method")

// Request is a child entity of Collection representing a single HTTP request.
type Request struct {
	ID           string
	CollectionID string
	Name         string
	Method       string // any HTTP token, e.g. GET, POST, TRACE, PROPFIND
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

// ValidMethod reports whether method is a non-empty HTTP token
// (RFC 9110 section 5.6.2), the only shape a request method may take.
func ValidMethod(method string) error {
	if method == "" {
		return fmt.Errorf("%w: method is empty", ErrInvalidMethod)
	}
	for i := 0; i < len(method); i++ {
		if !isTokenChar(method[i]) {
			return fmt.Errorf("%w: %q", ErrInvalidMethod, method)
		}
	}
	return nil
}

func isTokenChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	switch c {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}
	return false
}
