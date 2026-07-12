package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const visualOverflowStatus = "Visual Overflow; Please check --debug logs"

type visualOverflowReport struct {
	renderedH int
	budgetH   int
	layout    *normalLayout
	sidebar   string
	request   string
	response  string
	joined    string
	statusBar string
}

func (m Model) maybeLogVisualOverflow(out string, report *visualOverflowReport) {
	if m.height <= 0 {
		return
	}
	renderedH := lipgloss.Height(out)
	if renderedH <= m.height {
		return
	}
	if m.debugLog == nil {
		return
	}
	if report == nil {
		report = &visualOverflowReport{renderedH: renderedH, budgetH: m.height}
	} else {
		report.renderedH = renderedH
		report.budgetH = m.height
	}
	m.logVisualOverflow(report)
}

func (m Model) logVisualOverflow(r *visualOverflowReport) {
	if m.debugLog == nil {
		return
	}

	excess := r.renderedH - r.budgetH
	fmt.Fprintf(m.debugLog, "[%s] VISUAL OVERFLOW\n",
		time.Now().Format("2006-01-02 15:04:05.000"))
	fmt.Fprintf(m.debugLog, "  terminal=%dx%d rendered=%d excess=%d\n",
		m.width, r.budgetH, r.renderedH, excess)
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
