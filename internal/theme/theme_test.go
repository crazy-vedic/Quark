package theme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinsFollowTerminalBackground(t *testing.T) {
	darkAuto, err := Resolve(t.TempDir(), "auto", true)
	if err != nil {
		t.Fatal(err)
	}
	lightAuto, err := Resolve(t.TempDir(), "auto", false)
	if err != nil {
		t.Fatal(err)
	}
	if darkAuto.Variant != "dark" || lightAuto.Variant != "light" ||
		darkAuto.Foreground == lightAuto.Foreground {
		t.Fatalf("auto did not select distinct palettes: %#v %#v", darkAuto, lightAuto)
	}
	if darkAuto.Background != "" || lightAuto.Background != "" {
		t.Fatal("auto should inherit the terminal background")
	}
	forcedLight, err := Resolve(t.TempDir(), "light", true)
	if err != nil || forcedLight.Background == "" {
		t.Fatalf("forced light theme needs its own background: %#v %v", forcedLight, err)
	}
	transparent, err := Resolve(t.TempDir(), "transparent", false)
	if err != nil || transparent.Variant != "light" {
		t.Fatalf("transparent should follow terminal contrast: %#v %v", transparent, err)
	}
}

func TestCustomThemeExtendsAndValidates(t *testing.T) {
	dir := t.TempDir()
	themesDir := filepath.Join(dir, "themes")
	if err := os.MkdirAll(themesDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(themesDir, "ocean.toml")
	if err := os.WriteFile(
		path,
		[]byte(
			"extends = \"light\"\n[colors]\naccent = \"#123abc\"\nget = \"#15803d\"\nsyntax = \"github\"\n",
		),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	got, err := Resolve(dir, "ocean", true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Variant != "light" || got.Accent != "#123abc" || got.GET != "#15803d" ||
		got.Foreground != light.Foreground {
		t.Fatalf("unexpected resolved palette: %#v", got)
	}
	names, err := List(dir)
	if err != nil || names[len(names)-1] != "ocean" {
		t.Fatalf("custom theme missing from list: %v %v", names, err)
	}
	before, err := FileHash(dir, "ocean")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		path,
		[]byte("extends = \"light\"\n[colors]\naccent = \"#abcdef\"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	after, err := FileHash(dir, "ocean")
	if err != nil || before == after {
		t.Fatalf("file change not detected: %v", err)
	}
	for _, input := range []string{
		"extends = \"auto\"\n",
		"extends = \"dark\"\n[colors]\naccent = \"red\"\n",
		"extends = \"dark\"\n[colors]\nsyntax = \"missing-style-xyz\"\n",
		"extends = \"dark\"\n[colors]\nunknown = \"#123456\"\n",
	} {
		if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Resolve(dir, "ocean", true); err == nil {
			t.Errorf("expected invalid theme to fail: %q", input)
		}
	}
	if _, err := Resolve(
		dir,
		"../config",
		true,
	); err == nil ||
		!strings.Contains(err.Error(), "invalid theme name") {
		t.Fatalf("unsafe name accepted: %v", err)
	}
}
