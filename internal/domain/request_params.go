package domain

import (
	"encoding/json"
	"net/url"
	"strings"
)

// QueryParam is one name/value pair of a URL query string, as shown in the
// query parameter editor. Key and Value are decoded.
type QueryParam struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	// Disabled params are kept on the request but left out of the URL, so
	// they are not sent.
	Disabled bool `json:"-"`
	// Raw is the encoded "key=value" segment as found in the URL. It is
	// written back verbatim while Key and Value still decode from it, so an
	// untouched param keeps its exact bytes.
	Raw string `json:"-"`
}

// RequestParams is the JSON stored in Request.Params: editor state that the
// URL itself cannot hold.
type RequestParams struct {
	// DisabledQuery holds query params switched off in the editor.
	DisabledQuery []QueryParam `json:"disabled_query,omitempty"`
	// PathValues holds the value of each URL path placeholder, keyed by its
	// name without the ':'. Values may use {{variables}}.
	PathValues map[string]string `json:"path_values,omitempty"`
}

// ParseRequestParams decodes Request.Params. Blank input yields the zero value.
func ParseRequestParams(raw string) (RequestParams, error) {
	var params RequestParams
	if strings.TrimSpace(raw) == "" {
		return params, nil
	}
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return RequestParams{}, err
	}
	for i := range params.DisabledQuery {
		params.DisabledQuery[i].Disabled = true
	}
	return params, nil
}

// String encodes p for Request.Params; an empty value encodes as "{}".
func (p RequestParams) String() string {
	data, err := json.Marshal(p)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// splitURL separates rawURL into the part before the query, the raw query
// (without '?') and the fragment (with '#').
func splitURL(rawURL string) (base, query, fragment string) {
	rest := rawURL
	if i := strings.IndexByte(rest, '#'); i >= 0 {
		rest, fragment = rest[:i], rest[i:]
	}
	if i := strings.IndexByte(rest, '?'); i >= 0 {
		return rest[:i], rest[i+1:], fragment
	}
	return rest, "", fragment
}

// ParseURLQuery returns the query params of rawURL in order. Empty segments
// (as in "a=1&&b=2") are skipped. A value that does not decode is shown as
// written.
func ParseURLQuery(rawURL string) []QueryParam {
	_, query, _ := splitURL(rawURL)
	if query == "" {
		return nil
	}
	var params []QueryParam
	for _, segment := range strings.Split(query, "&") {
		if segment == "" {
			continue
		}
		key, value := decodeQuerySegment(segment)
		params = append(params, QueryParam{Key: key, Value: value, Raw: segment})
	}
	return params
}

func decodeQuerySegment(segment string) (key, value string) {
	rawKey, rawValue, _ := strings.Cut(segment, "=")
	return unescapeQuery(rawKey), unescapeQuery(rawValue)
}

func unescapeQuery(s string) string {
	decoded, err := url.QueryUnescape(s)
	if err != nil {
		return s
	}
	return decoded
}

// SetURLQuery replaces the query of rawURL with the enabled params in order,
// keeping the fragment. Params with an empty key and value are dropped. With
// no params left the '?' is removed.
func SetURLQuery(rawURL string, params []QueryParam) string {
	base, _, fragment := splitURL(rawURL)
	var segments []string
	for _, p := range params {
		if p.Disabled || (p.Key == "" && p.Value == "") {
			continue
		}
		segments = append(segments, encodeQueryParam(p))
	}
	if len(segments) == 0 {
		return base + fragment
	}
	return base + "?" + strings.Join(segments, "&") + fragment
}

func encodeQueryParam(p QueryParam) string {
	if p.Raw != "" {
		if key, value := decodeQuerySegment(p.Raw); key == p.Key && value == p.Value {
			return p.Raw
		}
	}
	return escapeQuery(p.Key, true) + "=" + escapeQuery(p.Value, false)
}

// escapeQuery percent-encodes the bytes that would change the meaning of a
// query segment. Unlike url.QueryEscape it leaves characters such as '{', '}'
// and '/' readable, so {{variables}} stay intact in the URL.
func escapeQuery(s string, isKey bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if queryByteNeedsEscape(c) || (isKey && c == '=') {
			b.WriteByte('%')
			b.WriteByte("0123456789ABCDEF"[c>>4])
			b.WriteByte("0123456789ABCDEF"[c&15])
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func queryByteNeedsEscape(c byte) bool {
	if c <= ' ' || c >= 0x7f {
		return true
	}
	switch c {
	case '%', '&', '#', '+', '"', '<', '>', '\\', '^', '`':
		return true
	}
	return false
}
