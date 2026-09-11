package tui

import "testing"

import "github.com/crazy-vedic/quark/internal/domain"

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

func TestHeaderValueSuggestion(t *testing.T) {
	if got := headerValueSuggestion("Content-Type", "app"); got != "application/json" {
		t.Fatalf("headerValueSuggestion = %q, want application/json", got)
	}
	if got := headerValueSuggestion("X-Custom", "app"); got != "" {
		t.Fatalf("custom header suggestion = %q, want empty", got)
	}
}

func TestVariableCompletion(t *testing.T) {
	input := "https://{{base_"
	got := variableCompletion(input, []string{"base_url", "token"})
	want := "https://{{base_url}}"
	if got != want {
		t.Fatalf("variableCompletion = %q, want %q", got, want)
	}
}

func TestURLSuggestionUsesLoadedRequests(t *testing.T) {
	m := Model{collectionRequests: map[string][]*domain.Request{
		"col": {{URL: "https://api.example.com/users"}},
	}}
	if got := m.urlSuggestion("https://api.example.com/u"); got != "https://api.example.com/users" {
		t.Fatalf("urlSuggestion = %q, want loaded request URL", got)
	}
}
