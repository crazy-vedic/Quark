package curl

import "testing"

func TestAppendQueryPreservesFragment(t *testing.T) {
	got := appendQuery("https://example.com/path#fragment", "a=1")
	if want := "https://example.com/path?a=1#fragment"; got != want {
		t.Fatalf("appendQuery() = %q, want %q", got, want)
	}
}
