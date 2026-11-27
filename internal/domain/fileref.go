package domain

import (
	"os"
	"path/filepath"
	"strings"
)

// File references in request bodies and header values.
//
// A stored body or header value of the form "@path" is read from that file
// when the request is sent. A value that starts with "@@" is sent as literal
// text with the first "@" removed, so "@@alice" sends "@alice". A lone "@"
// is sent as is.
//
// A relative path is resolved against a fixed base directory (Quark uses its
// config directory, ~/.quark by default) so the same request reads the same
// file from the TUI, `quark run` and scheduled runs, whatever the working
// directory. A path starting with "~/" is resolved against the home directory.

// ParseFileRef reports how a stored body or header value is sent: as the
// contents of the file at path (isFile true), or as the literal text.
func ParseFileRef(value string) (path string, isFile bool, literal string) {
	if !strings.HasPrefix(value, "@") || len(value) == 1 {
		return "", false, value
	}
	if strings.HasPrefix(value, "@@") {
		return "", false, value[1:]
	}
	return value[1:], true, ""
}

// EscapeFileRef returns the stored form of literal text, so that text which
// starts with "@" is sent as is instead of being read as a file reference.
// It is the inverse of ParseFileRef for literal values.
func EscapeFileRef(literal string) string {
	if strings.HasPrefix(literal, "@") && len(literal) > 1 {
		return "@" + literal
	}
	return literal
}

// ResolveFileRefPath returns the file a reference points to. An absolute
// path is returned unchanged, "~/" is expanded to the home directory, and any
// other relative path is joined to baseDir. An empty baseDir leaves relative
// paths relative to the working directory.
func ResolveFileRefPath(baseDir, path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	if filepath.IsAbs(path) || baseDir == "" {
		return path
	}
	return filepath.Join(baseDir, path)
}
