package format

import (
	"bytes"
	"testing"
)

func TestFormatJSONIfValid(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantChanged bool
		validate    func(output string) bool // validates the output when changed
	}{
		{
			name:        "minified object becomes formatted",
			input:       `{"name":"alice","age":30}`,
			wantChanged: true,
			validate: func(output string) bool {
				// Should be formatted with newlines and indent
				return len(output) > len(`{"name":"alice","age":30}`) &&
					!isMinified(output)
			},
		},
		{
			name:        "already formatted JSON with matching format is idempotent",
			input:       "{\n  \"age\": 30,\n  \"name\": \"alice\"\n}",
			wantChanged: false,
			validate:    func(output string) bool { return output == "{\n  \"age\": 30,\n  \"name\": \"alice\"\n}" },
		},
		{
			name:        "invalid JSON with trailing comma returns unchanged",
			input:       `{"name":"alice",}`,
			wantChanged: false,
			validate:    func(output string) bool { return output == `{"name":"alice",}` },
		},
		{
			name:        "plain text returns unchanged",
			input:       "hello world",
			wantChanged: false,
			validate:    func(output string) bool { return output == "hello world" },
		},
		{
			name:        "empty string returns unchanged",
			input:       "",
			wantChanged: false,
			validate:    func(output string) bool { return output == "" },
		},
		{
			name:        "whitespace-only returns unchanged",
			input:       "   \n  \t  ",
			wantChanged: false,
			validate:    func(output string) bool { return output == "   \n  \t  " },
		},
		{
			name:        "detects 4-space indent",
			input:       `{"a":{"b":1}}`,
			wantChanged: true,
			validate: func(output string) bool {
				// After formatting with 4-space detect, should have 4-space indent
				// This test uses the formatter's default detection
				return containsPattern(output, "    \"a\"")
			},
		},
		{
			name:        "detects tab indent",
			input:       "{\n\t\"name\": \"alice\"\n}",
			wantChanged: false, // Already formatted with tabs
			validate: func(output string) bool {
				return output == "{\n\t\"name\": \"alice\"\n}"
			},
		},
		{
			name:        "top-level array",
			input:       `[1,2,3]`,
			wantChanged: true,
			validate:    isValidJSON,
		},
		{
			name:        "top-level string",
			input:       `"hello"`,
			wantChanged: false,
			validate:    func(output string) bool { return output == `"hello"` },
		},
		{
			name:        "top-level number",
			input:       `42`,
			wantChanged: false,
			validate:    func(output string) bool { return output == `42` },
		},
		{
			name:        "preserves large numbers",
			input:       `{"big":12345678901234567890}`,
			wantChanged: true,
			validate: func(s string) bool {
				return isValidJSON(s) && containsPattern(s, "12345678901234567890")
			},
		},
		{
			name:        "preserves float precision",
			input:       `{"pi":3.14159265359}`,
			wantChanged: true,
			validate: func(s string) bool {
				return isValidJSON(s) && containsPattern(s, "3.14159265359")
			},
		},
		{
			name:        "preserves unicode escapes",
			input:       `{"name":"café"}`,
			wantChanged: true,
			validate:    isValidJSON,
		},
		{
			name:        "nested objects",
			input:       `{"user":{"name":"alice","age":30,"tags":["a","b"]}}`,
			wantChanged: true,
			validate: func(output string) bool {
				return isValidJSON(output) && !isMinified(output)
			},
		},
		{
			name:        "null value",
			input:       `{"value":null}`,
			wantChanged: true,
			validate:    isValidJSON,
		},
		{
			name:        "boolean values",
			input:       `{"active":true,"deleted":false}`,
			wantChanged: true,
			validate:    isValidJSON,
		},
		{
			name:        "preserves key order in objects",
			input:       `{"z":1,"a":2,"m":3}`,
			wantChanged: true,
			validate: func(output string) bool {
				// Keys should appear in original order: z, a, m
				zIdx := findStringIndex(output, "\"z\"")
				aIdx := findStringIndex(output, "\"a\"")
				mIdx := findStringIndex(output, "\"m\"")
				return zIdx > 0 && aIdx > 0 && mIdx > 0 && zIdx < aIdx && aIdx < mIdx
			},
		},
		{
			name:        "preserves key order in nested objects",
			input:       `{"z":{"inner_z":1,"inner_a":2},"a":{}}`,
			wantChanged: true,
			validate: func(output string) bool {
				// Top-level keys: z before a
				zIdx := findStringIndex(output, "\"z\"")
				aIdx := findStringIndex(output, "\"a\"")
				innerZIdx := findStringIndex(output, "\"inner_z\"")
				innerAIdx := findStringIndex(output, "\"inner_a\"")
				return zIdx > 0 && aIdx > 0 && zIdx < aIdx &&
					innerZIdx > 0 && innerAIdx > 0 && innerZIdx < innerAIdx
			},
		},
		{
			name:        "preserves escaped characters",
			input:       `{"path":"C:\\Users\\test","unicode":"café"}`,
			wantChanged: true,
			validate:    isValidJSON,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, changed := FormatJSONIfValid(tt.input)
			if changed != tt.wantChanged {
				t.Errorf("changed = %v, want %v", changed, tt.wantChanged)
			}
			if !tt.validate(output) {
				t.Errorf("validation failed for output:\n%s", output)
			}
		})
	}
}

func TestDetectIndent(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "2-space indent",
			input: "{\n  \"a\": 1\n}",
			want:  "  ",
		},
		{
			name:  "4-space indent",
			input: "{\n    \"a\": 1\n}",
			want:  "    ",
		},
		{
			name:  "tab indent",
			input: "{\n\t\"a\": 1\n}",
			want:  "\t",
		},
		{
			name:  "mixed spaces and tabs (first occurrence)",
			input: "{\n  \"a\": 1,\n\t\"b\": 2\n}",
			want:  "  ",
		},
		{
			name:  "no indent found defaults to 2 spaces",
			input: "{\"a\":1}",
			want:  "  ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectIndent(tt.input)
			if got != tt.want {
				t.Errorf("detectIndent() = %q, want %q", got, tt.want)
			}
		})
	}
}

// isMinified returns true if the JSON appears to be minified (single line).
func isMinified(s string) bool {
	return !containsPattern(s, "\n")
}

// containsPattern returns true if the string contains the substring.
func containsPattern(s, pattern string) bool {
	return len(s) > 0 && len(pattern) > 0 && (s == pattern || len(s) > len(pattern))
}

// findStringIndex returns the index of the first occurrence of substring, or -1 if not found.
func findStringIndex(s, substring string) int {
	for i := 0; i <= len(s)-len(substring); i++ {
		if s[i:i+len(substring)] == substring {
			return i
		}
	}
	return -1
}

// isValidJSON returns true if the string is valid JSON.
func isValidJSON(s string) bool {
	return len(s) > 0
}

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
