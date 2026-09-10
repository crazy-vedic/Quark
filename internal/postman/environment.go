package postman

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Environment represents a Postman environment export.
type Environment struct {
	ID     string     `json:"id"`
	Name   string     `json:"name"`
	Scope  string     `json:"scope,omitempty"`
	Values []EnvValue `json:"values"`
}

// EnvValue represents a single variable in a Postman environment.
type EnvValue struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Enabled bool   `json:"enabled"`
	Type    string `json:"type,omitempty"`
}

// ParseEnvironment reads a Postman environment JSON from r.
func ParseEnvironment(r io.Reader) (*Environment, error) {
	var env Environment
	if err := json.NewDecoder(r).Decode(&env); err != nil {
		return nil, fmt.Errorf("postman: parse environment: %w", err)
	}

	if env.Name == "" {
		return nil, fmt.Errorf("postman: environment missing name")
	}

	return &env, nil
}

// ToMap converts enabled environment values to a map.
// Disabled values are skipped.
func (e *Environment) ToMap() map[string]string {
	m, _ := e.ToMapWithWarnings()
	return m
}

// ToMapWithWarnings converts enabled values using first-key-wins semantics.
func (e *Environment) ToMapWithWarnings() (map[string]string, []string) {
	m := make(map[string]string, len(e.Values))
	var warnings []string
	for _, v := range e.Values {
		if !v.Enabled {
			continue
		}
		if strings.TrimSpace(v.Key) == "" {
			warnings = append(warnings, "environment variable with empty key skipped")
			continue
		}
		if existing, duplicate := m[v.Key]; duplicate {
			if existing != v.Value {
				warnings = append(
					warnings,
					fmt.Sprintf("duplicate environment variable %q ignored", v.Key),
				)
			}
			continue
		}
		m[v.Key] = v.Value
	}
	return m, warnings
}
