package tui

import "testing"

func TestHeaderNameSuggestion(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "content prefix", input: "cont", want: "Content-Length"},
		{name: "case insensitive", input: "auth", want: "Authorization"},
		{name: "canonical complete name", input: "Content-Type", want: ""},
		{name: "unknown header", input: "X-Custom", want: ""},
		{name: "empty", input: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := headerNameSuggestion(tt.input); got != tt.want {
				t.Fatalf("headerNameSuggestion(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
