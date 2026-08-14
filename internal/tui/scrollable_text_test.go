package tui

import "testing"

func TestScrollableTextViewWrapsAndClips(t *testing.T) {
	var text scrollableText
	text.SetContent("abcdef")

	if got := text.View(3, 2); got != "abc\ndef" {
		t.Fatalf("View() = %q, want %q", got, "abc\\ndef")
	}
	if got := text.View(20, 2); got != "abcdef" {
		t.Fatalf("wide View() = %q, want %q", got, "abcdef")
	}
}

func TestScrollableTextScrollClampsToBothEnds(t *testing.T) {
	var text scrollableText
	text.SetContent("one\ntwo\nthree\nfour\nfive")

	text.Scroll(-100, 20, 2)
	if text.offset != 0 {
		t.Fatalf("negative scroll offset = %d, want 0", text.offset)
	}

	text.Scroll(100, 20, 2)
	if text.offset != 3 {
		t.Fatalf("positive scroll offset = %d, want 3", text.offset)
	}
	if got := text.View(20, 2); got != "four\nfive" {
		t.Fatalf("bottom View() = %q, want %q", got, "four\\nfive")
	}
}

func TestScrollableTextShortContentCannotScroll(t *testing.T) {
	var text scrollableText
	text.SetContent("one\ntwo")
	text.Scroll(1, 20, 5)

	if text.offset != 0 {
		t.Fatalf("short-content offset = %d, want 0", text.offset)
	}
	if got := text.View(20, 5); got != "one\ntwo" {
		t.Fatalf("short-content View() = %q, want %q", got, "one\\ntwo")
	}
}

func TestScrollableTextSetContentResetsOnlyWhenContentChanges(t *testing.T) {
	var text scrollableText
	text.SetContent("one\ntwo\nthree")
	text.Scroll(1, 20, 1)
	if text.offset != 1 {
		t.Fatalf("setup offset = %d, want 1", text.offset)
	}

	text.SetContent("one\ntwo\nthree")
	if text.offset != 1 {
		t.Fatalf("same-content offset = %d, want 1", text.offset)
	}

	text.SetContent("new")
	if text.offset != 0 {
		t.Fatalf("changed-content offset = %d, want 0", text.offset)
	}
}

func TestScrollableTextCachesWrappedLinesPerWidth(t *testing.T) {
	var text scrollableText
	text.SetContent("one\ntwo\nthree")

	text.Scroll(1, 10, 1)
	first := text.wrapped
	text.Scroll(1, 10, 1)
	if len(text.wrapped) == 0 || &text.wrapped[0] != &first[0] {
		t.Fatal("scrolling at the same width should reuse wrapped lines")
	}

	text.Scroll(1, 20, 1)
	if text.wrappedFor != 20 {
		t.Fatalf("wrapped width = %d, want 20", text.wrappedFor)
	}
}

func TestScrollableTextCachesFormattedContentBySource(t *testing.T) {
	var text scrollableText
	calls := 0
	format := func() string {
		calls++
		return "formatted body"
	}

	text.SetFormattedContent("response-1", format)
	text.SetFormattedContent("response-1", format)
	if calls != 1 {
		t.Fatalf("formatter calls = %d, want 1 for the same source", calls)
	}

	text.Scroll(1, 20, 1)
	text.SetFormattedContent("response-1", format)
	if calls != 1 {
		t.Fatalf("formatter calls after scrolling = %d, want 1", calls)
	}

	text.SetFormattedContent("response-2", format)
	if calls != 2 {
		t.Fatalf("formatter calls after source change = %d, want 2", calls)
	}
	if text.offset != 0 {
		t.Fatalf("offset after source change = %d, want 0", text.offset)
	}
}

func TestScrollableTextHandlesEmptyAndInvalidViewports(t *testing.T) {
	var text scrollableText
	text.SetContent("content")
	text.Scroll(5, 0, 0)
	if text.offset != 0 {
		t.Fatalf("invalid-viewport scroll offset = %d, want 0", text.offset)
	}
	for _, tc := range []struct {
		width, height int
	}{
		{0, 5},
		{5, 0},
		{-1, 5},
		{5, -1},
	} {
		if got := text.View(tc.width, tc.height); got != "" {
			t.Fatalf("View(%d, %d) = %q, want empty", tc.width, tc.height, got)
		}
	}

	text.SetContent("")
	if got := text.View(5, 5); got != "" {
		t.Fatalf("empty-content View() = %q, want empty", got)
	}
}

func TestWrappedTextLinesPreservesBlankLines(t *testing.T) {
	got := wrappedTextLines("first\n\nthird", 20)
	want := []string{"first", "", "third"}
	if len(got) != len(want) {
		t.Fatalf("wrapped line count = %d, want %d (%q)", len(got), len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("wrapped line %d = %q, want %q", i, got[i], want[i])
		}
	}
}
