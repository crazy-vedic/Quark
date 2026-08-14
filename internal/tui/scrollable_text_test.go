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
	first := text.ensureCache().wrapped[10]
	text.Scroll(1, 10, 1)
	if len(text.ensureCache().wrapped[10]) == 0 || &text.ensureCache().wrapped[10][0] != &first[0] {
		t.Fatal("scrolling at the same width should reuse wrapped lines")
	}

	text.Scroll(1, 20, 1)
	if _, ok := text.ensureCache().wrapped[20]; !ok {
		t.Fatal("wrapped lines were not cached for width 20")
	}
	text.Scroll(-1, 10, 1)
	if &text.ensureCache().wrapped[10][0] != &first[0] {
		t.Fatal("returning to width 10 did not reuse its wrapped lines")
	}
}

func TestScrollableTextWrappedCacheSurvivesValueCopies(t *testing.T) {
	var original scrollableText
	original.SetContent("abcdefghijklmnopqrstuvwxyz")

	copyForRender := original
	if got := copyForRender.View(5, 2); got != "abcde\nfghij" {
		t.Fatalf("copied View() = %q, want %q", got, "abcde\\nfghij")
	}
	if len(original.ensureCache().wrapped) != 1 {
		t.Fatal("wrapped cache was not shared with the copied component")
	}
	first := original.ensureCache().wrapped[5]

	if got := original.View(5, 2); got != "abcde\nfghij" {
		t.Fatalf("original View() = %q, want %q", got, "abcde\\nfghij")
	}
	if &original.ensureCache().wrapped[5][0] != &first[0] {
		t.Fatal("original component did not reuse the copied component cache")
	}
}

func TestScrollableTextCachesDistinctWidthsWithoutMixingVisibleData(t *testing.T) {
	var text scrollableText
	text.SetContent("abcdefghijklmnop")

	if got := text.View(4, 2); got != "abcd\nefgh" {
		t.Fatalf("width-4 view = %q, want %q", got, "abcd\\nefgh")
	}
	if got := text.View(8, 2); got != "abcdefgh\nijklmnop" {
		t.Fatalf("width-8 view = %q, want %q", got, "abcdefgh\\nijklmnop")
	}
	if got := text.View(4, 2); got != "abcd\nefgh" {
		t.Fatalf("width-4 cached view = %q, want %q", got, "abcd\\nefgh")
	}
	if len(text.ensureCache().wrapped) != 2 {
		t.Fatalf("cached widths = %d, want 2", len(text.ensureCache().wrapped))
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
	_ = text.View(10, 1)
	_ = text.View(20, 1)
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
	if len(text.ensureCache().wrapped) != 0 {
		t.Fatalf("wrapped widths after source change = %d, want 0", len(text.ensureCache().wrapped))
	}
}

func TestScrollableTextFormattedCacheSurvivesValueCopies(t *testing.T) {
	var original scrollableText
	calls := 0
	original.SetFormattedContent("body-1", func() string {
		calls++
		return "one\ntwo\nthree"
	})

	copyForRender := original
	copyForRender.SetFormattedContent("body-1", func() string {
		calls++
		return "wrong content"
	})
	if calls != 1 {
		t.Fatalf("formatter calls after copied component = %d, want 1", calls)
	}
	if got := copyForRender.View(20, 2); got != "one\ntwo" {
		t.Fatalf("copied formatted View() = %q, want %q", got, "one\\ntwo")
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
