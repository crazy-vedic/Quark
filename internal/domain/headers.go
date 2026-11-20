package domain

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ParseHeaders decodes the JSON stored in Request.Headers. Each value may be
// a string or an array of strings; both shapes exist in saved data (the TUI
// editor and the importers write arrays). Header names keep their stored
// case. Blank input yields an empty map.
func ParseHeaders(raw string) (map[string][]string, error) {
	headers := map[string][]string{}
	if strings.TrimSpace(raw) == "" {
		return headers, nil
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, err
	}
	for key, encoded := range values {
		var repeated []string
		if err := json.Unmarshal(encoded, &repeated); err == nil {
			headers[key] = repeated
			continue
		}
		var single string
		if err := json.Unmarshal(encoded, &single); err != nil {
			return nil, fmt.Errorf("header %q: value must be a string or string array", key)
		}
		headers[key] = []string{single}
	}
	return headers, nil
}
