package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrInvalidEnvironmentData classifies malformed environment variable data.
var ErrInvalidEnvironmentData = errors.New("invalid environment data")

// Environment stores a set of key-value variables for a collection.
// CollectionID == "" means this is the global environment.
type Environment struct {
	ID           string
	CollectionID string
	Name         string
	Data         string // JSON key-value pairs
	SortOrder    int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// IsGlobal reports whether this is the global environment.
func (e *Environment) IsGlobal() bool {
	return e.CollectionID == ""
}

// DecodeVars strictly decodes Data as a JSON object containing only string values.
// The returned map never aliases storage owned by the Environment.
func (e *Environment) DecodeVars() (map[string]string, error) {
	if e == nil {
		return nil, fmt.Errorf("%w: nil environment", ErrInvalidEnvironmentData)
	}
	var vars map[string]string
	if err := json.Unmarshal([]byte(e.Data), &vars); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidEnvironmentData, err)
	}
	if vars == nil {
		return nil, fmt.Errorf("%w: root must be an object", ErrInvalidEnvironmentData)
	}
	return vars, nil
}

// Vars decodes the Data JSON into a map. It is retained for display-oriented
// callers; validation and execution paths must use DecodeVars so corruption is
// never mistaken for an empty environment.
func (e *Environment) Vars() map[string]string {
	m, err := e.DecodeVars()
	if err != nil {
		return nil
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// SetVars encodes a map into Data JSON.
func (e *Environment) SetVars(v map[string]string) {
	if len(v) == 0 {
		e.Data = "{}"
		return
	}
	b, _ := json.Marshal(v)
	e.Data = string(b)
}
