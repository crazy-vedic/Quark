package config

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// tomlEdit changes config.toml in place, line by line, so the user's
// comments, blank lines and key order survive a save. It is deliberately
// small: it can set or delete a string key and replace an array of tables,
// which is all the savers need. The input must be valid TOML (the savers
// decode it first); the caller checks the edited text decodes to the expected
// document and falls back to a full re-encode when it does not.
type tomlEdit struct {
	lines []string
	crlf  bool
}

func newTOMLEdit(src string) *tomlEdit {
	e := &tomlEdit{crlf: strings.Contains(src, "\r\n")}
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.TrimSuffix(src, "\n")
	if src != "" {
		e.lines = strings.Split(src, "\n")
	}
	return e
}

func (e *tomlEdit) String() string {
	if len(e.lines) == 0 {
		return ""
	}
	nl := "\n"
	if e.crlf {
		nl = "\r\n"
	}
	return strings.Join(e.lines, nl) + nl
}

type stmtKind int

const (
	stmtTable stmtKind = iota + 1
	stmtArrayTable
	stmtKeyValue
)

// stmt is one table header or key/value pair. Blank and comment lines are not
// statements.
type stmt struct {
	kind stmtKind
	// path is the header's name, or the full key (table + dotted key) of a pair.
	path []string
	// inArray marks a pair that belongs to an array-of-tables element.
	inArray bool
	// start and end are the first and last line of the statement.
	start, end int
	// prefix is the start line up to the value ("key = ") of a pair.
	prefix string
	// tail is the trailing comment on the end line, with its leading space.
	tail string
}

// scan splits the document into statements.
func (e *tomlEdit) scan() []stmt {
	var (
		out     []stmt
		table   []string
		inArray bool
	)
	for i := 0; i < len(e.lines); i++ {
		line := e.lines[i]
		trimmed := strings.TrimLeft(line, " \t")
		switch {
		case trimmed == "" || trimmed[0] == '#':
			continue
		case strings.HasPrefix(trimmed, "[["):
			path, _ := parseKey(trimmed, 2)
			table, inArray = path, true
			out = append(out, stmt{kind: stmtArrayTable, path: path, start: i, end: i})
		case trimmed[0] == '[':
			path, _ := parseKey(trimmed, 1)
			table, inArray = path, false
			out = append(out, stmt{kind: stmtTable, path: path, start: i, end: i})
		default:
			key, pos := parseKey(line, 0)
			pos = skipSpace(line, pos)
			if pos < len(line) && line[pos] == '=' {
				pos = skipSpace(line, pos+1)
			}
			s := stmt{
				kind:    stmtKeyValue,
				path:    append(append([]string{}, table...), key...),
				inArray: inArray,
				start:   i,
				prefix:  line[:pos],
			}
			var vs valueScanner
			comment := vs.line(line[pos:])
			for vs.open() && i+1 < len(e.lines) {
				i++
				comment = vs.line(e.lines[i])
			}
			s.end = i
			if comment >= 0 {
				last, lo := e.lines[i], 0
				if i == s.start {
					comment += pos
					lo = pos
				}
				// Keep the spacing before the comment, or add one space.
				k := comment
				for k > lo && (last[k-1] == ' ' || last[k-1] == '\t') {
					k--
				}
				s.tail = last[k:]
				if k == comment {
					s.tail = " " + s.tail
				}
			}
			out = append(out, s)
		}
	}
	return out
}

// valueScanner tracks strings and brackets across the lines of one value.
type valueScanner struct {
	depth int
	// ml is the delimiter of an open multi-line string, or "".
	ml string
}

func (v *valueScanner) open() bool { return v.depth > 0 || v.ml != "" }

// line scans one line and returns the offset of a trailing comment, or -1.
func (v *valueScanner) line(s string) int {
	for i := 0; i < len(s); i++ {
		if v.ml != "" {
			if v.ml == `"""` && s[i] == '\\' {
				i++
				continue
			}
			if strings.HasPrefix(s[i:], v.ml) {
				// A closing delimiter may carry up to two extra quotes.
				i += 2
				for n := 0; n < 2 && i+1 < len(s) && s[i+1] == v.ml[0]; n++ {
					i++
				}
				v.ml = ""
			}
			continue
		}
		switch c := s[i]; c {
		case '#':
			return i
		case '"', '\'':
			delim := strings.Repeat(string(c), 3)
			if strings.HasPrefix(s[i:], delim) {
				v.ml = delim
				i += 2
				continue
			}
			i = skipString(s, i)
		case '[', '{':
			v.depth++
		case ']', '}':
			v.depth--
		}
	}
	return -1
}

// skipString returns the index of the quote that closes the single-line
// string opening at s[i].
func skipString(s string, i int) int {
	q := s[i]
	for i++; i < len(s); i++ {
		if q == '"' && s[i] == '\\' {
			i++
			continue
		}
		if s[i] == q {
			return i
		}
	}
	return i
}

func skipSpace(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return i
}

// parseKey reads a dotted key starting at s[i] and returns its parts and the
// offset just past it.
func parseKey(s string, i int) ([]string, int) {
	var parts []string
	for {
		i = skipSpace(s, i)
		if i >= len(s) {
			return parts, i
		}
		switch s[i] {
		case '"':
			end := skipString(s, i)
			raw := s[i:min(end+1, len(s))]
			part, err := strconv.Unquote(raw)
			if err != nil {
				part = strings.Trim(raw, `"`)
			}
			parts = append(parts, part)
			i = end + 1
		case '\'':
			end := skipString(s, i)
			parts = append(parts, s[i+1:min(end, len(s))])
			i = end + 1
		default:
			start := i
			for i < len(s) && isBareKeyChar(s[i]) {
				i++
			}
			parts = append(parts, s[start:i])
		}
		i = skipSpace(s, i)
		if i >= len(s) || s[i] != '.' {
			return parts, i
		}
		i++
	}
}

func isBareKeyChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
		c == '_' || c == '-'
}

func hasPrefix(path, prefix []string) bool {
	return len(path) >= len(prefix) && slices.Equal(path[:len(prefix)], prefix)
}

// formatKey renders one key part, quoting it when it is not a bare key.
func formatKey(k string) string {
	if k == "" {
		return `""`
	}
	for i := 0; i < len(k); i++ {
		if !isBareKeyChar(k[i]) {
			return quoteString(k)
		}
	}
	return k
}

func formatPath(path []string) string {
	parts := make([]string, len(path))
	for i, p := range path {
		parts[i] = formatKey(p)
	}
	return strings.Join(parts, ".")
}

// quoteString renders s as a TOML basic string.
func quoteString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func (e *tomlEdit) splice(start, end int, repl ...string) {
	lines := make([]string, 0, len(e.lines)-(end-start)+len(repl))
	lines = append(lines, e.lines[:start]...)
	lines = append(lines, repl...)
	lines = append(lines, e.lines[end:]...)
	e.lines = lines
}

// appendBlock adds lines at the end of the document, after a blank line.
func (e *tomlEdit) appendBlock(lines ...string) {
	if n := len(e.lines); n > 0 && strings.TrimSpace(e.lines[n-1]) != "" {
		e.lines = append(e.lines, "")
	}
	e.lines = append(e.lines, lines...)
}

// setString sets the key path (table parts, then the key) to the string value.
// An existing pair keeps its position, indentation and trailing comment; a new
// pair goes after the last pair of its table, or in a new table at the end.
func (e *tomlEdit) setString(path []string, value string) {
	stmts := e.scan()
	val := quoteString(value)
	for _, s := range stmts {
		if s.kind == stmtKeyValue && !s.inArray && slices.Equal(s.path, path) {
			e.splice(s.start, s.end+1, s.prefix+val+s.tail)
			return
		}
	}
	table, key := path[:len(path)-1], path[len(path)-1]
	line := formatKey(key) + " = " + val
	inTable := len(table) == 0
	at, indent := -1, ""
	for _, s := range stmts {
		if s.kind == stmtKeyValue {
			if inTable {
				at, indent = s.end+1, indentOf(e.lines[s.start])
			}
			continue
		}
		if inTable {
			break
		}
		if s.kind == stmtTable && slices.Equal(s.path, table) {
			inTable, at = true, s.end+1
		}
	}
	line = indent + line
	switch {
	case at >= 0:
		e.splice(at, at, line)
	case len(table) == 0:
		e.splice(0, 0, line)
	default:
		e.appendBlock("["+formatPath(table)+"]", line)
	}
}

// blank reports whether lines [from, to) are all empty.
func (e *tomlEdit) blank(from, to int) bool {
	for _, l := range e.lines[from:to] {
		if strings.TrimSpace(l) != "" {
			return false
		}
	}
	return true
}

func indentOf(s string) string { return s[:len(s)-len(strings.TrimLeft(s, " \t"))] }

// deleteKey removes the pair at path, if present.
func (e *tomlEdit) deleteKey(path []string) {
	for _, s := range e.scan() {
		if s.kind == stmtKeyValue && !s.inArray && slices.Equal(s.path, path) {
			e.splice(s.start, s.end+1)
			return
		}
	}
}

// field is one key of an array-of-tables element, written in order.
type field struct{ key, value string }

// setArrayTable replaces every [[path]] element (and an inline path = [...]
// pair) with entries. Comments before the first element are kept, and the new
// elements take its place; without one they go at the end of the document.
func (e *tomlEdit) setArrayTable(path []string, entries [][]field) {
	stmts := e.scan()
	type span struct{ start, end int }
	var spans []span
	at := -1
	for i := 0; i < len(stmts); i++ {
		s := stmts[i]
		switch {
		case s.kind == stmtKeyValue && hasPrefix(s.path, path):
			spans = append(spans, span{s.start, s.end + 1})
		case s.kind != stmtKeyValue && hasPrefix(s.path, path):
			// The element runs to its last pair; trailing comments belong to
			// whatever follows.
			end := s.end + 1
			for i+1 < len(stmts) && stmts[i+1].kind == stmtKeyValue {
				i++
				end = stmts[i].end + 1
			}
			// Blank lines between two elements go with the first one.
			if i+1 < len(stmts) && stmts[i+1].kind != stmtKeyValue &&
				hasPrefix(stmts[i+1].path, path) && e.blank(end, stmts[i+1].start) {
				end = stmts[i+1].start
			}
			if at < 0 {
				at = s.start
			}
			spans = append(spans, span{s.start, end})
		}
	}
	for i := len(spans) - 1; i >= 0; i-- {
		e.splice(spans[i].start, spans[i].end)
		if at > spans[i].start {
			at -= spans[i].end - spans[i].start
		}
	}

	var block []string
	for i, entry := range entries {
		if i > 0 {
			block = append(block, "")
		}
		block = append(block, "[["+formatPath(path)+"]]")
		for _, f := range entry {
			block = append(block, formatKey(f.key)+" = "+quoteString(f.value))
		}
	}
	if len(block) == 0 {
		// Do not leave two blank lines where the elements were.
		if at > 0 && at+1 <= len(e.lines) && e.blank(at-1, at+1) {
			e.splice(at, at+1)
		}
		return
	}
	if at < 0 {
		e.appendBlock(block...)
		return
	}
	if at < len(e.lines) && strings.TrimSpace(e.lines[at]) != "" {
		block = append(block, "")
	}
	e.splice(at, at, block...)
}
