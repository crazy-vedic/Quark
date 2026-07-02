// Package postman implements parsing and import of Postman Collection v2.1 format.
package postman

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// scalarValue is a string-backed type that unmarshals from any JSON scalar
// (string, bool, number, null). Non-string values are stored as their JSON text.
// It is used for Postman fields that may contain mixed types (e.g. auth params
// where showPassword is a boolean but username is a string).
type scalarValue string

func (s *scalarValue) UnmarshalJSON(data []byte) error {
	// Try string first
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		*s = scalarValue(str)
		return nil
	}
	// null → empty
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		*s = ""
		return nil
	}
	// Store raw JSON text for bools, numbers, etc.
	*s = scalarValue(string(data))
	return nil
}

func (s scalarValue) String() string { return string(s) }

// Collection represents a Postman Collection v2.1 document.
type Collection struct {
	Info     Info       `json:"info"`
	Item     []Item     `json:"item"`
	Variable []Variable `json:"variable,omitempty"`
}

// Info contains collection metadata.
type Info struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Schema      string `json:"schema"`
	PostmanID   string `json:"_postman_id,omitempty"`
}

// Item represents a request or folder in a Postman collection.
// Items can be nested recursively (folders contain items).
type Item struct {
	Name     string     `json:"name"`
	Item     []Item     `json:"item,omitempty"` // nested folders
	Request  *Request   `json:"request,omitempty"`
	Response []Response `json:"response,omitempty"`
}

// Request represents a single HTTP request in Postman.
type Request struct {
	Method string   `json:"method"`
	URL    URL      `json:"url"`
	Header []Header `json:"header,omitempty"`
	Body   Body     `json:"body,omitempty"`
	Auth   *Auth    `json:"auth,omitempty"`
}

// URL represents a Postman URL, which can be a string or object.
type URL struct {
	Raw      string        `json:"raw"`
	Protocol string        `json:"protocol,omitempty"` // "http" or "https"
	Host     []string      `json:"host,omitempty"`
	Path     []any         `json:"path,omitempty"` // string or {"key": "value"}
	Query    []QueryParam  `json:"query,omitempty"`
	Variable []URLVariable `json:"variable,omitempty"`
}

// UnmarshalJSON implements custom unmarshaling for URL so it can handle both
// string URLs ("https://example.com") and object URLs ({"raw": "..."}).
func (u *URL) UnmarshalJSON(data []byte) error {
	// Try string first — Postman allows url as a plain string
	var raw string
	if err := json.Unmarshal(data, &raw); err == nil {
		u.Raw = raw
		return nil
	}
	// Fall back to object form
	type urlAlias URL
	var obj urlAlias
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	*u = URL(obj)
	return nil
}

// QueryParam represents a URL query parameter.
type QueryParam struct {
	Key   string      `json:"key"`
	Value scalarValue `json:"value"`
}

// URLVariable represents a URL path variable.
type URLVariable struct {
	Key   string      `json:"key"`
	Value scalarValue `json:"value"`
}

// Header represents an HTTP header.
type Header struct {
	Key   string      `json:"key"`
	Value scalarValue `json:"value"`
	Type  string      `json:"type,omitempty"` // usually "text"
}

// Body represents the request body.
type Body struct {
	Mode    string  `json:"mode"`              // "raw", "urlencoded", "formdata", "graphql", "file"
	Raw     string  `json:"raw,omitempty"`     // for mode="raw"
	Options Options `json:"options,omitempty"` // language info
}

// Options contains body format options.
type Options struct {
	Raw struct {
		Language string `json:"language,omitempty"`
	} `json:"raw,omitempty"`
}

// Auth represents authentication configuration.
type Auth struct {
	Type   string      `json:"type"` // "basic", "bearer", "apikey", "oauth2", "aws", "ntlm", "noauth"
	Basic  []AuthParam `json:"basic,omitempty"`
	Bearer []AuthParam `json:"bearer,omitempty"`
	APIKey []AuthParam `json:"apikey,omitempty"`
}

// AuthParam represents a single authentication parameter.
type AuthParam struct {
	Key   string      `json:"key"`
	Value scalarValue `json:"value"`
	Type  string      `json:"type,omitempty"`
}

// Variable represents a collection variable.
type Variable struct {
	Key   string      `json:"key"`
	Value scalarValue `json:"value"`
	Type  string      `json:"type,omitempty"` // "string", "boolean", "number", "any"
}

// Response represents a saved response (not used for import, but kept for completeness).
type Response struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Code   int    `json:"code"`
}

// Parse reads a Postman Collection v2.1 JSON from r and returns the parsed Collection.
func Parse(r io.Reader) (*Collection, error) {
	var c Collection
	if err := json.NewDecoder(r).Decode(&c); err != nil {
		return nil, fmt.Errorf("postman: parse JSON: %w", err)
	}

	// Validate schema is v2.1 (non-strict: allow any schema that looks like a Postman
	// collection; we only enforce that it has at least info and item fields).

	if c.Info.Name == "" && len(c.Item) == 0 {
		return nil, fmt.Errorf("postman: not a valid collection: missing info.name and items")
	}

	return &c, nil
}
