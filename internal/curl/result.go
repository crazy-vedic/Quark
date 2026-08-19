// Package curl parses curl commands into structured HTTP request data.
package curl

import "net/http"

// SecurityLevel classifies the risk level of a parsed curl command.
type SecurityLevel int

const (
	// Safe: no credentials, no file reads, no shell expansion detected.
	Safe SecurityLevel = iota
	// Review: contains credentials (Authorization, Cookie, -u), body data,
	// or other headers that warrant inspection before import.
	Review
	// Dangerous: contains @filename body reads or shell expansion patterns.
	Dangerous
)

func (s SecurityLevel) String() string {
	switch s {
	case Safe:
		return "SAFE"
	case Review:
		return "REVIEW"
	case Dangerous:
		return "DANGEROUS"
	default:
		return "UNKNOWN"
	}
}

// ImportResult holds the parsed output of a curl command.
// error is always its own return value — never part of this struct.
type ImportResult struct {
	Method      string
	URL         string
	Headers     http.Header
	Body        string
	Security    SecurityLevel
	Certificate *CertificateSpec
	// Warnings is sorted lexicographically before return.
	// Tests must use assert.Equal, not assert.ElementsMatch.
	Warnings []string
}

// CertificateSpec contains curl TLS options that cannot be represented by an
// http.Request. It is kept separate from HTTP headers and body data.
type CertificateSpec struct {
	File     string
	Type     string
	Password string
	KeyFile  string
	CAFile   string
}
