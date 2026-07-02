package exec

import (
	"encoding/json"
	"strings"
)

// newStringReader wraps a string as an io.Reader.
func newStringReader(s string) *strings.Reader {
	return strings.NewReader(s)
}

// parseHeaders decodes the JSON object stored in domain.Request.Headers.
// Returns a flat map[string]string.
func parseHeaders(raw string) (map[string]string, error) {
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, err
	}
	return m, nil
}
