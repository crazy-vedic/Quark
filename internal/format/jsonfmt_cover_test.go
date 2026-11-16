package format

import (
	"bytes"
	"testing"
)

func TestWriteJSONStringEscapes(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"quote and backslash", `a"b\c`, `"a\"b\\c"`},
		{"newline carriage tab", "a\nb\rc\td", `"a\nb\rc\td"`},
		{"backspace and formfeed", "a\bb\fc", `"a\bb\fc"`},
		{"other control char", "a\x01b", `"a\u0001b"`},
		{"unicode passthrough", "héllo✓", `"héllo✓"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			writeJSONString(&buf, tt.in)
			if buf.String() != tt.want {
				t.Fatalf("got %s, want %s", buf.String(), tt.want)
			}
		})
	}
}

func TestFormatJSONIfValidStructuresAndIndent(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"tab indent preserved", "{\n\t\"a\":1,\n\t\"b\": 2\n}", "{\n\t\"a\": 1,\n\t\"b\": 2\n}"},
		{
			"four space minified",
			`{"a":{"b":[1,2]}}`,
			"{\n  \"a\": {\n    \"b\": [\n      1,\n      2\n    ]\n  }\n}",
		},
		{"empty object and array", `{"a":{},"b":[]}`, "{\n  \"a\": {},\n  \"b\": []\n}"},
		{"literals", `[true,false,null,"x"]`, "[\n  true,\n  false,\n  null,\n  \"x\"\n]"},
		{"top-level array", `[{"a":1}]`, "[\n  {\n    \"a\": 1\n  }\n]"},
		{"string with braces is not indent source", `{"a":"{"}`, "{\n  \"a\": \"{\"\n}"},
		{"brace without newline defaults", `{"a":1}`, "{\n  \"a\": 1\n}"},
		{"blank line after brace defaults", "{\n\n\"a\":1}", "{\n  \"a\": 1\n}"},
		{"escaped quote inside string", `{"a":"x\"y"}`, "{\n  \"a\": \"x\\\"y\"\n}"},
		{"negative and exponent numbers", `[-1.5e3, 0]`, "[\n  -1.5e3,\n  0\n]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := FormatJSONIfValid(tt.in)
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
			if !changed {
				t.Fatalf("expected changed=true for %q", tt.in)
			}
		})
	}
}

func TestFormatJSONIfValidWhitespaceOnly(t *testing.T) {
	got, changed := FormatJSONIfValid("   \n\t ")
	if changed || got != "   \n\t " {
		t.Fatalf("whitespace input should be unchanged, got %q changed=%v", got, changed)
	}
}
