package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const visualOverflowStatus = "Visual Overflow; Please check --debug logs"

type visualOverflowReport struct {
	renderedH int
	budgetH   int
	renderedW int
	budgetW   int
	layout    *normalLayout
	sidebar   string
	request   string
	response  string
	joined    string
	statusBar string
}

// frameOverflows reports whether the rendered frame exceeds the terminal
// on either axis.
func frameOverflows(out string, width, height int) bool {
	if height > 0 && lipgloss.Height(out) > height {
		return true
	}
	if width > 0 && lipgloss.Width(out) > width {
		return true
	}
	return false
}

// HasFrameOverflow reports whether the current View exceeds the model's
// terminal size, or whether the visual-overflow status banner is showing.
// Returns false before a WindowSizeMsg (width/height still 0).
func (m Model) HasFrameOverflow() bool {
	if m.width <= 0 || m.height <= 0 {
		return false
	}
	out := m.View()
	if frameOverflows(out, m.width, m.height) {
		return true
	}
	return strings.Contains(out, visualOverflowStatus)
}

func (m Model) maybeLogVisualOverflow(out string, report *visualOverflowReport) {
	if !frameOverflows(out, m.width, m.height) {
		return
	}
	if m.debugLog == nil {
		return
	}
	renderedH := lipgloss.Height(out)
	renderedW := lipgloss.Width(out)
	if report == nil {
		report = &visualOverflowReport{}
	}
	report.renderedH = renderedH
	report.budgetH = m.height
	report.renderedW = renderedW
	report.budgetW = m.width
	m.logVisualOverflow(report)
}

func (m Model) logVisualOverflow(r *visualOverflowReport) {
	if m.debugLog == nil {
		return
	}

	excessH := r.renderedH - r.budgetH
	excessW := r.renderedW - r.budgetW
	fmt.Fprintf(m.debugLog, "[%s] VISUAL OVERFLOW\n",
		time.Now().Format("2006-01-02 15:04:05.000"))
	fmt.Fprintf(m.debugLog, "  terminal=%dx%d rendered=%dx%d excessH=%d excessW=%d\n",
		r.budgetW, r.budgetH, r.renderedW, r.renderedH, excessH, excessW)
	fmt.Fprintf(m.debugLog, "  mode=%d focus=%d responseTab=%d activeField=%d\n",
		m.mode, m.focus, m.responseTab, m.activeField)

	if r.layout != nil {
		l := r.layout
		fmt.Fprintf(
			m.debugLog,
			"  layout: sidebarW=%d mainW=%d sidebarInnerH=%d rightInnerTotal=%d requestH=%d responseH=%d\n",
			l.sidebarW,
			l.mainW,
			l.sidebarInnerH,
			l.rightInnerTotal,
			l.requestH,
			l.responseH,
		)
	}

	if r.sidebar != "" || r.request != "" || r.response != "" {
		fmt.Fprintf(
			m.debugLog,
			"  component heights: sidebar=%d request=%d response=%d joined=%d statusBar=%d\n",
			lipgloss.Height(r.sidebar),
			lipgloss.Height(r.request),
			lipgloss.Height(r.response),
			lipgloss.Height(r.joined),
			lipgloss.Height(r.statusBar),
		)
		fmt.Fprintf(
			m.debugLog,
			"  component widths: sidebar=%d request=%d response=%d joined=%d statusBar=%d\n",
			lipgloss.Width(r.sidebar),
			lipgloss.Width(r.request),
			lipgloss.Width(r.response),
			lipgloss.Width(r.joined),
			lipgloss.Width(r.statusBar),
		)
	}

	if len(m.executions) > 0 {
		ex := m.selectedExecution()
		bodyLen := 0
		if ex != nil {
			bodyLen = len(ex.ResponseBody)
		}
		fmt.Fprintf(m.debugLog, "  executions=%d execCursor=%d selectedBodyBytes=%d\n",
			len(m.executions), m.execCursor, bodyLen)
	} else if m.response != nil {
		bodyLen := 0
		if m.response.Body != nil {
			bodyLen = len(m.response.Body)
		}
		fmt.Fprintf(m.debugLog, "  liveResponse status=%d bodyBytes=%d\n",
			m.response.StatusCode, bodyLen)
	}

	if r.layout != nil && r.response != "" {
		bodyWidth := max(1, r.layout.mainW-2)
		bodyBudget := max(1, r.layout.responseH-5)
		body := ""
		if ex := m.selectedExecution(); ex != nil {
			switch m.responseTab {
			case bodyTab:
				body = m.viewExecutionBody(ex)
			case headersTab:
				body = m.viewExecutionHeaders(ex)
			case rawTab:
				body = stripANSI(ex.ResponseBody)
			}
		} else if m.response != nil {
			switch m.responseTab {
			case bodyTab:
				body = m.viewResponseBody()
			case headersTab:
				body = m.viewResponseHeaders()
			case rawTab:
				if m.response.Body != nil {
					body = stripANSI(string(m.response.Body))
				}
			}
		}
		if body != "" {
			clipped := limitLines(body, bodyWidth, bodyBudget)
			fmt.Fprintf(m.debugLog,
				"  response content: rawVisualRows=%d clippedVisualRows=%d budget=%d width=%d\n",
				visualRows(body, bodyWidth),
				visualRows(clipped, bodyWidth),
				bodyBudget,
				bodyWidth,
			)
		}
	}
}
