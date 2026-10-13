// Package theme defines Quark's semantic terminal colors and loads user themes.
package theme

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/alecthomas/chroma/v2/styles"
)

// Palette contains colors for UI roles, not individual screens. An empty
// background inherits the terminal default.
type Palette struct {
	Foreground   string `toml:"foreground"`
	Background   string `toml:"background"`
	Muted        string `toml:"muted"`
	Accent       string `toml:"accent"`
	Info         string `toml:"info"`
	Success      string `toml:"success"`
	Warning      string `toml:"warning"`
	Error        string `toml:"error"`
	ActiveBorder string `toml:"active_border"`
	Match        string `toml:"match"`
	GET          string `toml:"get"`
	POST         string `toml:"post"`
	PUT          string `toml:"put"`
	DELETE       string `toml:"delete"`
	Syntax       string `toml:"syntax"`
}

// Theme is a resolved theme. Name is the selection; Variant is the concrete
// palette used by auto and transparent themes.
type Theme struct {
	Name, Variant string
	Palette
}

var validName = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
var validColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

var dark = Palette{
	Foreground: "#c0caf5", Background: "#1a1b26", Muted: "#737aa2", Accent: "#7aa2f7",
	Info: "#7dcfff", Success: "#9ece6a", Warning: "#e0af68",
	Error: "#f7768e", ActiveBorder: "#ffffff", Match: "#e0af68",
	GET: "#9ece6a", POST: "#7aa2f7", PUT: "#e0af68", DELETE: "#f7768e",
	Syntax: "monokai",
}

var light = Palette{
	Foreground: "#24292f", Background: "#f6f8fa", Muted: "#57606a", Accent: "#0550ae",
	Info: "#0969da", Success: "#116329", Warning: "#9a6700",
	Error: "#cf222e", ActiveBorder: "#24292f", Match: "#9a6700",
	GET: "#116329", POST: "#0550ae", PUT: "#9a6700", DELETE: "#cf222e",
	Syntax: "github",
}

// Builtins lists selection names in a stable order.
func Builtins() []string { return []string{"auto", "dark", "light", "transparent"} }

// List returns built-in and valid local theme names. A malformed theme is
// omitted here; Resolve reports its detailed error when selected.
func List(configDir string) ([]string, error) {
	names := Builtins()
	entries, err := os.ReadDir(filepath.Join(configDir, "themes"))
	if os.IsNotExist(err) {
		return names, nil
	}
	if err != nil {
		return nil, err
	}
	custom := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := strings.TrimSuffix(entry.Name(), ".toml")
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".toml") &&
			validName.MatchString(name) && !isBuiltin(name) {
			custom = append(custom, name)
		}
	}
	sort.Strings(custom)
	return append(names, custom...), nil
}

func isBuiltin(name string) bool {
	for _, builtin := range Builtins() {
		if name == builtin {
			return true
		}
	}
	return false
}

// IsBuiltin reports whether a selection needs a local theme file.
func IsBuiltin(name string) bool { return isBuiltin(name) }

// FileHash detects editor writes, including atomic replacements with unchanged
// timestamps. Call only for a validated custom name.
func FileHash(configDir, name string) ([32]byte, error) {
	if !validName.MatchString(name) || isBuiltin(name) {
		return [32]byte{}, fmt.Errorf("%q is not a custom theme", name)
	}
	raw, err := os.ReadFile(filepath.Join(configDir, "themes", name+".toml"))
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(raw), nil
}

// Resolve validates a selection and overlays a local TOML theme on its base.
// darkBackground comes from terminal detection, so auto works independently of
// the terminal's color depth and requires no process-global mutable state.
func Resolve(configDir, name string, darkBackground bool) (Theme, error) {
	if name == "" {
		name = "auto"
	}
	if !validName.MatchString(name) {
		return Theme{}, fmt.Errorf("invalid theme name %q", name)
	}
	base := dark
	variant := "dark"
	if (name == "auto" || name == "transparent") && !darkBackground || name == "light" {
		base, variant = light, "light"
	}
	if isBuiltin(name) {
		if name == "auto" || name == "transparent" {
			base.Background = ""
		}
		return Theme{Name: name, Variant: variant, Palette: base}, nil
	}
	path := filepath.Join(configDir, "themes", name+".toml")
	var doc struct {
		Extends string  `toml:"extends"`
		Colors  Palette `toml:"colors"`
	}
	md, err := toml.DecodeFile(path, &doc)
	if err != nil {
		return Theme{}, fmt.Errorf("load theme %q: %w", name, err)
	}
	for _, key := range md.Undecoded() {
		return Theme{}, fmt.Errorf("theme %q: unknown key %s", name, key)
	}
	if doc.Extends == "" {
		return Theme{}, fmt.Errorf("theme %q: extends must be dark or light", name)
	}
	switch doc.Extends {
	case "dark":
		base, variant = dark, "dark"
	case "light":
		base, variant = light, "light"
	default:
		return Theme{}, fmt.Errorf("theme %q: extends must be dark or light", name)
	}
	if err := applyOverrides(&base, doc.Colors); err != nil {
		return Theme{}, fmt.Errorf("theme %q: %w", name, err)
	}
	return Theme{Name: name, Variant: variant, Palette: base}, nil
}

func applyOverrides(dst *Palette, src Palette) error {
	values := []struct {
		key      string
		value    *string
		override string
	}{
		{"foreground", &dst.Foreground, src.Foreground}, {"muted", &dst.Muted, src.Muted},
		{"accent", &dst.Accent, src.Accent}, {"info", &dst.Info, src.Info},
		{"success", &dst.Success, src.Success}, {"warning", &dst.Warning, src.Warning},
		{"error", &dst.Error, src.Error}, {"active_border", &dst.ActiveBorder, src.ActiveBorder},
		{"match", &dst.Match, src.Match}, {"get", &dst.GET, src.GET},
		{"post", &dst.POST, src.POST}, {"put", &dst.PUT, src.PUT}, {"delete", &dst.DELETE, src.DELETE},
	}
	if src.Background == "transparent" {
		dst.Background = ""
	} else if src.Background != "" {
		if !validColor.MatchString(src.Background) {
			return fmt.Errorf("colors.background must be a six-digit hex color or transparent")
		}
		dst.Background = src.Background
	}
	for _, item := range values {
		if item.override == "" {
			continue
		}
		if !validColor.MatchString(item.override) {
			return fmt.Errorf("colors.%s must be a six-digit hex color", item.key)
		}
		*item.value = item.override
	}
	if src.Syntax != "" {
		// Chroma falls back gracefully for unknown styles, but a typo in a
		// saved theme should be reported at load time.
		if _, ok := styles.Registry[strings.ToLower(src.Syntax)]; !ok {
			return fmt.Errorf("colors.syntax %q is not a Chroma style", src.Syntax)
		}
		dst.Syntax = src.Syntax
	}
	return nil
}
