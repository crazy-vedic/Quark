package tui_test

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/tui"
)

// The normal 3-pane layout is sized to occupy exactly the terminal height. A
// large response body — including lines whose width lands right on the pane's
// inner-width boundary — must never push the rendered output past that height,
// otherwise the terminal scrolls and the top row of the app disappears.
//
// Regression test for the report where a 178x47 terminal rendered 48 rows
// because the response body was clipped at width w instead of the pane's inner
// width w-2, so a line 147-148 columns wide counted as one row but wrapped to
// two when rendered.
func TestView_ResponsePane_DoesNotOverflowHeight(t *testing.T) {
	t.Parallel()

	// Body with a mix of line widths, including ones that sit exactly on the
	// w-2 boundary for common terminal widths, plus very long wrapping lines.
	var body strings.Builder
	for i := 0; i < 400; i++ {
		switch i % 4 {
		case 0:
			body.WriteString(strings.Repeat("a", 146)) // 178-wide term inner width
		case 1:
			body.WriteString(strings.Repeat("b", 147))
		case 2:
			body.WriteString(strings.Repeat("c", 148))
		default:
			body.WriteString(strings.Repeat("abcdefghij ", 30))
		}
		body.WriteString("\n")
	}

	sizes := []struct{ w, h int }{
		{178, 47}, // the reported case
		{100, 24},
		{140, 40},
		{80, 16},
		{200, 50},
		{120, 30},
	}

	tabs := []struct {
		name  string
		apply func(tui.Model) tui.Model
	}{
		{"body", func(m tui.Model) tui.Model { return m.WithResponseTab(tui.BodyTab) }},
		{"raw", func(m tui.Model) tui.Model { return m.WithResponseTab(tui.RawTab) }},
		{"headers", func(m tui.Model) tui.Model { return m.WithResponseTab(tui.HeadersTab) }},
	}

	for _, tab := range tabs {
		for _, sz := range sizes {
			m := newModel(defaultConfig())
			m = callUpdate(t, m, tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
			ex := &domain.Execution{
				StatusCode:      405,
				ResponseBody:    body.String(),
				ResponseHeaders: `{"Content-Type":["text/html"]}`,
				ResponseTimeMs:  169,
				CompletedAt:     time.Now(),
			}
			m = tab.apply(m.WithExecutions([]*domain.Execution{ex}).WithExecCursor(0))

			view := m.View()
			gotH := lipgloss.Height(view)
			if gotH > sz.h {
				t.Errorf(
					"tab=%s size=%dx%d: View() rendered %d rows, exceeds terminal height %d",
					tab.name, sz.w, sz.h, gotH, sz.h,
				)
			}
		}
	}
}

// The overflow safety-net must fire when the layout genuinely can't fit (a
// terminal too small to satisfy the panes' minimum sizes): it surfaces a status
// error to the user and writes a detailed block to the --debug log.
func TestView_VisualOverflow_DetectionFiresAndLogs(t *testing.T) {
	t.Parallel()

	debugPath := t.TempDir() + "/debug.log"
	f, err := os.Create(debugPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })

	m := newModel(defaultConfig()).WithDebugLog(f)
	// A 6-row terminal cannot satisfy the pane minimums, so the layout is
	// structurally taller than the screen regardless of content.
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 40, Height: 6})
	ex := &domain.Execution{
		StatusCode:     405,
		ResponseBody:   strings.Repeat("payload line\n", 50),
		ResponseTimeMs: 169,
		CompletedAt:    time.Now(),
	}
	m = m.WithExecutions([]*domain.Execution{ex}).WithExecCursor(0)

	view := m.View()
	if lipgloss.Height(view) <= m.Height() {
		t.Fatalf("expected structural overflow at 40x6, rendered %d rows", lipgloss.Height(view))
	}
	if !strings.Contains(view, "Visual Overflow") {
		t.Fatalf("expected overflow status in view, got:\n%s", view)
	}

	if err := f.Sync(); err != nil {
		t.Fatal(err)
	}
	logBytes, err := os.ReadFile(debugPath)
	if err != nil {
		t.Fatal(err)
	}
	logText := string(logBytes)
	if !strings.Contains(logText, "VISUAL OVERFLOW") {
		t.Fatalf("expected VISUAL OVERFLOW in debug log, got:\n%s", logText)
	}
	if !strings.Contains(logText, "terminal=40x6") {
		t.Fatalf("expected terminal dimensions in debug log, got:\n%s", logText)
	}
}
