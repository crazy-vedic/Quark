// Package jsonpath evaluates a small, documented subset of JSONPath against
// a decoded JSON document. It selects exactly one value; it has no wildcards,
// filters, slices or recursive descent.
//
// Supported syntax:
//
//	$                 the whole document (the leading $ is optional)
//	$.name            object member "name" (letters, digits, _ and -)
//	$['any key']      object member with any name ("double quotes" also work)
//	$[0]              array element; a negative index counts from the end
//	$.items[0].id     steps can be chained
//
// A path that does not match the document returns ErrNotFound.
package jsonpath

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrNotFound means the path is valid but selects nothing in the document.
var ErrNotFound = errors.New("jsonpath: no value at path")

// ErrSyntax means the path is not valid in the supported subset.
var ErrSyntax = errors.New("jsonpath: invalid path")

// step is one member name or one array index.
type step struct {
	key     string
	index   int
	isIndex bool
}

// Path is a parsed JSONPath expression.
type Path struct {
	raw   string
	steps []step
}

// String returns the expression as written.
func (p Path) String() string { return p.raw }

// Parse parses expr. It returns an error wrapping ErrSyntax when expr uses
// syntax outside the supported subset.
func Parse(expr string) (Path, error) {
	raw := expr
	s := strings.TrimSpace(expr)
	if s == "" {
		return Path{}, fmt.Errorf("%w: empty path", ErrSyntax)
	}
	s = strings.TrimPrefix(s, "$")
	var steps []step
	for len(s) > 0 {
		switch s[0] {
		case '.':
			s = s[1:]
			n := 0
			for n < len(s) && isNameByte(s[n]) {
				n++
			}
			if n == 0 {
				return Path{}, fmt.Errorf("%w: %q: expected a name after '.'", ErrSyntax, raw)
			}
			steps = append(steps, step{key: s[:n]})
			s = s[n:]
		case '[':
			end, st, err := parseBracket(s)
			if err != nil {
				return Path{}, fmt.Errorf("%w: %q: %s", ErrSyntax, raw, err.Error())
			}
			steps = append(steps, st)
			s = s[end:]
		default:
			if len(steps) == 0 && isNameByte(s[0]) && !strings.HasPrefix(raw, "$") {
				// Allow "data.token" as a shorthand for "$.data.token".
				s = "." + s
				continue
			}
			return Path{}, fmt.Errorf("%w: %q: unexpected %q", ErrSyntax, raw, s[0])
		}
	}
	return Path{raw: raw, steps: steps}, nil
}

// parseBracket parses one [...] step at the start of s and returns the
// number of bytes consumed.
func parseBracket(s string) (int, step, error) {
	if len(s) < 2 {
		return 0, step{}, errors.New("unterminated '['")
	}
	if s[1] == '\'' || s[1] == '"' {
		quote := s[1]
		var b strings.Builder
		for i := 2; i < len(s); i++ {
			c := s[i]
			if c == '\\' && i+1 < len(s) {
				b.WriteByte(s[i+1])
				i++
				continue
			}
			if c == quote {
				if i+1 >= len(s) || s[i+1] != ']' {
					return 0, step{}, errors.New("expected ']' after quoted name")
				}
				return i + 2, step{key: b.String()}, nil
			}
			b.WriteByte(c)
		}
		return 0, step{}, errors.New("unterminated quoted name")
	}
	end := strings.IndexByte(s, ']')
	if end < 0 {
		return 0, step{}, errors.New("unterminated '['")
	}
	inner := strings.TrimSpace(s[1:end])
	idx, err := strconv.Atoi(inner)
	if err != nil {
		return 0, step{}, fmt.Errorf(
			"unsupported selector [%s]: use a number or a quoted name",
			inner,
		)
	}
	return end + 1, step{index: idx, isIndex: true}, nil
}

func isNameByte(c byte) bool {
	return c == '_' || c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' ||
		c >= 'A' && c <= 'Z' || c >= 0x80
}

// Lookup returns the value at p in doc, a value produced by decoding JSON
// with UseNumber (or plain json.Unmarshal).
func (p Path) Lookup(doc any) (any, error) {
	cur := doc
	for _, st := range p.steps {
		if st.isIndex {
			arr, ok := cur.([]any)
			if !ok {
				return nil, fmt.Errorf("%w: %s: not an array at [%d]", ErrNotFound, p.raw, st.index)
			}
			i := st.index
			if i < 0 {
				i += len(arr)
			}
			if i < 0 || i >= len(arr) {
				return nil, fmt.Errorf(
					"%w: %s: index %d out of range",
					ErrNotFound,
					p.raw,
					st.index,
				)
			}
			cur = arr[i]
			continue
		}
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: %s: not an object at %q", ErrNotFound, p.raw, st.key)
		}
		v, ok := obj[st.key]
		if !ok {
			return nil, fmt.Errorf("%w: %s: no member %q", ErrNotFound, p.raw, st.key)
		}
		cur = v
	}
	return cur, nil
}

// Decode parses body as JSON, keeping numbers exact.
func Decode(body []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("jsonpath: response body is not JSON: %w", err)
	}
	return doc, nil
}

// Eval parses expr, decodes body and returns the selected value.
func Eval(body []byte, expr string) (any, error) {
	p, err := Parse(expr)
	if err != nil {
		return nil, err
	}
	doc, err := Decode(body)
	if err != nil {
		return nil, err
	}
	return p.Lookup(doc)
}

// Text renders a selected value as a variable value: strings without
// quotes, numbers and booleans as written, null as "null", and objects or
// arrays as compact JSON.
func Text(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case nil:
		return "null"
	case bool:
		return strconv.FormatBool(t)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}
