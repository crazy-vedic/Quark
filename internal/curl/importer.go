package curl

import (
	"fmt"
	"io"
	"sort"
)

const maxCommandBytes = 4 << 20

// Importer parses curl commands into ImportResult values.
type Importer struct{}

// NewImporter constructs an Importer. Zero options are valid.
func NewImporter(opts ...Option) *Importer {
	_ = applyOpts(opts)
	return &Importer{}
}

// Parse reads a curl command from r and returns a structured ImportResult.
// Warnings are sorted lexicographically before returning.
func (im *Importer) Parse(r io.Reader) (*ImportResult, error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxCommandBytes+1))
	if err != nil {
		return nil, fmt.Errorf("curl: read input: %w", err)
	}
	if len(raw) > maxCommandBytes {
		return nil, fmt.Errorf("curl: command exceeds %d bytes", maxCommandBytes)
	}

	result, err := parseCommand(string(raw))
	if err != nil {
		return nil, err
	}

	sort.Strings(result.Warnings)
	return result, nil
}
