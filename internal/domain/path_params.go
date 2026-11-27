package domain

import "strings"

// A path placeholder is a whole URL path segment that starts with ':' and
// then a name, such as the ":id" in https://api.test/users/:id.

// PathPlaceholders returns the distinct placeholder names in rawURL's path,
// in the order they first appear. The scheme, host, query and fragment are
// never searched.
func PathPlaceholders(rawURL string) []string {
	_, segments, _, _ := splitPathURL(rawURL)
	var names []string
	seen := make(map[string]bool)
	for _, seg := range segments {
		name, ok := pathPlaceholderName(seg)
		if !ok || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

// ApplyPathParams replaces each placeholder that has a non-empty value in
// values. Placeholders without a value stay as written. Values are escaped
// like query values, except that '{' and '}' stay readable so {{variables}}
// still work. An escaped value never starts a new placeholder, so applying
// the result again changes nothing.
func ApplyPathParams(rawURL string, values map[string]string) string {
	prefix, segments, query, fragment := splitPathURL(rawURL)
	hasQuery := query != nil
	for i, seg := range segments {
		name, ok := pathPlaceholderName(seg)
		if !ok {
			continue
		}
		if value := values[name]; value != "" {
			segments[i] = escapePathValue(value)
		}
	}
	var b strings.Builder
	b.WriteString(prefix)
	b.WriteString(strings.Join(segments, "/"))
	if hasQuery {
		b.WriteString("?")
		b.WriteString(*query)
	}
	b.WriteString(fragment)
	return b.String()
}

// splitPathURL splits rawURL into the part before the path, the path
// segments and the query and fragment. The path always starts with '/' when
// it is not empty, so its first segment is "". The query is nil when there
// is no '?'.
func splitPathURL(
	rawURL string,
) (prefix string, segments []string, query *string, fragment string) {
	rest := rawURL
	if i := strings.IndexByte(rest, '#'); i >= 0 {
		rest, fragment = rest[:i], rest[i:]
	}
	if i := strings.IndexByte(rest, '?'); i >= 0 {
		q := rest[i+1:]
		query = &q
		rest = rest[:i]
	}
	start := 0
	if i := strings.Index(rest, "://"); i >= 0 {
		start = i + len("://")
	}
	j := strings.IndexByte(rest[start:], '/')
	if j < 0 {
		return rest, nil, query, fragment
	}
	prefix = rest[:start+j]
	return prefix, strings.Split(rest[start+j:], "/"), query, fragment
}

// pathPlaceholderName reports the name of a ":name" segment.
func pathPlaceholderName(segment string) (string, bool) {
	if len(segment) < 2 || segment[0] != ':' {
		return "", false
	}
	name := segment[1:]
	for i := 0; i < len(name); i++ {
		c := name[i]
		isLetter := c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		isDigit := c >= '0' && c <= '9'
		if isLetter || (i > 0 && (isDigit || c == '-')) {
			continue
		}
		return "", false
	}
	return name, true
}

func escapePathValue(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if queryByteNeedsEscape(c) || c == '/' || c == '?' || (i == 0 && c == ':') {
			b.WriteByte('%')
			b.WriteByte("0123456789ABCDEF"[c>>4])
			b.WriteByte("0123456789ABCDEF"[c&15])
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}
