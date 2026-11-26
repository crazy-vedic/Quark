package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const commentedConfig = `# Quark configuration
# kept by hand

[ui]
# colours
theme = 'dark'   # my favourite
editor = "nano"

[http]
timeout = "60s" # slow API
headers = [
  "a", # first
  "b",
]

# certificates for the staging API
[[http.client_certificates]]
host = "old.example" # old host
file = "old.p12"

[[http.client_certificates]]
host = "older.example"
file = "older.p12"

# backups
[backup]
keep_last = 3
`

func TestTOMLEdit_SetStringKeepsCommentsAndLayout(t *testing.T) {
	e := newTOMLEdit(commentedConfig)
	e.setString([]string{"ui", "theme"}, "light")
	e.setString([]string{"ui", "last_request_id"}, "req-1")
	e.setString([]string{"backup", "note"}, `a "b" \c`)
	e.setString([]string{"keybindings", "quit"}, "ctrl+c")
	e.setString([]string{"top"}, "level")

	want := `top = "level"
# Quark configuration
# kept by hand

[ui]
# colours
theme = "light"   # my favourite
editor = "nano"
last_request_id = "req-1"
` + commentedConfig[len("# Quark configuration\n# kept by hand\n\n[ui]\n# colours\ntheme = 'dark'   # my favourite\neditor = \"nano\"\n"):len(commentedConfig)-1] + `
note = "a \"b\" \\c"

[keybindings]
quit = "ctrl+c"
`
	assert.Equal(t, want, e.String())
}

func TestTOMLEdit_SetStringReplacesMultiLineValue(t *testing.T) {
	e := newTOMLEdit(commentedConfig)
	e.setString([]string{"http", "headers"}, "x")
	assert.Contains(
		t,
		e.String(),
		"timeout = \"60s\" # slow API\nheaders = \"x\"\n\n# certificates",
	)
}

func TestTOMLEdit_DeleteKey(t *testing.T) {
	e := newTOMLEdit(commentedConfig)
	e.deleteKey([]string{"ui", "editor"})
	e.deleteKey([]string{"ui", "missing"})
	e.deleteKey(
		[]string{"http", "client_certificates", "host"},
	) // inside an array element: untouched
	got := e.String()
	assert.NotContains(t, got, "editor")
	assert.Contains(t, got, "theme = 'dark'   # my favourite\n\n[http]")
	assert.Contains(t, got, `host = "old.example" # old host`)
}

func TestTOMLEdit_SetArrayTableReplacesInPlace(t *testing.T) {
	e := newTOMLEdit(commentedConfig)
	e.setArrayTable([]string{"http", "client_certificates"}, [][]field{
		{{"host", "new.example"}, {"file", "new.p12"}},
		{{"host", "b.example"}, {"file", "b.pem"}, {"key_file", "b.key"}},
	})
	want := `  "b",
]

# certificates for the staging API
[[http.client_certificates]]
host = "new.example"
file = "new.p12"

[[http.client_certificates]]
host = "b.example"
file = "b.pem"
key_file = "b.key"

# backups
[backup]
`
	assert.Contains(t, e.String(), want)
	assert.NotContains(t, e.String(), "old")
}

func TestTOMLEdit_SetArrayTableEmptyAndAppend(t *testing.T) {
	e := newTOMLEdit(commentedConfig)
	e.setArrayTable([]string{"http", "client_certificates"}, nil)
	assert.NotContains(t, e.String(), "client_certificates")
	assert.Contains(t, e.String(), "# certificates for the staging API\n\n# backups\n[backup]")

	e = newTOMLEdit("[ui]\ntheme = \"dark\"\n")
	e.setArrayTable([]string{"http", "client_certificates"}, [][]field{{{"host", "h"}}})
	assert.Equal(
		t,
		"[ui]\ntheme = \"dark\"\n\n[[http.client_certificates]]\nhost = \"h\"\n",
		e.String(),
	)

	e = newTOMLEdit("")
	e.setArrayTable([]string{"http", "client_certificates"}, [][]field{{{"host", "h"}}})
	assert.Equal(t, "[[http.client_certificates]]\nhost = \"h\"\n", e.String())
}

func TestTOMLEdit_InlineArrayMovesToEnd(t *testing.T) {
	e := newTOMLEdit("[http]\nclient_certificates = [{host = \"a\"}]\ntimeout = \"1s\"\n")
	e.setArrayTable([]string{"http", "client_certificates"}, [][]field{{{"host", "b"}}})
	assert.Equal(
		t,
		"[http]\ntimeout = \"1s\"\n\n[[http.client_certificates]]\nhost = \"b\"\n",
		e.String(),
	)
}

func TestTOMLEdit_KeysAndStrings(t *testing.T) {
	e := newTOMLEdit("[ \"my table\" . 'x' ]\n\"a.b\" = \"#not a comment\" # real\n" +
		"ml = \"\"\"\n[not a header]\n\"\"\"\n")
	e.setString([]string{"my table", "x", "a.b"}, "v")
	e.setString([]string{"my table", "x", "new key"}, "\t\x01")
	assert.Equal(t, "[ \"my table\" . 'x' ]\n\"a.b\" = \"v\" # real\n"+
		"ml = \"\"\"\n[not a header]\n\"\"\"\n\"new key\" = \"\\t\\u0001\"\n", e.String())
}

func TestTOMLEdit_KeepsCRLF(t *testing.T) {
	e := newTOMLEdit("[ui]\r\ntheme = \"dark\"\r\n")
	e.setString([]string{"ui", "theme"}, "light")
	assert.Equal(t, "[ui]\r\ntheme = \"light\"\r\n", e.String())
}

func TestTOMLEdit_KeepsIndentation(t *testing.T) {
	e := newTOMLEdit("[ui]\n  theme = \"dark\"\n")
	e.setString([]string{"ui", "editor"}, "vi")
	assert.Equal(t, "[ui]\n  theme = \"dark\"\n  editor = \"vi\"\n", e.String())
}
