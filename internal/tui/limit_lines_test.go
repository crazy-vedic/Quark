package tui

import (
	"fmt"
	"strings"
	"testing"
)

// limitLines must never emit more visual rows than its budget. Regression test
// for the bug where the appended "… N more lines" notice pushed clipped output
// to maxRows+1 rows, overflowing the exactly-terminal-height layout and
// scrolling the top row of the app off-screen.
func TestLimitLines_NeverExceedsMaxRows(t *testing.T) {
	t.Parallel()

	widths := []int{10, 40, 80}
	maxRowsCases := []int{1, 2, 3, 5, 10}
	lineCounts := []int{0, 1, 5, 50, 500}

	for _, contentWidth := range widths {
		for _, maxRows := range maxRowsCases {
			for _, n := range lineCounts {
				var sb strings.Builder
				for i := 0; i < n; i++ {
					// Mix short lines and lines wide enough to wrap.
					if i%3 == 0 {
						sb.WriteString(strings.Repeat("x", contentWidth*2+3))
					} else {
						fmt.Fprintf(&sb, "line %d", i)
					}
					if i != n-1 {
						sb.WriteString("\n")
					}
				}

				out := limitLines(sb.String(), contentWidth, maxRows)
				got := visualRows(out, contentWidth)
				if got > maxRows {
					t.Errorf(
						"limitLines(width=%d, maxRows=%d, lines=%d) used %d visual rows, want <= %d",
						contentWidth,
						maxRows,
						n,
						got,
						maxRows,
					)
				}
			}
		}
	}
}

// When content is clipped, the truncation notice should still be present.
func TestLimitLines_ShowsTruncationNotice(t *testing.T) {
	t.Parallel()

	var sb strings.Builder
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}

	out := limitLines(sb.String(), 40, 5)
	if !strings.Contains(out, "more lines") {
		t.Fatalf("expected truncation notice, got:\n%s", out)
	}
	if rows := visualRows(out, 40); rows != 5 {
		t.Fatalf("expected notice to consume the full budget (5 rows), got %d rows", rows)
	}
}

// A single logical line wider than the budget must still show a partial prefix,
// not only the truncation notice (common for minified HTML bodies).
func TestLimitLines_ShowsPartialLongLine(t *testing.T) {
	t.Parallel()

	longLine := "<!DOCTYPE html>" + strings.Repeat("x", 5000)
	for i := 0; i < 148; i++ {
		longLine += fmt.Sprintf("\nline %d", i)
	}

	const contentWidth = 80
	const maxRows = 15

	out := limitLines(longLine, contentWidth, maxRows)
	if !strings.Contains(out, "<!DOCTYPE html>") {
		t.Fatalf("expected partial body content before notice, got:\n%s", out)
	}
	if !strings.Contains(out, "more lines") {
		t.Fatalf("expected truncation notice, got:\n%s", out)
	}
	if rows := visualRows(out, contentWidth); rows > maxRows {
		t.Fatalf("expected at most %d visual rows, got %d", maxRows, rows)
	}
	if rows := visualRows(out, contentWidth); rows < 2 {
		t.Fatalf("expected partial content plus notice (>=2 rows), got %d rows", rows)
	}
}

// Content that fits must pass through untouched (no notice, no dropped lines).
func TestLimitLines_FitsWithoutNotice(t *testing.T) {
	t.Parallel()

	in := "alpha\nbeta\ngamma"
	out := limitLines(in, 40, 10)
	if out != in {
		t.Fatalf("expected unchanged content, got %q", out)
	}
	if strings.Contains(out, "more lines") {
		t.Fatalf("did not expect a truncation notice for content that fits")
	}
}
