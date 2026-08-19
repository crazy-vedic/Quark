package exec

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// newStringReader wraps a string as an io.Reader.
func newStringReader(s string) *strings.Reader {
	return strings.NewReader(s)
}

// parseHeaders decodes the JSON object stored in domain.Request.Headers.
// Both the legacy {"Name":"value"} representation and the repeated-value
// {"Name":["one","two"]} representation are accepted.
func parseHeaders(raw string) (http.Header, error) {
	var values map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, err
	}
	headers := make(http.Header, len(values))
	for key, encoded := range values {
		var repeated []string
		if err := json.Unmarshal(encoded, &repeated); err == nil {
			for _, value := range repeated {
				headers.Add(key, value)
			}
			continue
		}
		var single string
		if err := json.Unmarshal(encoded, &single); err != nil {
			return nil, fmt.Errorf("header %q: value must be a string or string array", key)
		}
		headers.Add(key, single)
	}
	return headers, nil
}
