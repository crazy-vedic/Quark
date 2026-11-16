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

func writeThemeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	themes := filepath.Join(dir, "themes")
	if err := os.MkdirAll(themes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(themes, name+".toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestListReturnsBuiltinsAndValidCustomThemes(t *testing.T) {
	dir := t.TempDir()

	names, err := List(dir)
	if err != nil || len(names) != len(Builtins()) {
		t.Fatalf("no themes dir: got %v err=%v", names, err)
	}

	writeThemeFile(t, dir, "zeta", "")
	writeThemeFile(t, dir, "alpha", "")
	writeThemeFile(t, dir, "Bad Name", "")
	writeThemeFile(t, dir, "dark", "") // builtin names are not duplicated
	writeThemeFile(t, dir, "notes", "")
	if err := os.WriteFile(filepath.Join(dir, "themes", "readme.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "themes", "folder.toml"), 0o755); err != nil {
		t.Fatal(err)
	}

	names, err = List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	joined := strings.Join(names, ",")
	if !strings.HasSuffix(joined, ",alpha,notes,zeta") {
		t.Fatalf("custom themes not sorted/appended: %s", joined)
	}
	if strings.Count(joined, "dark") != 1 || strings.Contains(joined, "Bad Name") {
		t.Fatalf("builtins duplicated or invalid name listed: %s", joined)
	}
}

func TestListPropagatesReadErrors(t *testing.T) {
	dir := t.TempDir()
	// A regular file where the themes directory should be makes ReadDir fail
	// with something other than "not exist".
	if err := os.WriteFile(filepath.Join(dir, "themes"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := List(dir); err == nil {
		t.Fatal("expected error when themes path is a file")
	}
}

func TestIsBuiltin(t *testing.T) {
	for _, name := range Builtins() {
		if !IsBuiltin(name) {
			t.Errorf("%s should be builtin", name)
		}
	}
	if IsBuiltin("custom") {
		t.Error("custom should not be builtin")
	}
}

func TestFileHash(t *testing.T) {
	dir := t.TempDir()
	writeThemeFile(t, dir, "mine", `extends = "dark"`)

	h1, err := FileHash(dir, "mine")
	if err != nil {
		t.Fatalf("FileHash: %v", err)
	}
	writeThemeFile(t, dir, "mine", `extends = "light"`)
	h2, err := FileHash(dir, "mine")
	if err != nil || h1 == h2 {
		t.Fatalf("hash should change after edit: err=%v", err)
	}

	if _, err := FileHash(dir, "dark"); err == nil {
		t.Error("builtin names are not custom themes")
	}
	if _, err := FileHash(dir, "Not Valid"); err == nil {
		t.Error("invalid names are rejected")
	}
	if _, err := FileHash(dir, "missing"); err == nil {
		t.Error("missing file returns an error")
	}
}

func TestResolveBuiltinSelections(t *testing.T) {
	cases := []struct {
		name, sel string
		darkBG    bool
		variant   string
		bgEmpty   bool
	}{
		{"empty defaults to auto dark", "", true, "dark", true},
		{"auto on light terminal", "auto", false, lightName, true},
		{"transparent on dark terminal", "transparent", true, "dark", true},
		{"explicit dark", "dark", false, "dark", false},
		{"explicit light", lightName, true, lightName, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			th, err := Resolve(t.TempDir(), tc.sel, tc.darkBG)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if th.Variant != tc.variant {
				t.Errorf("variant=%s want %s", th.Variant, tc.variant)
			}
			if (th.Background == "") != tc.bgEmpty {
				t.Errorf("background=%q, bgEmpty=%v", th.Background, tc.bgEmpty)
			}
			if th.Selection == "" {
				t.Error("builtin palette must define a selection color")
			}
		})
	}
}

func TestResolveRejectsInvalidThemes(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"badname":    "",
		"nope":       "",
		"unknownkey": "extends = \"dark\"\nwhat = 1\n",
		"noextends":  "[colors]\nmatch = \"#ffffff\"\n",
		"badextends": "extends = \"solarized\"\n",
		"badcolor":   "extends = \"dark\"\n[colors]\nerror = \"red\"\n",
		"badbg":      "extends = \"dark\"\n[colors]\nbackground = \"nope\"\n",
		"badsyntax":  "extends = \"dark\"\n[colors]\nsyntax = \"not-a-style\"\n",
		"malformed":  "extends = [",
	}
	writeThemeFile(t, dir, "unknownkey", cases["unknownkey"])
	writeThemeFile(t, dir, "noextends", cases["noextends"])
	writeThemeFile(t, dir, "badextends", cases["badextends"])
	writeThemeFile(t, dir, "badcolor", cases["badcolor"])
	writeThemeFile(t, dir, "badbg", cases["badbg"])
	writeThemeFile(t, dir, "badsyntax", cases["badsyntax"])
	writeThemeFile(t, dir, "malformed", cases["malformed"])

	for name := range cases {
		if name == "badname" || name == "nope" {
			continue
		}
		if _, err := Resolve(dir, name, true); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	if _, err := Resolve(dir, "Bad Name", true); err == nil {
		t.Error("invalid name must be rejected")
	}
	if _, err := Resolve(dir, "nope", true); err == nil {
		t.Error("missing custom theme must be rejected")
	}
}

func TestResolveCustomThemeOverlaysOverrides(t *testing.T) {
	dir := t.TempDir()
	writeThemeFile(t, dir, "ocean", `extends = "light"
[colors]
background = "#112233"
foreground = "#000001"
muted = "#000002"
accent = "#000003"
info = "#000004"
success = "#000005"
warning = "#000006"
error = "#000007"
active_border = "#000008"
match = "#000009"
selection = "#00000a"
get = "#00000b"
post = "#00000c"
put = "#00000d"
delete = "#00000e"
syntax = "monokai"
`)
	th, err := Resolve(dir, "ocean", true)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	p := th.Palette
	if th.Variant != lightName || p.Background != "#112233" || p.Selection != "#00000a" ||
		p.DELETE != "#00000e" || p.Syntax != "monokai" || p.Foreground != "#000001" {
		t.Fatalf("overrides not applied: %+v", p)
	}

	writeThemeFile(t, dir, "clear", "extends = \"dark\"\n[colors]\nbackground = \"transparent\"\n")
	th, err = Resolve(dir, "clear", true)
	if err != nil || th.Background != "" || th.Variant != "dark" {
		t.Fatalf("transparent background override: err=%v palette=%+v", err, th.Palette)
	}
}

func TestApplyOverridesDirectErrors(t *testing.T) {
	dst := dark
	if err := applyOverrides(&dst, Palette{Background: "bogus"}); err == nil {
		t.Error("bad background must fail")
	}
	dst = dark
	if err := applyOverrides(&dst, Palette{Muted: "#12"}); err == nil {
		t.Error("bad override color must fail")
	}
	dst = dark
	if err := applyOverrides(&dst, Palette{Syntax: "nope"}); err == nil {
		t.Error("unknown syntax style must fail")
	}
}
