package domain

import (
	"fmt"
	"net/url"
	"strings"
)

// BodyType says how Request.Body is sent. A new body type is added by a
// constant here, a case in NormalizeBodyType, and handling in exec.
type BodyType = string

const (
	// BodyTypeNone sends no body, even when Request.Body holds text.
	BodyTypeNone = "none"
	// BodyTypeRaw sends Request.Body exactly as stored.
	BodyTypeRaw = "raw"
	// BodyTypeJSON sends Request.Body as stored, and sets
	// Content-Type: application/json when the request has no Content-Type.
	BodyTypeJSON = "json"
	// BodyTypeFormURLEncoded sends Request.Body as a stored
	// application/x-www-form-urlencoded string. It is set to that
	// Content-Type when the request has none.
	BodyTypeFormURLEncoded = "form-urlencoded"
)

// NormalizeBodyType returns the canonical body type for s. Blank means raw,
// which is how requests saved before body types existed behave.
func NormalizeBodyType(s string) (BodyType, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", BodyTypeRaw:
		return BodyTypeRaw, nil
	case BodyTypeNone:
		return BodyTypeNone, nil
	case BodyTypeJSON:
		return BodyTypeJSON, nil
	case BodyTypeFormURLEncoded:
		return BodyTypeFormURLEncoded, nil
	}
	return "", fmt.Errorf("unsupported body type %q", s)
}

// NextBodyType returns the body type after current, in the order the TUI
// cycles through them. An unknown current value starts at none.
func NextBodyType(current BodyType) BodyType {
	order := []BodyType{BodyTypeNone, BodyTypeRaw, BodyTypeJSON, BodyTypeFormURLEncoded}
	normalized, err := NormalizeBodyType(current)
	if err != nil {
		return order[0]
	}
	for i, t := range order {
		if t == normalized {
			return order[(i+1)%len(order)]
		}
	}
	return order[0]
}

// DefaultBodyContentType is the Content-Type a body type sends when the
// request has no Content-Type of its own. It is "" when the type sets none.
func DefaultBodyContentType(bodyType BodyType) string {
	switch bodyType {
	case BodyTypeJSON:
		return "application/json"
	case BodyTypeFormURLEncoded:
		return "application/x-www-form-urlencoded"
	}
	return ""
}

// FormField is one name/value row of a form-urlencoded body. Key and Value
// are decoded.
type FormField struct {
	Key   string
	Value string
}

// ParseFormBody decodes a form-urlencoded body into rows, in order. Empty
// segments are skipped, and a segment that does not decode is shown as
// written.
func ParseFormBody(body string) []FormField {
	var fields []FormField
	for _, segment := range strings.Split(body, "&") {
		if segment == "" {
			continue
		}
		rawKey, rawValue, _ := strings.Cut(segment, "=")
		fields = append(
			fields,
			FormField{Key: decodeFormComponent(rawKey), Value: decodeFormComponent(rawValue)},
		)
	}
	return fields
}

// EncodeFormBody encodes rows as a form-urlencoded body. Rows with an empty
// key and value are dropped. Each key and value is escaped as a query
// component, but '{' and '}' stay readable so {{variables}} still work.
func EncodeFormBody(fields []FormField) string {
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		if f.Key == "" && f.Value == "" {
			continue
		}
		parts = append(parts, EncodeFormKey(f.Key)+"="+EncodeFormValue(f.Value))
	}
	return strings.Join(parts, "&")
}

// EncodeFormKey escapes s for use as a form field name.
func EncodeFormKey(s string) string { return encodeFormComponent(s) }

// EncodeFormValue escapes s for use as a form field value. Interpolation uses
// it for the value of a {{variable}} placed in a form body.
func EncodeFormValue(s string) string { return encodeFormComponent(s) }

func decodeFormComponent(s string) string {
	decoded, err := url.QueryUnescape(s)
	if err != nil {
		return s
	}
	return decoded
}

// encodeFormComponent escapes everything except the unreserved characters
// and '{' and '}', which stay readable so {{variables}} survive. A space is
// written as '+', as url.QueryEscape does.
func encodeFormComponent(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ' ':
			b.WriteByte('+')
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9',
			c == '-', c == '_', c == '.', c == '~', c == '{', c == '}':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String()
}
