package domain

import (
	"encoding/json"
	"time"
)

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

// Vars decodes the Data JSON into a map.
// Returns nil on invalid JSON or empty data.
func (e *Environment) Vars() map[string]string {
	if e.Data == "" || e.Data == "{}" {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(e.Data), &m); err != nil {
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
