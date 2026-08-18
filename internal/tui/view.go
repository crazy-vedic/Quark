package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/highlight"
	"github.com/crazy-vedic/quark/internal/keybindings"
	"github.com/crazy-vedic/quark/internal/search"
	"github.com/crazy-vedic/quark/internal/timing"
)

// ansiEscape strips ANSI/VT100 escape sequences from untrusted terminal output.
// Prevents terminal injection (OSC 52 clipboard hijack, screen clear, etc.)
// from hostile HTTP response bodies.
var ansiEscape = regexp.MustCompile(
	`\x1b(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\))`,
)

func stripANSI(s string) string { return ansiEscape.ReplaceAllString(s, "") }

// --- Styles (foreground-only; background inherits terminal → transparency) ---

var (
	// Accent colours.
	blue   = lipgloss.Color("#7aa2f7")
	red    = lipgloss.Color("#f7768e")
	green  = lipgloss.Color("#9ece6a")
	yellow = lipgloss.Color("#e0af68")
	cyan   = lipgloss.Color("#7dcfff")
	muted  = lipgloss.Color("#737aa2") // brighter than before for readability

	// BUG-007: Use high-contrast white for active border so it's unmistakably distinct
	// from the muted inactive border regardless of terminal colour profile.
	activeBorderColor = lipgloss.Color("#ffffff")

	// Active pane border — bright white, clearly distinct from inactive.
	activeBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(activeBorderColor)

	// Inactive pane border — muted blue-grey.
	inactiveBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(muted)

	titleStyle      = lipgloss.NewStyle().Bold(true).Foreground(blue)
	methodStyle     = lipgloss.NewStyle().Bold(true).Foreground(cyan)
	sendButtonStyle = lipgloss.NewStyle().Bold(true).Foreground(green)
	mutedStyle      = lipgloss.NewStyle().Foreground(muted)
	errorStyle      = lipgloss.NewStyle().Foreground(red)
	goodStyle       = lipgloss.NewStyle().Foreground(green)
	warnStyle       = lipgloss.NewStyle().Foreground(yellow)

	// Status bar base style.
	statusStyle = lipgloss.NewStyle().Foreground(muted)
)

var helpActionLabels = map[string]string{
	"quit":                          "quit",
	"help":                          "help",
	keybindings.ActionSearch:        keybindings.ActionSearch,
	keybindings.ActionFocusSidebar:  "focus sidebar",
	keybindings.ActionFocusRequest:  "focus request",
	keybindings.ActionFocusResponse: "focus response",
	"pane_next":                     "next pane",
	"pane_prev":                     "prev pane",
	"sidebar_down":                  helpLabelMoveDown,
	"sidebar_up":                    helpLabelMoveUp,
	"sidebar_expand":                "expand",
	"sidebar_collapse":              "collapse",
	"sidebar_add_request":           "new request",
	"sidebar_add":                   "new collection",
	"sidebar_delete":                "delete collection",
	"sidebar_rename":                "rename collection",
	keybindings.ActionEditURL:       "edit url",
	keybindings.ActionMethodNext:    "next method",
	keybindings.ActionMethodPrev:    "prev method",
	keybindings.ActionSendRequest:   "send request",
	keybindings.ActionEditBody:      "edit body",
	keybindings.ActionEditHeaders:   "edit headers",
	"edit_auth":                     "edit auth",
	keybindings.ActionScheduleRun:   "schedule request",
	"response_down":                 "next history item",
	"response_up":                   "prev history item",
	"response_retry":                "retry request",
	"tab_body":                      "show body",
	"tab_headers":                   "show headers",
	"tab_raw":                       "show raw",
	"tab_next":                      "next view",
	"tab_prev":                      "prev view",
	"search_select":                 "select result",
	"search_down":                   helpLabelMoveDown,
	"search_up":                     helpLabelMoveUp,
	"search_cancel":                 "close search",
	"help_close":                    "close help",
	"help_down":                     helpLabelMoveDown,
	"help_up":                       helpLabelMoveUp,
	"help_edit":                     "edit binding",
	"help_reset":                    "reset binding",
	"help_reset_all":                "reset all bindings",
	"help_unbind":                   "unbind key",
	"import_confirm":                "import request",
	keybindings.ActionImportCancel:  "cancel import",
	"env_save":                      "save env",
	"env_cancel":                    "close env editor",
	"env_create":                    "new environment",
	"env_tab_next":                  "next env tab",
	"env_tab_prev":                  "prev env tab",
	"env_down":                      helpLabelMoveDown,
	"env_up":                        helpLabelMoveUp,
	"env_add":                       "add variable",
	"env_delete":                    "delete variable",
	"env_edit":                      "edit variable",
	"env_edit_confirm":              "confirm edit",
	"env_edit_switch_field":         "switch field",
	"body_save":                     "save body",
	"body_newline":                  "insert newline",
	"body_cancel":                   "cancel body edit",
	"header_down":                   helpLabelMoveDown,
	"header_up":                     helpLabelMoveUp,
	"header_add":                    "add header",
	"header_delete":                 "delete header",
	"header_edit":                   "edit header",
	"header_save":                   "save headers",
	"header_cancel":                 "cancel header edit",
	"header_switch_field":           "switch field",
	"auth_down":                     helpLabelMoveDown,
	"auth_up":                       helpLabelMoveUp,
	"auth_edit":                     "edit auth row",
	"auth_save":                     "save auth",
	"auth_cancel":                   "cancel auth edit",
	"auth_option_next":              "next option",
	"auth_option_prev":              "prev option",
}

const (
	historyPopupVisibleRows = 6
	helpLabelMoveDown       = "move down"
	helpLabelMoveUp         = "move up"
	helpLabelClose          = "close"
	helpLabelCancel         = "cancel"
	helpLabelConfirm        = "confirm"
)

type hintItem struct {
	Label          string
	Actions        []string
	IncludeAliases bool
}

// View implements tea.Model. Renders all panes and overlays.
func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}
	if m.effectiveDim() == DimAbsurd {
		return m.viewTooSmall()
	}

	// Overlay modes take precedence.
	var out string
	switch m.mode {
	case helpMode:
		out = m.viewHelp()
	case envMode:
		out = m.viewEnvModal()
	case importMode:
		out = m.viewImportModal()
	case searchMode:
		out = m.viewSearchModal()
	case collectionPromptMode:
		out = m.viewCollectionPromptModal()
	case scheduleMode:
		out = m.viewScheduleModal()
	case viewerMode:
		out = m.viewViewer()
	default:
		return m.viewByDim()
	}

	m.maybeLogVisualOverflow(out, nil)
	return out
}

// viewByDim renders the normal-mode frame for the active density tier.
func (m Model) viewByDim() string {
	switch m.effectiveDim() {
	case DimNarrow:
		return m.viewStacked()
	case DimTiny:
		return m.viewSinglePane()
	default:
		return m.viewWide()
	}
}

// viewTooSmall renders a centered message when the terminal is absurdly small
// (or --dim=absurd is forced).
func (m Model) viewTooSmall() string {
	msg := fmt.Sprintf(
		"Terminal too small (%dx%d).\nResize to at least %dx%d.",
		m.width,
		m.height,
		MinTerminalWidth,
		MinTerminalHeight,
	)
	if m.forceDim == DimAbsurd {
		msg = fmt.Sprintf(
			"Forced --dim=absurd (%dx%d).\nUnset --dim or pass another tier.",
			m.width,
			m.height,
		)
	}
	boxW := max(1, m.width-2)
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(yellow).
		Padding(0, 1).
		Width(max(1, boxW-2)).
		MaxWidth(boxW).
		Render(warnStyle.Render(msg))
	if m.width > 0 && m.height > 0 {
		placed := lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
		// Hard-clamp in case Place still expands on tiny terminals.
		return clipToRows(placed, m.width, m.height)
	}
	return box
}

// Ensure tea.Model is satisfied at compile time.
var _ tea.Model = Model{}

// --- Density layouts ---

func (m Model) viewWide() string {
	paneLayout := m.currentLayout()

	sidebar := m.viewSidebar(paneLayout.sidebarW, paneLayout.sidebarInnerH)
	request := m.viewRequestPane(paneLayout.mainW, paneLayout.requestH)
	response := m.viewResponsePane(paneLayout.mainW, paneLayout.responseH)
	right := lipgloss.JoinVertical(lipgloss.Left, request, response)

	joined := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, right)
	statusBar := m.viewStatusBar("")

	out := lipgloss.JoinVertical(lipgloss.Left, joined, statusBar)
	if frameOverflows(out, m.width, m.height) {
		report := &visualOverflowReport{
			layout:    &paneLayout,
			sidebar:   sidebar,
			request:   request,
			response:  response,
			joined:    joined,
			statusBar: statusBar,
		}
		m.maybeLogVisualOverflow(out, report)
		statusBar = m.viewStatusBar(visualOverflowStatus)
		out = lipgloss.JoinVertical(lipgloss.Left, joined, statusBar)
	}
	return out
}

func (m Model) viewStacked() string {
	paneLayout := m.currentLayout()

	sidebar := m.viewSidebar(paneLayout.sidebarW, paneLayout.sidebarInnerH)
	request := m.viewRequestPane(paneLayout.mainW, paneLayout.requestH)
	response := m.viewResponsePane(paneLayout.mainW, paneLayout.responseH)
	statusBar := m.viewStatusBar("")

	joined := lipgloss.JoinVertical(lipgloss.Left, sidebar, request, response)
	out := lipgloss.JoinVertical(lipgloss.Left, joined, statusBar)
	if frameOverflows(out, m.width, m.height) {
		report := &visualOverflowReport{
			layout:    &paneLayout,
			sidebar:   sidebar,
			request:   request,
			response:  response,
			joined:    joined,
			statusBar: statusBar,
		}
		m.maybeLogVisualOverflow(out, report)
		statusBar = m.viewStatusBar(visualOverflowStatus)
		out = lipgloss.JoinVertical(lipgloss.Left, joined, statusBar)
	}
	return out
}

func (m Model) viewSinglePane() string {
	paneLayout := m.currentLayout()
	var pane string
	switch m.focus {
	case requestPane:
		pane = m.viewRequestPane(paneLayout.mainW, paneLayout.requestH)
	case responsePane:
		pane = m.viewResponsePane(paneLayout.mainW, paneLayout.responseH)
	default:
		pane = m.viewSidebar(paneLayout.sidebarW, paneLayout.sidebarInnerH)
	}
	statusBar := m.viewStatusBar("")
	out := lipgloss.JoinVertical(lipgloss.Left, pane, statusBar)
	if frameOverflows(out, m.width, m.height) {
		report := &visualOverflowReport{
			layout:    &paneLayout,
			statusBar: statusBar,
		}
		m.maybeLogVisualOverflow(out, report)
		statusBar = m.viewStatusBar(visualOverflowStatus)
		out = lipgloss.JoinVertical(lipgloss.Left, pane, statusBar)
	}
	return out
}

// --- Sidebar ---

func (m Model) viewSidebar(w, h int) string {
	border := inactiveBorder
	if m.focus == sidebarPane {
		border = activeBorder
	}

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Collections") + "\n")

	rows, start, end := m.sidebarListWindow()
	if start > 0 {
		sb.WriteString(mutedStyle.Render("  ↑ more above") + "\n")
	}
	for i := start; i < end; i++ {
		row := rows[i]
		switch row.kind {
		case sidebarCollectionRow:
			col := m.collections[row.colIndex]
			cursor := "  "
			if row.colIndex == m.colCursor && m.reqCursor == -1 && m.focus == sidebarPane {
				cursor = "▸ "
			}
			expanded := m.expanded[col.ID]
			icon := "▶ "
			if expanded {
				icon = "▼ "
			}
			innerW := max(1, w-2)
			name := truncate(col.Name, innerW-4)
			line := cursor + icon + name
			if row.colIndex == m.colCursor && m.reqCursor == -1 {
				line = lipgloss.NewStyle().Foreground(blue).Bold(true).Render(line)
			} else {
				line = lipgloss.NewStyle().Foreground(lipgloss.Color("#a9b1d6")).Render(line)
			}
			sb.WriteString(line + "\n")
		case sidebarRequestRow:
			col := m.collections[row.colIndex]
			req := m.collectionRequests[col.ID][row.reqIndex]
			isSelected := row.colIndex == m.colCursor && row.reqIndex == m.reqCursor
			cursor := "    "
			if isSelected {
				cursor = "  ▸ "
			}
			innerW := max(1, w-2)
			badge := methodBadge(req.Method)
			nameWidth := innerW - lipgloss.Width(cursor) - lipgloss.Width(badge) - 1
			line := cursor + badge + " " + truncate(req.Name, nameWidth)
			if isSelected {
				line = lipgloss.NewStyle().Foreground(cyan).Render(line)
			} else {
				line = mutedStyle.Render(line)
			}
			sb.WriteString(line + "\n")
		}
	}
	if end < len(rows) {
		sb.WriteString(mutedStyle.Render("  ↓ more below") + "\n")
	}

	if len(m.collections) == 0 {
		addCollection := m.renderHintKeys([]string{"sidebar_add"}, false)
		sb.WriteString(
			"\n" + mutedStyle.Render("  No collections.\n  Press "+addCollection+" to add."),
		)
	}

	inner := sb.String()
	return border.Width(w).Height(h).MaxHeight(h + 2).Render(inner)
}

// --- Request pane ---

const requestSendButtonLabel = "[ Send ]"

func renderMethodBadge(method string) string {
	if method == "" {
		method = "GET"
	}
	return methodStyle.Render(fmt.Sprintf(" %s ", method))
}

func renderSendButton() string {
	return sendButtonStyle.Render(requestSendButtonLabel)
}

func methodBadgeWidth(method string) int {
	return lipgloss.Width(renderMethodBadge(method))
}

func requestSendButtonWidth() int {
	return lipgloss.Width(renderSendButton())
}

func (m Model) renderRequestTitleRightChrome() string {
	sendBtn := renderSendButton()
	envName := m.activeEnvName()
	if envName == "" {
		return sendBtn
	}
	envText := fmt.Sprintf("◀ %s ▶", envName)
	envStyled := lipgloss.NewStyle().Foreground(yellow).Render(envText)
	return sendBtn + " " + envStyled
}

func (m Model) requestTitleRightChromeWidth() int {
	return lipgloss.Width(m.renderRequestTitleRightChrome())
}

func (m Model) viewRequestPane(w, h int) string {
	timingSpan := m.timing.Track("tui.view_request_pane")
	defer timingSpan.Done()
	started := time.Now()
	defer func() {
		logDebugTiming(m.debugLog, "view_request_pane", started,
			fmt.Sprintf("width=%d height=%d field=%d", w, h, m.activeField))
	}()
	border := inactiveBorder
	if m.focus == requestPane {
		border = activeBorder
	}

	var top []string

	// Title line with the request label first, then send/env chrome aligned right.
	titleLabel := "Request"
	if m.activeRequest != nil && strings.TrimSpace(m.activeRequest.Name) != "" {
		titleLabel = "Request " + truncate(m.activeRequest.Name, max(12, w/2))
	}
	titleLine := titleStyle.Render(titleLabel)
	rightChrome := m.renderRequestTitleRightChrome()
	padding := w - 2 - lipgloss.Width(titleLine) - lipgloss.Width(rightChrome)
	if padding < 0 {
		padding = 0
	}
	top = append(top, titleLine+strings.Repeat(" ", padding)+rightChrome)

	// Method badge + URL on one line — truncate URL to available width.
	badge := renderMethodBadge(m.method)
	badgeW := lipgloss.Width(badge)
	urlAvail := w - badgeW - 4 // padding around badge/url
	if urlAvail < 1 {
		urlAvail = 1
	}
	var urlDisplay string
	if m.activeField == urlField {
		urlDisplay = m.urlInput.View()
	} else {
		urlVal := m.urlInput.Value()
		if urlVal == "" {
			urlDisplay = mutedStyle.Render(
				"press " + m.renderHintKeys(
					[]string{keybindings.ActionEditURL},
					false,
				) + " to enter a URL",
			)
		} else {
			// Truncate long URLs so they don't wrap to next line.
			urlDisplay = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#c0caf5")).
				Render(truncate(urlVal, urlAvail))
		}
	}
	top = append(top, badge+"  "+urlDisplay)

	if m.activeRequest != nil {
		authLabel := mutedStyle.Render("  Auth: ")
		authSummary := truncate(
			newAuthEditor(m.activeRequest).summary(),
			max(1, w-2-lipgloss.Width("  Auth: ")),
		)
		top = append(
			top,
			authLabel+lipgloss.NewStyle().
				Foreground(lipgloss.Color("#c0caf5")).
				Render(authSummary),
		)
		if m.activeField == urlField {
			if warning := singleLineInputWarning(m.urlInput.Value()); warning != "" {
				top = append(top, warning)
			}
		}
	}

	top = append(top, "")

	// Validation / status errors — strip Go error chain prefix for readability.
	if m.activeValidationErr() != "" {
		msg := cleanError(m.activeValidationErr())
		top = append(top, errorStyle.Render("✗ "+truncate(msg, w-4)))
	}

	// Loading indicator.
	if m.loading {
		top = append(top, warnStyle.Render("  ⟳ Sending…  [Esc] cancel"))
	}

	var content string
	// Inline body / headers field.
	switch {
	case m.activeField == bodyField && m.activeRequest != nil:
		content = m.bodyTextarea.View()
	case m.activeField == authField && m.activeRequest != nil:
		content = m.viewAuthEditor()
	case m.activeField == headersField && m.activeRequest != nil:
		var preview strings.Builder
		if m.headerEditing {
			preview.WriteString("Key:\n")
			preview.WriteString(m.headerKeyInput.View() + "\n\n")
			preview.WriteString("Value:\n")
			preview.WriteString(m.headerValueInput.View() + "\n")
			if m.headerKeyInput.Focused() {
				if warning := singleLineInputWarning(m.headerKeyInput.Value()); warning != "" {
					preview.WriteString(warning + "\n")
				}
			} else if warning := singleLineInputWarning(m.headerValueInput.Value()); warning != "" {
				preview.WriteString(warning + "\n")
			}
		} else {
			if len(m.headerPairs) == 0 {
				preview.WriteString(
					mutedStyle.Render(
						"  No headers. Press " + m.renderHintKeys(
							[]string{"header_add"},
							false,
						) + " to add.\n",
					),
				)
			} else {
				for i, p := range m.headerPairs {
					cursor := "  "
					if i == m.headerCursor {
						cursor = "▸ "
					}
					plain := truncate(cursor+p.Key+": "+p.Value, w-4)
					var line string
					if i == m.headerCursor {
						line = lipgloss.NewStyle().Bold(true).Render(plain)
					} else {
						line = lipgloss.NewStyle().Foreground(cyan).Render(plain)
					}
					preview.WriteString(line + "\n")
				}
			}
		}
		content = preview.String()
	case m.activeRequest != nil:
		// The read-only preview is formatted lazily by requestText below.
		content = ""
	}

	// Responsive key hints — shorten at narrow terminals, then hard-clamp to one
	// row so long bindings never wrap onto a second line inside the pane.
	innerWidth := max(1, w-2)
	var hintsPlain string
	switch {
	case w < 55:
		hintsPlain = m.renderHints([]hintItem{
			{Label: "url", Actions: []string{keybindings.ActionEditURL}},
			{
				Label:   "cycle method",
				Actions: []string{keybindings.ActionMethodNext, keybindings.ActionMethodPrev},
			},
			{Label: "send", Actions: []string{keybindings.ActionSendRequest}, IncludeAliases: true},
		})
	case w < 90:
		hintsPlain = m.renderHints([]hintItem{
			{Label: "url", Actions: []string{keybindings.ActionEditURL}},
			{
				Label:   "cycle method",
				Actions: []string{keybindings.ActionMethodNext, keybindings.ActionMethodPrev},
			},
			{Label: "send", Actions: []string{keybindings.ActionSendRequest}, IncludeAliases: true},
			{Label: "body", Actions: []string{keybindings.ActionEditBody}},
			{Label: "headers", Actions: []string{keybindings.ActionEditHeaders}},
			{Label: "env", Actions: []string{keybindings.ActionEnvOpen}},
		})
	case w < 110:
		hintsPlain = m.renderHints([]hintItem{
			{Label: "url", Actions: []string{keybindings.ActionEditURL}},
			{
				Label:   "cycle method",
				Actions: []string{keybindings.ActionMethodNext, keybindings.ActionMethodPrev},
			},
			{Label: "send", Actions: []string{keybindings.ActionSendRequest}, IncludeAliases: true},
			{Label: "body", Actions: []string{keybindings.ActionEditBody}},
			{Label: "headers", Actions: []string{keybindings.ActionEditHeaders}},
			{Label: "env", Actions: []string{keybindings.ActionEnvOpen}},
			{Label: "cycle env", Actions: []string{"env_prev", "env_next"}},
		})
	default:
		hintsPlain = m.renderHints([]hintItem{
			{Label: "url", Actions: []string{keybindings.ActionEditURL}},
			{
				Label:   "cycle method",
				Actions: []string{keybindings.ActionMethodNext, keybindings.ActionMethodPrev},
			},
			{Label: "send", Actions: []string{keybindings.ActionSendRequest}, IncludeAliases: true},
			{Label: "body", Actions: []string{keybindings.ActionEditBody}},
			{Label: "headers", Actions: []string{keybindings.ActionEditHeaders}},
			{Label: "auth", Actions: []string{"edit_auth"}},
			{Label: "env", Actions: []string{keybindings.ActionEnvOpen}},
			{Label: "cycle env", Actions: []string{"env_prev", "env_next"}},
		})
	}
	hints := mutedStyle.Render(truncate(hintsPlain, innerWidth))

	// Measure chrome (title/url/auth + separator/hints) in visual rows so wrapped
	// lines cannot steal body budget unnoticed — same approach as the response pane.
	chromeTop := strings.Join(top, "\n")
	sepAndHints := mutedStyle.Render(strings.Repeat("─", innerWidth)) + "\n" + hints
	chromeRows := visualRows(chromeTop, innerWidth) + visualRows(sepAndHints, innerWidth)
	contentLines := h - chromeRows
	if contentLines < 0 {
		contentLines = 0
	}
	var renderedContent string
	if m.activeField == noneField && m.activeRequest != nil {
		m.requestText.SetDebugLog(m.debugLog, "request")
		m.requestText.SetTiming(m.timing)
		m.requestText.SetFormattedContent(m.requestBodyPreviewSourceKey(), func() string {
			return m.requestBodyPreviewContent(timingSpan)
		}, timingSpan)
		renderedContent = m.requestText.View(innerWidth, contentLines, timingSpan)
	} else {
		renderedContent = limitLines(content, innerWidth, contentLines)
	}
	paddingLines := contentLines - visualRows(renderedContent, innerWidth)
	if paddingLines < 0 {
		paddingLines = 0
	}

	var sb strings.Builder
	sb.WriteString(chromeTop)
	sb.WriteString("\n")
	if renderedContent != "" {
		sb.WriteString(renderedContent)
		if !strings.HasSuffix(renderedContent, "\n") {
			sb.WriteString("\n")
		}
	}
	if paddingLines > 0 {
		sb.WriteString(strings.Repeat("\n", paddingLines))
	}
	sb.WriteString(sepAndHints)

	return border.Width(w).Height(h).MaxHeight(h + 2).Render(clipToRows(sb.String(), innerWidth, h))
}

func (m Model) requestBodyPreviewContent(parent ...*timing.Span) string {
	timingSpan := m.timing.Track("tui.request_body_preview_content", timingParent(parent))
	defer timingSpan.Done()
	started := time.Now()
	defer func() {
		logDebugTiming(m.debugLog, "request_body_preview_content", started,
			fmt.Sprintf("has_request=%t", m.activeRequest != nil))
	}()
	if m.activeRequest == nil {
		return ""
	}
	if m.activeRequest.Body != "" {
		lines := strings.Split(m.activeRequest.Body, "\n")
		for i := range lines {
			lines[i] = "  " + lines[i]
		}
		return strings.Join(lines, "\n")
	}
	if m.activeRequest.Headers == "" || m.activeRequest.Headers == "{}" {
		return ""
	}
	var hdrs map[string]string
	if err := json.Unmarshal([]byte(m.activeRequest.Headers), &hdrs); err != nil {
		return ""
	}
	keys := sortedStringMapKeys(hdrs)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, "  "+k+": "+hdrs[k])
	}
	return strings.Join(lines, "\n")
}

func (m Model) viewAuthEditor() string {
	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Auth") + "\n")
	sb.WriteString(mutedStyle.Render("  Secrets stay hidden in the preview.") + "\n\n")
	for idx, row := range m.authEditor.rows() {
		cursor := "  "
		if idx == m.authEditor.cursor {
			cursor = "▸ "
		}
		label := lipgloss.NewStyle().Foreground(cyan).Render(authRowLabel(row))
		value := m.authEditor.valueForRow(row, m.authEditor.editing)
		line := cursor + label + ": " + value
		if idx == m.authEditor.cursor {
			line = lipgloss.NewStyle().Bold(true).Render(line)
		}
		sb.WriteString(line + "\n")
		if idx == m.authEditor.cursor && m.authEditor.editing {
			if warning := singleLineInputWarning(m.authEditor.singleLineValue(row)); warning != "" {
				sb.WriteString(warning + "\n")
			}
		}
	}
	sb.WriteString("\n")
	sb.WriteString(mutedStyle.Render(m.renderHints([]hintItem{
		{Label: "move", Actions: []string{"auth_up", "auth_down"}},
		{Label: "edit", Actions: []string{"auth_edit"}},
		{Label: "cycle", Actions: []string{"auth_option_prev", "auth_option_next"}},
		{Label: "save", Actions: []string{"auth_save"}},
	})))
	return sb.String()
}

// --- Response pane ---

func (m Model) viewResponsePane(w, h int) string {
	timingSpan := m.timing.Track("tui.view_response_pane")
	defer timingSpan.Done()
	started := time.Now()
	defer func() {
		logDebugTiming(m.debugLog, "view_response_pane", started,
			fmt.Sprintf("width=%d height=%d tab=%d cursor=%d", w, h, m.responseTab, m.execCursor))
	}()
	border := inactiveBorder
	if m.focus == responsePane {
		border = activeBorder
	}

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Response") + "\n")

	currentExec := m.selectedExecution()
	if currentExec == nil && m.response == nil {
		sendHint := m.renderHintKeys([]string{keybindings.ActionSendRequest}, true)
		sb.WriteString("\n" + mutedStyle.Render("  No response yet — press "+sendHint+" to send."))
		return border.Width(w).Height(h).MaxHeight(h + 2).Render(sb.String())
	}

	if currentExec != nil {
		sb.WriteString(
			mutedStyle.Render(
				fmt.Sprintf(
					"  Run %d/%d  %s",
					m.execCursor+1,
					len(m.executions),
					currentExec.CompletedAt.In(time.Local).Format("2006-01-02 15:04:05"),
				),
			) + "\n",
		)
		sb.WriteString(m.viewExecutionStatus(currentExec) + "\n")
	} else {
		sb.WriteString(m.viewLiveResponseStatus() + "\n")
	}

	// Body gets whatever inner height remains after chrome. Measure the actual
	// chrome rows at the pane's inner width (w-2, since the border consumes a
	// column on each side) so that a wrapped tab bar — or a body line near the
	// width boundary — can't push the pane one row past its budget and scroll
	// the whole app off the top. The trailing "\n" delimits the body start, so
	// it must not be counted as a chrome row.
	innerWidth := max(1, w-2)

	// Tab bar — clamp helper hints so the bar stays a single row.
	tabs := m.viewTabBar(innerWidth)
	sb.WriteString(tabs + "\n\n")
	chromeRows := visualRows(strings.TrimSuffix(sb.String(), "\n"), innerWidth)
	// Body may shrink to zero: on very small panes the chrome (status + a
	// wrapped tab bar) can consume the entire budget, and forcing a minimum of
	// one body row would push the pane past its height and scroll the app.
	bodyLines := h - chromeRows
	if bodyLines < 0 {
		bodyLines = 0
	}

	// Tab content is formatted lazily by the scrollable component.
	var formatBody func() string
	if currentExec != nil {
		switch m.responseTab {
		case bodyTab:
			formatBody = func() string { return m.viewExecutionBody(currentExec, timingSpan) }
		case headersTab:
			formatBody = func() string { return m.viewExecutionHeaders(currentExec) }
		case rawTab:
			formatBody = func() string {
				if currentExec.ResponseBody == "" {
					return mutedStyle.Render("  (empty body)")
				}
				return stripANSI(currentExec.ResponseBody)
			}
		}
	} else if r := m.response; r != nil {
		switch m.responseTab {
		case bodyTab:
			formatBody = func() string { return m.viewResponseBody(timingSpan) }
		case headersTab:
			formatBody = func() string { return m.viewResponseHeaders() }
		case rawTab:
			formatBody = func() string {
				if r.Body != nil {
					return stripANSI(string(r.Body))
				}
				if r.TempPath != "" {
					return mutedStyle.Render(fmt.Sprintf("[streamed → %s]", r.TempPath))
				}
				return ""
			}
		}
	}
	// Body/Raw content is owned by a scrollable text component. Keep the
	// response history popup as a separate column, but let wheel and arrows
	// move through the body instead of changing history.
	m.responseText.SetDebugLog(m.debugLog, "response")
	m.responseText.SetTiming(m.timing)
	m.responseText.SetFormattedContent(m.responseTextSourceKey(), formatBody, timingSpan)

	popup := m.viewExecutionHistoryPopup(w, bodyLines)
	if popup != "" && w >= 64 {
		popupWidth := lipgloss.Width(popup)
		bodyWidth := w - popupWidth - 4
		if bodyWidth >= 18 {
			left := lipgloss.NewStyle().Width(bodyWidth).MaxHeight(bodyLines).
				Render(m.responseText.View(bodyWidth, bodyLines, timingSpan))
			sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", popup))
			return border.Width(w).
				Height(h).
				MaxHeight(h + 2).
				Render(clipToRows(sb.String(), innerWidth, h))
		}
	}

	if popup != "" {
		popupRows := lipgloss.Height(popup)
		if popupRows < bodyLines {
			sb.WriteString(popup + "\n")
			bodyLines -= popupRows + 1
			if bodyLines < 1 {
				bodyLines = 1
			}
		}
	}

	sb.WriteString(m.responseText.View(innerWidth, bodyLines, timingSpan))

	return border.Width(w).Height(h).MaxHeight(h + 2).Render(clipToRows(sb.String(), innerWidth, h))
}

func (m Model) requestBodyPreviewSourceKey() string {
	if m.activeRequest == nil {
		return "request:none"
	}
	return fmt.Sprintf("request:%p:%s", m.activeRequest, m.activeRequest.ID)
}

func (m Model) responseTextSourceKey() string {
	if currentExec := m.selectedExecution(); currentExec != nil {
		return fmt.Sprintf("execution:%p:%s:%d", currentExec, currentExec.ID, m.responseTab)
	}
	if m.response != nil {
		return fmt.Sprintf("response:%p:%d", m.response, m.responseTab)
	}
	return fmt.Sprintf("response:none:%d", m.responseTab)
}

// formatResponseTextContent produces the formatted response content. The
// scrollable component owns caching; callers should use setResponseTextContent.
func (m Model) formatResponseTextContent(parent ...*timing.Span) string {
	timingSpan := m.timing.Track("tui.response_text_content", timingParent(parent))
	defer timingSpan.Done()
	started := time.Now()
	defer func() {
		logDebugTiming(m.debugLog, "response_text_content", started,
			fmt.Sprintf("tab=%d cursor=%d", m.responseTab, m.execCursor))
	}()
	currentExec := m.selectedExecution()
	if currentExec != nil {
		switch m.responseTab {
		case bodyTab:
			return m.viewExecutionBody(currentExec, timingSpan)
		case headersTab:
			return m.viewExecutionHeaders(currentExec)
		case rawTab:
			if currentExec.ResponseBody == "" {
				return mutedStyle.Render("  (empty body)")
			}
			return stripANSI(currentExec.ResponseBody)
		}
	} else if r := m.response; r != nil {
		switch m.responseTab {
		case bodyTab:
			return m.viewResponseBody(timingSpan)
		case headersTab:
			return m.viewResponseHeaders()
		case rawTab:
			if r.Body != nil {
				return stripANSI(string(r.Body))
			}
			if r.TempPath != "" {
				return mutedStyle.Render(fmt.Sprintf("[streamed â†’ %s]", r.TempPath))
			}
		}
	}
	return ""
}

func (m *Model) setRequestTextContent(parent ...*timing.Span) {
	m.requestText.SetDebugLog(m.debugLog, "request")
	m.requestText.SetTiming(m.timing)
	m.requestText.SetFormattedContent(m.requestBodyPreviewSourceKey(), func() string {
		return m.requestBodyPreviewContent(timingParent(parent))
	}, timingParent(parent))
}

func (m *Model) setResponseTextContent(parent ...*timing.Span) {
	m.responseText.SetDebugLog(m.debugLog, "response")
	m.responseText.SetTiming(m.timing)
	m.responseText.SetFormattedContent(m.responseTextSourceKey(), func() string {
		return m.formatResponseTextContent(timingParent(parent))
	}, timingParent(parent))
}

func (m Model) viewTabBar(maxWidth int) string {
	tabs := []struct {
		label string
		id    responseTabID
		key   string
	}{
		{"Body", bodyTab, "b"},
		{"Headers", headersTab, "h"},
		{"Raw", rawTab, "r"},
	}
	var parts []string
	for _, t := range tabs {
		action := map[responseTabID]string{
			bodyTab:    "tab_body",
			headersTab: "tab_headers",
			rawTab:     "tab_raw",
		}[t.id]
		label := fmt.Sprintf(
			"[%s] %s",
			keybindings.FormatKey(keybindings.GetAction(m.cfg.Keybindings, action)),
			t.label,
		)
		if m.responseTab == t.id {
			parts = append(
				parts,
				lipgloss.NewStyle().Foreground(blue).Underline(true).Bold(true).Render(label),
			)
		} else {
			parts = append(parts, mutedStyle.Render(label))
		}
	}
	tabPart := "  " + strings.Join(parts, "  ")

	hintItems := []hintItem{
		{Label: "view", Actions: []string{"tab_prev", "tab_next"}},
		{Label: "retry", Actions: []string{"response_retry"}},
	}
	if len(m.executions) > 1 {
		hintItems = append(hintItems, hintItem{
			Label:   "history",
			Actions: []string{"response_up", "response_down"},
		})
	}
	hintsPlain := m.renderHints(hintItems)
	if hintsPlain == "" {
		if maxWidth > 0 && lipgloss.Width(tabPart) > maxWidth {
			return mutedStyle.Render(truncate(stripANSI(tabPart), maxWidth))
		}
		return tabPart
	}

	const gap = "    "
	if maxWidth <= 0 {
		return tabPart + gap + mutedStyle.Render(hintsPlain)
	}
	avail := maxWidth - lipgloss.Width(tabPart) - lipgloss.Width(gap)
	if avail < 1 {
		if lipgloss.Width(tabPart) > maxWidth {
			return mutedStyle.Render(truncate(stripANSI(tabPart), maxWidth))
		}
		return tabPart
	}
	return tabPart + gap + mutedStyle.Render(truncate(hintsPlain, avail))
}

func (m Model) selectedExecution() *domain.Execution {
	if len(m.executions) == 0 || m.execCursor < 0 || m.execCursor >= len(m.executions) {
		return nil
	}
	return m.executions[m.execCursor]
}

func (m Model) viewingHistoricalExecution() bool {
	return len(m.executions) > 1 && m.execCursor > 0
}

func (m Model) viewLiveResponseStatus() string {
	r := m.response
	statusColor := goodStyle
	if r.StatusCode >= 400 {
		statusColor = errorStyle
	} else if r.StatusCode >= 300 {
		statusColor = warnStyle
	}
	return statusColor.Render(fmt.Sprintf("  %s", r.Status)) +
		"  " + mutedStyle.Render(fmt.Sprintf("%v  %d bytes", r.Duration.Round(1_000_000), r.Size))
}

func (m Model) viewExecutionStatus(ex *domain.Execution) string {
	statusLabel := fmt.Sprintf("%d", ex.StatusCode)
	statusStyle := goodStyle
	switch {
	case ex.Error != "" && ex.StatusCode == 0:
		statusLabel = "ERROR"
		statusStyle = errorStyle
	case ex.StatusCode >= 400:
		statusStyle = errorStyle
	case ex.StatusCode >= 300:
		statusStyle = warnStyle
	}
	return statusStyle.Render("  "+statusLabel) +
		"  " + mutedStyle.Render(
		fmt.Sprintf("%dms  %d bytes", ex.ResponseTimeMs, len(ex.ResponseBody)),
	)
}

// isBinaryBody reports whether body bytes look like binary/non-textual content.
// Uses utf8.Valid as a fast heuristic; also catches null bytes.
func isBinaryBody(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	// Null bytes are a strong indicator of binary content.
	for _, c := range b {
		if c == 0x00 {
			return true
		}
	}
	return !utf8.Valid(b)
}

func (m Model) viewResponseBody(parent ...*timing.Span) string {
	timingSpan := m.timing.Track("tui.view_response_body", timingParent(parent))
	defer timingSpan.Done()
	started := time.Now()
	defer func() {
		bodyBytes := 0
		if m.response != nil {
			bodyBytes = len(m.response.Body)
		}
		logDebugTiming(m.debugLog, "view_response_body", started,
			fmt.Sprintf("bytes=%d", bodyBytes))
	}()
	r := m.response
	if r.Body == nil {
		if r.TempPath != "" {
			return mutedStyle.Render(fmt.Sprintf("  [large response → %s]", r.TempPath))
		}
		return mutedStyle.Render("  (empty body)")
	}

	// BUG-002: binary content must not be rendered raw — it can corrupt the terminal.
	if isBinaryBody(r.Body) {
		ct := http.Header(r.Headers).Get("Content-Type")
		if ct == "" {
			ct = "application/octet-stream"
		}
		return warnStyle.Render(fmt.Sprintf(
			"  [binary content, %d bytes, content-type: %s]\n  Use Raw tab to view raw bytes path.",
			len(r.Body), ct,
		))
	}

	// Pretty-print JSON if content-type matches.
	ct := http.Header(r.Headers).Get("Content-Type")
	if strings.Contains(ct, "application/json") || json.Valid(r.Body) {
		var out any
		if err := json.Unmarshal(r.Body, &out); err == nil {
			pretty, err := json.MarshalIndent(out, "", "  ")
			if err == nil {
				return highlight.JSON(string(pretty), m.cfg.UI.Theme)
			}
		}
	}
	return stripANSI(string(r.Body))
}

func (m Model) viewResponseHeaders() string {
	if m.response == nil {
		return ""
	}
	return renderHTTPHeaders(m.response.Headers)
}

func (m Model) viewExecutionBody(ex *domain.Execution, parent ...*timing.Span) string {
	timingSpan := m.timing.Track("tui.view_execution_body", timingParent(parent))
	defer timingSpan.Done()
	started := time.Now()
	defer func() {
		bodyBytes := 0
		if ex != nil {
			bodyBytes = len(ex.ResponseBody)
		}
		logDebugTiming(m.debugLog, "view_execution_body", started,
			fmt.Sprintf("bytes=%d", bodyBytes))
	}()
	if ex.Error != "" {
		if ex.ResponseBody == "" {
			return errorStyle.Render("  " + ex.Error)
		}
		return errorStyle.Render("  "+ex.Error+"\n\n") + stripANSI(ex.ResponseBody)
	}
	if ex.ResponseBody == "" {
		return mutedStyle.Render("  (empty body)")
	}
	bodyBytes := []byte(ex.ResponseBody)
	if isBinaryBody(bodyBytes) {
		return warnStyle.Render(fmt.Sprintf(
			"  [binary content, %d bytes]\n  Use Raw tab to inspect the stored payload.",
			len(bodyBytes),
		))
	}
	if headers := m.executionHeaders(
		ex,
	); strings.Contains(
		headers.Get("Content-Type"),
		"application/json",
	) ||
		json.Valid(bodyBytes) {
		var out any
		if err := json.Unmarshal(bodyBytes, &out); err == nil {
			pretty, err := json.MarshalIndent(out, "", "  ")
			if err == nil {
				return highlight.JSON(string(pretty), m.cfg.UI.Theme)
			}
		}
	}
	return stripANSI(ex.ResponseBody)
}

func (m Model) viewExecutionHeaders(ex *domain.Execution) string {
	headers := m.executionHeaders(ex)
	if len(headers) == 0 {
		if ex.Error != "" {
			return mutedStyle.Render("  No response headers captured for this failed execution.")
		}
		return mutedStyle.Render("  (no headers)")
	}
	return renderHTTPHeaders(headers)
}

func renderHTTPHeaders(headers http.Header) string {
	var sb strings.Builder
	for _, k := range sortedHeaderKeys(headers) {
		vals := headers[k]
		for _, v := range vals {
			sb.WriteString("  " + lipgloss.NewStyle().Foreground(cyan).Render(k))
			sb.WriteString(
				": " + lipgloss.NewStyle().Foreground(lipgloss.Color("#c0caf5")).Render(v) + "\n",
			)
		}
	}
	return sb.String()
}

func (m Model) executionHeaders(ex *domain.Execution) http.Header {
	if ex == nil || ex.ResponseHeaders == "" {
		return nil
	}
	var headers map[string][]string
	if err := json.Unmarshal([]byte(ex.ResponseHeaders), &headers); err != nil {
		return nil
	}
	return http.Header(headers)
}

func (m Model) viewExecutionHistoryPopup(maxWidth, maxRows int) string {
	if !m.viewingHistoricalExecution() {
		return ""
	}

	if maxRows < 6 {
		return ""
	}

	maxVisible := min(historyPopupVisibleRows, maxRows-3)
	if maxVisible < 3 {
		return ""
	}

	indices, hasAbove, hasBelow := m.visibleExecutionHistoryWindow(maxVisible)
	if len(indices) == 0 {
		return ""
	}

	popupWidth := min(maxWidth-8, 34)
	if popupWidth < 22 {
		popupWidth = 22
	}
	innerWidth := max(1, popupWidth-4)

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Execution History") + "\n")
	if hasAbove {
		sb.WriteString(mutedStyle.Render("  ↑ more above") + "\n")
	} else {
		sb.WriteString("\n")
	}
	for _, idx := range indices {
		ex := m.executions[idx]
		sb.WriteString(m.viewExecutionHistoryLine(idx, ex, innerWidth) + "\n")
	}
	if hasBelow {
		sb.WriteString(mutedStyle.Render("  ↓ more below"))
	} else {
		sb.WriteString("")
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(muted).
		Padding(0, 1).
		Width(popupWidth).
		Render(sb.String())
}

func (m Model) visibleExecutionHistoryWindow(maxVisible int) ([]int, bool, bool) {
	if maxVisible <= 0 || len(m.executions) == 0 {
		return nil, false, false
	}
	if len(m.executions) <= maxVisible {
		indices := make([]int, len(m.executions))
		for i := range m.executions {
			indices[i] = i
		}
		return indices, false, false
	}

	indices := []int{0}
	window := maxVisible - 1
	if window <= 0 {
		return indices, false, len(m.executions) > 1
	}

	start := m.execCursor - (window - 1)
	if start < 1 {
		start = 1
	}
	end := start + window
	if end > len(m.executions) {
		end = len(m.executions)
		start = max(1, end-window)
	}
	for i := start; i < end; i++ {
		indices = append(indices, i)
	}
	return indices, start > 1, end < len(m.executions)
}

func (m Model) viewExecutionHistoryLine(idx int, ex *domain.Execution, width int) string {
	label := executionHistoryLabel(idx, ex.CompletedAt)
	statusText, statusPaint := executionHistoryStatus(ex)

	cursor := "  "
	if idx == m.execCursor {
		cursor = "▸ "
	}

	statusWidth := lipgloss.Width(statusText)
	labelAvail := width - lipgloss.Width(cursor) - statusWidth - 2
	if labelAvail < 8 {
		labelAvail = 8
	}
	label = truncate(label, labelAvail)
	gap := width - lipgloss.Width(cursor) - lipgloss.Width(label) - statusWidth
	if gap < 1 {
		gap = 1
	}

	line := cursor + label + strings.Repeat(" ", gap) + statusPaint.Render(statusText)
	if idx == m.execCursor {
		line = lipgloss.NewStyle().Bold(true).Render(line)
	}
	return line
}

func executionHistoryLabel(idx int, completedAt time.Time) string {
	if idx == 0 {
		return "Latest"
	}
	if completedAt.IsZero() {
		return "Unknown time"
	}

	local := completedAt.In(time.Local)
	now := time.Now().In(time.Local)
	if sameLocalDay(local, now) {
		return "Today, " + local.Format("3:04 PM")
	}
	if sameLocalDay(local, now.AddDate(0, 0, -1)) {
		return "Yesterday, " + local.Format("3:04 PM")
	}
	return local.Format("2006-01-02 3:04 PM")
}

func executionHistoryStatus(ex *domain.Execution) (string, lipgloss.Style) {
	if ex == nil {
		return "—", mutedStyle
	}
	if ex.Error != "" && ex.StatusCode == 0 {
		return "ERR", errorStyle
	}
	if ex.StatusCode == 0 {
		return "—", mutedStyle
	}
	switch {
	case ex.StatusCode >= 400:
		return fmt.Sprintf("%d", ex.StatusCode), errorStyle
	case ex.StatusCode >= 300:
		return fmt.Sprintf("%d", ex.StatusCode), warnStyle
	default:
		return fmt.Sprintf("%d", ex.StatusCode), goodStyle
	}
}

func sameLocalDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func sortedHeaderKeys(headers http.Header) []string {
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sortHeaderKeys(keys)
	return keys
}

func sortedStringMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sortHeaderKeys(keys)
	return keys
}

func sortHeaderKeys(keys []string) {
	sort.Slice(keys, func(i, j int) bool {
		ri := headerPriority(keys[i])
		rj := headerPriority(keys[j])
		if ri != rj {
			return ri < rj
		}

		li := strings.ToLower(keys[i])
		lj := strings.ToLower(keys[j])
		if li != lj {
			return li < lj
		}
		return keys[i] < keys[j]
	})
}

func headerPriority(key string) int {
	switch strings.ToLower(key) {
	case "content-type":
		return 0
	case "content-length":
		return 1
	case "date":
		return 2
	case "server":
		return 3
	default:
		return 100
	}
}

// --- Status bar ---

func (m Model) viewStatusBar(statusOverride string) string {
	items := []hintItem{
		{Label: "quit", Actions: []string{"quit"}},
		{Label: "help", Actions: []string{"help"}},
		{Label: "search", Actions: []string{keybindings.ActionSearch}},
		{Label: "pane", Actions: []string{
			keybindings.ActionFocusSidebar,
			keybindings.ActionFocusRequest,
			keybindings.ActionFocusResponse,
		}},
	}
	if !m.tmuxDetected {
		items = append(items, hintItem{Label: "cycle", Actions: []string{"pane_next", "pane_prev"}})
	}
	dim := m.effectiveDim()
	if m.mode == normalMode && m.focus == sidebarPane && m.width >= 120 && dim == DimWide {
		items = append(items,
			hintItem{Label: "nav", Actions: []string{"sidebar_up", "sidebar_down"}},
			hintItem{Label: "tree", Actions: []string{"sidebar_collapse", "sidebar_expand"}},
			hintItem{Label: "new req", Actions: []string{"sidebar_add_request"}},
			hintItem{Label: "new col", Actions: []string{"sidebar_add"}},
			hintItem{Label: "rename", Actions: []string{"sidebar_rename"}},
			hintItem{Label: "delete", Actions: []string{"sidebar_delete"}},
		)
	}
	// Build plain text first, then style after any truncation. Truncating
	// already-styled strings via stripANSI drops the muted color and falls
	// back to the terminal default foreground when the width changes.
	hintsPlain := m.renderHints(items)
	if dim != DimWide || m.forceDim != DimAuto {
		tag := "[" + dim.String() + "]"
		if m.forceDim != DimAuto {
			tag = "[dim:" + dim.String() + "]"
		}
		hintsPlain = tag + " " + hintsPlain
	}

	var rightPlain string
	var rightStyle lipgloss.Style
	switch {
	// User-facing action feedback takes priority over layout/debug overrides
	// (e.g. visual overflow), so keys like 'a' still surface "Select a
	// collection first" in the status bar.
	case m.statusSuccess != "":
		rightPlain = "  ✓ " + m.statusSuccess
		rightStyle = goodStyle
	case m.statusErr != "":
		rightPlain = "  ✗ " + m.statusErr
		rightStyle = errorStyle
	case statusOverride != "":
		rightPlain = "  ✗ " + statusOverride
		rightStyle = errorStyle
	case m.err != nil:
		rightPlain = "  ✗ " + m.err.Error()
		rightStyle = errorStyle
	case m.tmuxDetected && m.showTmuxWarning:
		// BUG-010: only show tmux warning briefly (first N renders), not permanently.
		rightPlain = "  ⚠ tmux: Ctrl+w intercepted — use " + m.renderHintKeys(
			[]string{
				keybindings.ActionFocusSidebar,
				keybindings.ActionFocusRequest,
				keybindings.ActionFocusResponse,
			},
			false,
		) + " for panes"
		rightStyle = warnStyle
	}

	if rightPlain == "" {
		if m.width <= 0 {
			return statusStyle.Render(hintsPlain)
		}
		return statusStyle.Render(truncate(hintsPlain, m.width))
	}

	if m.width <= 0 {
		return statusStyle.Render(hintsPlain) + " " + rightStyle.Render(rightPlain)
	}

	rightW := lipgloss.Width(rightPlain)
	// Prefer the status/error message over hints when crowded.
	if rightW >= m.width {
		return rightStyle.Render(truncate(rightPlain, m.width))
	}

	// Right-align the status message when there's room.
	gap := m.width - lipgloss.Width(hintsPlain) - rightW
	if gap >= 1 {
		return statusStyle.Render(
			hintsPlain,
		) + strings.Repeat(
			" ",
			gap,
		) + rightStyle.Render(
			rightPlain,
		)
	}

	avail := m.width - rightW
	if avail < 1 {
		return rightStyle.Render(rightPlain)
	}
	return statusStyle.Render(truncate(hintsPlain, avail)) + rightStyle.Render(rightPlain)
}

// --- Search modal ---

func (m Model) searchModalHeight() int {
	// Never exceed the terminal: lipgloss.Place will grow the frame if the
	// child is taller than the target area.
	return max(1, m.height-4)
}

func (m Model) searchVisibleRows() int {
	// title+blank, input+blank, scroll indicators, bottom hint, border+padding
	overhead := 2 + 2 + 2 + 2 + 4
	visible := m.searchModalHeight() - overhead
	if visible < 1 {
		visible = 1
	}
	return visible
}

func (m Model) ensureSearchCursorVisible() Model {
	m.searchScroll = adjustListViewport(listViewport{
		Scroll:      m.searchScroll,
		SelectedRow: m.searchCursor,
		TotalRows:   len(m.searchResults),
		VisibleRows: m.searchVisibleRows(),
	})
	return m
}

func (m Model) searchModalWidth() int {
	return m.twoThirdsModalWidth()
}

// twoThirdsModalWidth returns (2/3)*width capped at modalMaxWidth.
func (m Model) twoThirdsModalWidth() int {
	w := m.width * 2 / 3
	maxW := m.modalMaxWidth()
	if w > maxW {
		w = maxW
	}
	if w < 1 {
		w = 1
	}
	return w
}

func (m Model) viewSearchModal() string {
	var sb strings.Builder
	if m.isCommandPalette() {
		sb.WriteString(titleStyle.Render("Command palette") + "\n\n")
	} else {
		sb.WriteString(titleStyle.Render("Search all requests") + "\n\n")
	}
	sb.WriteString(m.searchInput.View() + "\n\n")
	query := strings.TrimSpace(m.searchInput.Value())

	switch {
	case m.isCommandPalette():
		if len(m.commands) == 0 {
			sb.WriteString(mutedStyle.Render("  No commands."))
		} else {
			for i, item := range m.commands {
				cursor := "  "
				line := item.Title
				if i == m.searchCursor {
					cursor = "▸ "
					line = lipgloss.NewStyle().Foreground(blue).Bold(true).Render(line)
				}
				sb.WriteString(cursor + line + mutedStyle.Render("  "+item.Action) + "\n")
			}
		}
	case len(m.searchResults) == 0:
		// BUG-008: distinguish "not yet searched" from "searched and found nothing".
		if !m.searched {
			sb.WriteString(mutedStyle.Render("  Type to search…"))
		} else {
			sb.WriteString(mutedStyle.Render("  No results."))
		}
	default:
		rows, selectedRow := buildSearchRows(m.searchResults, m.searchCursor)
		visible := m.searchVisibleRows()
		start := min(m.searchScroll, max(0, len(rows)-visible))
		end := min(len(rows), start+visible)
		contentWidth := max(1, m.searchModalWidth()-6)
		if start > 0 {
			sb.WriteString(mutedStyle.Render("  ↑ more above") + "\n")
		}
		for i := start; i < end; i++ {
			hit := rows[i].hit
			cursor := "  "
			if i == selectedRow {
				cursor = "▸ "
			}
			prefix := cursor + methodBadge(hit.Request.Method) + " "
			line := prefix + m.renderSearchHit(
				hit,
				query,
				max(1, contentWidth-lipgloss.Width(prefix)),
			)
			if i == selectedRow {
				line = lipgloss.NewStyle().Foreground(blue).Bold(true).Render(line)
			}
			sb.WriteString(line + "\n")
		}
		if end < len(rows) {
			sb.WriteString(mutedStyle.Render("  ↓ more below") + "\n")
		}
	}

	sb.WriteString("\n" + mutedStyle.Render(m.renderHints([]hintItem{
		{Label: "select", Actions: []string{"search_select"}},
		{Label: helpLabelClose, Actions: []string{"search_cancel"}},
		{Label: "navigate", Actions: []string{"search_up", "search_down"}},
	})))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(blue).
		Padding(1, 2).
		Width(m.searchModalWidth()).
		Height(m.searchModalHeight()).
		Render(sb.String())

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) viewScheduleModal() string {
	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Schedule request") + "\n\n")
	if m.activeRequest != nil {
		sb.WriteString(mutedStyle.Render(m.activeRequest.Name) + "\n\n")
	}
	sb.WriteString(m.scheduleInput.View() + "\n\n")
	sb.WriteString(mutedStyle.Render("Examples: 10m, in 1h, 2026-06-25 18:30") + "\n\n")
	sb.WriteString(mutedStyle.Render(m.renderHints([]hintItem{
		{Label: "save", Actions: []string{"import_confirm"}},
		{Label: helpLabelClose, Actions: []string{keybindings.ActionImportCancel}},
	})))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(blue).
		Padding(1, 2).
		Width(m.twoThirdsModalWidth()).
		Height(10).
		Render(sb.String())

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) renderSearchHit(hit *search.SearchHit, query string, maxWidth int) string {
	if hit == nil || hit.Request == nil {
		return ""
	}

	collectionName := m.collectionNameForSearchHit(hit)
	name := hit.Request.Name
	if collectionName != "" {
		name = collectionName + "/" + name
	}

	nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#c0caf5"))
	urlStyle := mutedStyle
	matchStyle := lipgloss.NewStyle().Bold(true).Foreground(yellow)

	if maxWidth <= 0 {
		return ""
	}

	if strings.TrimSpace(hit.Request.URL) == "" {
		return highlightSearchMatch(truncate(name, maxWidth), query, nameStyle, matchStyle)
	}

	if lipgloss.Width(name) >= maxWidth {
		return highlightSearchMatch(truncate(name, maxWidth), query, nameStyle, matchStyle)
	}

	remaining := maxWidth - lipgloss.Width(name) - 1
	if remaining <= 0 {
		return highlightSearchMatch(truncate(name, maxWidth), query, nameStyle, matchStyle)
	}

	urlSegment := "(" + hit.Request.URL + ")"
	truncatedURL := truncate(urlSegment, remaining)
	renderedName := highlightSearchMatch(name, query, nameStyle, matchStyle)
	renderedURL := highlightSearchMatch(truncatedURL, query, urlStyle, matchStyle)
	return renderedName + " " + renderedURL
}

func (m Model) collectionNameForSearchHit(hit *search.SearchHit) string {
	if hit == nil {
		return ""
	}
	if hit.Collection != nil && strings.TrimSpace(hit.Collection.Name) != "" {
		return hit.Collection.Name
	}
	request := hit.Request
	if request == nil || request.CollectionID == "" {
		return ""
	}
	for _, col := range m.collections {
		if col != nil && col.ID == request.CollectionID {
			return col.Name
		}
	}
	return ""
}

func highlightSearchMatch(text, query string, base, match lipgloss.Style) string {
	if text == "" {
		return ""
	}
	if strings.TrimSpace(query) == "" {
		return base.Render(text)
	}

	lowerText := strings.ToLower(text)
	lowerQuery := strings.ToLower(strings.TrimSpace(query))
	if lowerQuery == "" {
		return base.Render(text)
	}

	var sb strings.Builder
	start := 0
	for {
		idx := strings.Index(lowerText[start:], lowerQuery)
		if idx < 0 {
			sb.WriteString(base.Render(text[start:]))
			break
		}
		idx += start
		end := idx + len(lowerQuery)
		if idx > start {
			sb.WriteString(base.Render(text[start:idx]))
		}
		sb.WriteString(match.Render(text[idx:end]))
		start = end
	}
	return sb.String()
}

// --- Help overlay ---

func (m Model) viewHelp() string {
	entries := keybindings.ListEntries(m.cfg.Keybindings)
	rows, selectedRow := buildHelpRows(entries, m.helpCursor)

	// --- Height budget ---
	//
	// We reserve a 2-row margin on top and bottom of the screen.
	// Inside the box: border(2) + padding(2) = 4 rows are consumed by lipgloss.
	//   title(2) + blank line after title
	//   scroll indicators (up to 2, conditional)
	//   bottom hint (2: leading \n + hint line)
	//   optional recording prompt(2) + error banner(2)
	//
	// boxHeight is the total outer height of the box (including border+padding).
	// maxLines is the number of content lines (entries + group headers) the
	// for-loop is allowed to render.
	boxHeight := m.height - 4 // 2-row margin top + bottom
	if boxHeight < 1 {
		boxHeight = 1
	}
	// Do not force a minimum taller than the terminal — Place would overflow.
	// Everything except the entry-list content:
	overhead := 2 /*title+blank*/ + 2 /*indicators*/ + 2 /*bottom hint*/ + 4 /*border+padding*/
	if m.helpEditState == helpRecording {
		overhead += 2
	}
	if m.helpEditState == helpError {
		overhead += 2
	}
	maxLines := boxHeight - overhead
	if maxLines < 3 {
		maxLines = 3
	}

	// Debug: log scroll state so we can verify behaviour at runtime.
	if m.debugLog != nil {
		fmt.Fprintf(
			m.debugLog,
			"[viewHelp] height=%d boxHeight=%d maxLines=%d scrollOffset=%d cursor=%d selectedRow=%d rows=%d entries=%d\n",
			m.height,
			boxHeight,
			maxLines,
			m.helpScrollOffset,
			m.helpCursor,
			selectedRow,
			len(rows),
			len(entries),
		)
	}

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Keyboard Reference") + "\n\n")

	if m.helpEditState == helpConfirmResetAll {
		diffs := helpResetAllDiffs(m.cfg.Keybindings)
		sb.WriteString(warnStyle.Render("  Reset all keybindings to defaults?") + "\n\n")
		sb.WriteString(
			mutedStyle.Render(
				"  The following custom bindings will be updated to their default values:",
			) + "\n",
		)
		for _, diff := range diffs {
			sb.WriteString("  - " + diff + "\n")
		}
		sb.WriteString("\n" + mutedStyle.Render(
			fmt.Sprintf(
				"  [%s] confirm  [%s] cancel",
				keybindings.FormatKey("enter"),
				keybindings.FormatKey(keybindings.GetAction(m.cfg.Keybindings, "help_close")),
			),
		))

		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(blue).
			Padding(1, 2).
			Width(m.twoThirdsModalWidth()).
			Height(boxHeight).
			Render(sb.String())

		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	}

	// Top scroll indicator — placed right after the title so the user sees
	// it before the entry list, not at the bottom.
	if m.helpScrollOffset > 0 {
		sb.WriteString(mutedStyle.Render("  ↑ more above") + "\n")
	}

	start := min(m.helpScrollOffset, max(0, len(rows)-maxLines))
	end := min(len(rows), start+maxLines)
	for i := start; i < end; i++ {
		row := rows[i]
		switch row.kind {
		case helpSpacerRow:
			sb.WriteString("\n")
		case helpGroupRow:
			sb.WriteString(
				lipgloss.NewStyle().Bold(true).Foreground(yellow).Render(row.group) + "\n",
			)
		case helpBindingRow:
			cursor := "  "
			if i == selectedRow {
				cursor = "▸ "
			}
			key := row.entry.Key
			if key == "" {
				key = mutedStyle.Render("(unbound)")
			} else {
				key = lipgloss.NewStyle().Foreground(cyan).Render(key)
			}
			line := fmt.Sprintf("%s%-20s %s", cursor, helpActionLabel(row.entry.Action), key)
			if i == selectedRow {
				line = lipgloss.NewStyle().Bold(true).Render(line)
			}
			sb.WriteString(line + "\n")
		}
	}

	// Bottom scroll indicator.
	if end < len(rows) {
		sb.WriteString(mutedStyle.Render("  ↓ more below") + "\n")
	}

	// Recording prompt.
	if m.helpEditState == helpRecording {
		sb.WriteString("\n" + warnStyle.Render("  Press key to bind, "+m.renderHints([]hintItem{
			{Label: helpLabelCancel, Actions: []string{"help_close"}},
			{Label: "unbind", Actions: []string{"help_unbind"}},
		})) + "\n")
	}

	// Error banner.
	if m.helpEditState == helpError && m.helpEditErrMsg != "" {
		sb.WriteString("\n" + errorStyle.Render("  ✗ "+m.helpEditErrMsg) + "\n")
	}

	// Bottom hint.
	if m.helpEditState == helpViewing {
		sb.WriteString("\n" + mutedStyle.Render("  "+m.renderHints([]hintItem{
			{Label: "navigate", Actions: []string{"help_up", "help_down"}},
			{Label: "edit", Actions: []string{"help_edit"}},
			{Label: "reset one", Actions: []string{"help_reset"}},
			{Label: "reset all", Actions: []string{"help_reset_all"}},
			{Label: helpLabelClose, Actions: []string{"help_close"}},
		})))
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(blue).
		Padding(1, 2).
		Width(m.twoThirdsModalWidth()).
		Height(boxHeight).
		Render(sb.String())

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// --- Import modal ---

func (m Model) viewImportModal() string {
	if m.importPreview == nil {
		return ""
	}

	p := m.importPreview
	secColor := goodStyle
	switch p.Security {
	case 1: // Review
		secColor = warnStyle
	case 2: // Dangerous
		secColor = errorStyle
	}

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Import curl command") + "\n\n")
	innerW := max(1, min(m.width-4, 70)-4)
	fmt.Fprintf(&sb, "Method:   %s\n", methodStyle.Render(p.Method))
	fmt.Fprintf(
		&sb,
		"URL:      %s\n",
		lipgloss.NewStyle().
			Foreground(lipgloss.Color("#c0caf5")).
			Render(truncate(p.URL, max(1, innerW-10))),
	)
	if len(p.Headers) > 0 {
		sb.WriteString("Headers:\n")
		for _, k := range sortedStringMapKeys(p.Headers) {
			v := p.Headers[k]
			if isCredentialHeader(k) {
				v = "[REDACTED]"
			}
			fmt.Fprintf(&sb, "  %s\n", truncate(k+": "+v, innerW))
		}
	}
	fmt.Fprintf(&sb, "Security: %s\n", secColor.Render(p.Security.String()))
	for _, w := range p.Warnings {
		sb.WriteString(warnStyle.Render("⚠ "+w) + "\n")
	}
	sb.WriteString("\n")
	sb.WriteString("Save as: " + m.importName.View() + "\n\n")
	sb.WriteString(mutedStyle.Render(m.renderHints([]hintItem{
		{Label: "import", Actions: []string{"import_confirm"}},
		{Label: helpLabelCancel, Actions: []string{keybindings.ActionImportCancel}},
	})))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(blue).
		Padding(1, 2).
		Width(min(m.width-4, 70)). //nolint:predeclared // uses Go 1.21 built-in min
		Render(sb.String())

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// --- Helpers ---

func (m Model) renderHints(items []hintItem) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		hint := m.renderHint(item)
		if hint == "" {
			continue
		}
		parts = append(parts, hint)
	}
	return strings.Join(parts, "  ")
}

func (m Model) renderHint(item hintItem) string {
	keys := m.hintKeys(item.Actions, item.IncludeAliases)
	if len(keys) == 0 {
		return ""
	}
	return fmt.Sprintf("[%s] %s", strings.Join(keys, "/"), item.Label)
}

func (m Model) renderHintKeys(actions []string, includeAliases bool) string {
	keys := m.hintKeys(actions, includeAliases)
	if len(keys) == 0 {
		return ""
	}
	return fmt.Sprintf("[%s]", strings.Join(keys, "/"))
}

func (m Model) hintKeys(actions []string, includeAliases bool) []string {
	keys := make([]string, 0, len(actions))
	for i, action := range actions {
		showAliases := includeAliases && i == 0 && len(actions) == 1
		for _, key := range keybindings.HintKeys(m.cfg.Keybindings, action, showAliases) {
			keys = append(keys, keybindings.FormatKey(key))
		}
	}
	return dedupeHintKeys(keys)
}

func dedupeHintKeys(keys []string) []string {
	seen := make(map[string]struct{}, len(keys))
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

func helpActionLabel(action string) string {
	if label, ok := helpActionLabels[action]; ok {
		return label
	}
	return strings.ReplaceAll(action, "_", " ")
}

func methodBadge(method string) string {
	color := mutedStyle
	switch method {
	case "GET":
		color = lipgloss.NewStyle().Foreground(green)
	case "POST":
		color = lipgloss.NewStyle().Foreground(blue)
	case "PUT", "PATCH":
		color = lipgloss.NewStyle().Foreground(yellow)
	case "DELETE":
		color = lipgloss.NewStyle().Foreground(red)
	}
	return color.Render(method)
}

// lineVisualRows returns how many terminal rows one logical line occupies when
// wrapped at contentWidth.
func lineVisualRows(line string, contentWidth int) int {
	return lipgloss.Height(softWrap(line, contentWidth))
}

// truncateLineToVisualRows returns the longest prefix of line that fits within
// maxVisualRows when wrapped at contentWidth.
func truncateLineToVisualRows(line string, contentWidth, maxVisualRows int) string {
	if maxVisualRows <= 0 {
		return ""
	}
	if lineVisualRows(line, contentWidth) <= maxVisualRows {
		return line
	}
	runes := []rune(line)
	lo, hi := 0, len(runes)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if lineVisualRows(string(runes[:mid]), contentWidth) <= maxVisualRows {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	if lo == 0 {
		return ""
	}
	return string(runes[:lo])
}

// limitLines clips content to fit within maxRows VISUAL rows.
// It accounts for line wrapping: a logical line wider than contentWidth
// occupies ceil(lineWidth/contentWidth) visual rows.
// A truncation notice is appended when content is clipped.
func limitLines(s string, contentWidth, maxRows int) string {
	if contentWidth <= 0 || maxRows <= 0 {
		return ""
	}
	rowsFor := func(line string) int {
		return lineVisualRows(line, contentWidth)
	}

	lines := strings.Split(s, "\n")
	var kept []string
	used := 0
	for i, line := range lines {
		rows := rowsFor(line)
		if used+rows > maxRows {
			hidden := len(lines) - i
			// The truncation notice occupies rows too, so it must fit within
			// maxRows. Drop already-kept lines until there's room, otherwise the
			// pane renders one row taller than its budget and shoves the whole
			// app up by a line (the top row scrolls off-screen).
			notice := func() string { return fmt.Sprintf("  … %d more lines", hidden) }
			for len(kept) > 0 && used+rowsFor(notice()) > maxRows {
				dropped := kept[len(kept)-1]
				kept = kept[:len(kept)-1]
				used -= rowsFor(dropped)
				hidden++
			}
			if hidden > 0 {
				n := notice()
				// On very small panes even the bare notice can wrap past the
				// budget; fall back to a single-column marker so we never
				// overflow maxRows.
				if used+rowsFor(n) > maxRows {
					n = "…"
				}
				noticeRows := rowsFor(n)
				lineBudget := maxRows - used - noticeRows
				if lineBudget > 0 && rows > lineBudget {
					if partial := truncateLineToVisualRows(
						line,
						contentWidth,
						lineBudget,
					); partial != "" {
						kept = append(kept, partial)
					}
				}
				kept = append(kept, mutedStyle.Render(n))
			}
			break
		}
		kept = append(kept, line)
		used += rows
	}
	return strings.Join(kept, "\n")
}

// softWrap re-wraps s exactly the way lipgloss does when rendering with a fixed
// content width. We must use lipgloss' own wrapping (word-aware, not a simple
// ceil(width/contentWidth) estimate) so our row math matches what actually gets
// displayed — a long line containing spaces word-wraps to MORE rows than the
// naive char-based estimate, which is what caused the response pane to spill one
// row past its budget and scroll the top of the app off-screen.
func softWrap(s string, contentWidth int) string {
	if contentWidth <= 0 {
		return s
	}
	return lipgloss.NewStyle().Width(contentWidth).Render(s)
}

// clipToRows hard-truncates content to at most maxRows visual rows. It pre-wraps
// with lipgloss so the returned string is already broken into physical lines no
// wider than contentWidth; a subsequent border render at >= contentWidth cannot
// re-wrap it, which guarantees the pane never exceeds maxRows rows. This is the
// final safety clamp that keeps a huge response body from pushing the whole app
// past the terminal height.
func clipToRows(s string, contentWidth, maxRows int) string {
	if maxRows <= 0 || contentWidth <= 0 {
		return ""
	}
	wrapped := softWrap(s, contentWidth)
	lines := strings.Split(wrapped, "\n")
	if len(lines) <= maxRows {
		return wrapped
	}
	return strings.Join(lines[:maxRows], "\n")
}

func visualRows(s string, contentWidth int) int {
	if s == "" || contentWidth <= 0 {
		return 0
	}
	return lipgloss.Height(softWrap(s, contentWidth))
}

// truncate shortens s to at most maxCols terminal display columns, appending
// "…" when clipped. Uses lipgloss.Width so double-width runes (CJK, emoji)
// count correctly — unlike a byte-length cut.
func truncate(s string, maxCols int) string {
	if maxCols <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxCols {
		return s
	}
	if maxCols <= 3 {
		var b strings.Builder
		width := 0
		for _, r := range s {
			rw := lipgloss.Width(string(r))
			if width+rw > maxCols {
				break
			}
			b.WriteRune(r)
			width += rw
		}
		return b.String()
	}
	budget := maxCols - lipgloss.Width("…")
	if budget < 1 {
		return "…"
	}
	var b strings.Builder
	width := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if width+rw > budget {
			break
		}
		b.WriteRune(r)
		width += rw
	}
	b.WriteString("…")
	return b.String()
}

// cleanError strips verbose Go error-chain prefixes so only the human-readable
// tail is shown to the user (e.g. "exec: build request: invalid URL: foo" → "invalid URL: foo").
func cleanError(msg string) string {
	// Strip known internal prefixes.
	prefixes := []string{
		"exec: build request: ",
		"exec: ",
		"build request: ",
	}
	for _, p := range prefixes {
		msg = strings.TrimPrefix(msg, p)
	}
	return msg
}

// isCredentialHeader reports whether a header key holds sensitive credentials.
func isCredentialHeader(key string) bool {
	lower := strings.ToLower(key)
	switch lower {
	case "authorization", "cookie", "x-api-key", "x-auth-token", "api-key":
		return true
	}
	return false
}

// --- Env modal ---

func (m Model) envModalHeight() int {
	boxHeight := max(1, m.height-4)
	return boxHeight
}

// modalMaxWidth is the largest modal width that still fits in the terminal
// (accounting for a small margin). Never forces a width larger than the screen.
func (m Model) modalMaxWidth() int {
	return max(1, m.width-4)
}

func (m Model) envModalWidth() int {
	maxWidth := m.modalMaxWidth()
	width := m.width * 4 / 5
	if width < 70 && maxWidth >= 70 {
		width = 70
	}
	if width > maxWidth {
		width = maxWidth
	}
	if width < 1 {
		width = 1
	}
	return width
}

func (m Model) envVisibleRows() int {
	// title+blank, tabs+blank, scroll indicators, bottom hints, border+padding
	overhead := 2 + 2 + 2 + 2 + 4
	if m.envEditor.editing {
		overhead += 5
	}
	if m.envEditor.saveErr != "" {
		overhead += 2
	}
	visible := m.envModalHeight() - overhead
	if visible < 1 {
		visible = 1
	}
	return visible
}

func (m Model) viewEnvModal() string {
	if !m.envEditor.active {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Environment Variables") + "\n\n")

	// Tabs.
	var tabParts []string
	for i, t := range m.envEditor.tabs {
		label := fmt.Sprintf("[%s]", t.Name)
		if i == m.envEditor.tabIdx {
			label = lipgloss.NewStyle().Foreground(blue).Underline(true).Bold(true).Render(label)
		} else {
			label = mutedStyle.Render(label)
		}
		tabParts = append(tabParts, label)
	}
	sb.WriteString("  " + strings.Join(tabParts, "  ") + "\n\n")

	// Variables.
	if len(m.envEditor.vars) == 0 {
		sb.WriteString(
			mutedStyle.Render(
				"  No variables. Press "+m.renderHintKeys([]string{"env_add"}, false)+" to add.",
			) + "\n",
		)
	} else {
		rows, selectedRow := buildEnvVarRows(m.envEditor.vars, m.envEditor.varCursor)
		visible := m.envVisibleRows()
		start := min(m.envEditor.scroll, max(0, len(rows)-visible))
		end := min(len(rows), start+visible)
		if start > 0 {
			sb.WriteString(mutedStyle.Render("  ↑ more above") + "\n")
		}
		for i := start; i < end; i++ {
			v := rows[i].variable
			cursor := "  "
			if i == selectedRow {
				cursor = "▸ "
			}
			unsaved := ""
			if !v.Saved {
				unsaved = "*"
			}
			plain := truncate(cursor+v.Key+unsaved+" = "+v.Value, max(1, m.envModalWidth()-4))
			var line string
			if i == selectedRow {
				line = lipgloss.NewStyle().Bold(true).Render(plain)
			} else {
				line = lipgloss.NewStyle().Foreground(cyan).Render(plain)
			}
			sb.WriteString(line + "\n")
		}
		if end < len(rows) {
			sb.WriteString(mutedStyle.Render("  ↓ more below") + "\n")
		}
	}

	// Editing sub-mode.
	if m.envEditor.editing {
		sb.WriteString("\n")
		sb.WriteString("Key:   " + m.envEditor.editKey.View() + "\n")
		sb.WriteString("Value: " + m.envEditor.editVal.View() + "\n")
		sb.WriteString("\n" + mutedStyle.Render("  "+m.renderHints([]hintItem{
			{Label: "switch", Actions: []string{"env_edit_switch_field"}},
			{Label: helpLabelConfirm, Actions: []string{"env_edit_confirm"}},
			{Label: helpLabelCancel, Actions: []string{"env_cancel"}},
		})) + "\n")
	}

	// Save error.
	if m.envEditor.saveErr != "" {
		sb.WriteString("\n" + errorStyle.Render("✗ "+m.envEditor.saveErr) + "\n")
	}

	// Hints.
	sb.WriteString("\n" + mutedStyle.Render(m.renderHints([]hintItem{
		{Label: "tabs", Actions: []string{"env_tab_prev", "env_tab_next"}},
		{Label: "nav", Actions: []string{"env_up", "env_down"}},
		{Label: "add var", Actions: []string{"env_add"}},
		{Label: "new env", Actions: []string{"env_create"}},
		{Label: "edit", Actions: []string{"env_edit"}},
		{Label: "delete", Actions: []string{"env_delete"}},
		{Label: "save", Actions: []string{"env_save"}},
		{Label: helpLabelClose, Actions: []string{"env_cancel"}},
	})))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(blue).
		Padding(1, 2).
		Width(m.envModalWidth()).
		Height(m.envModalHeight()).
		Render(sb.String())

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// viewCollectionPromptModal renders the centered collection prompt overlay.
func (m Model) viewCollectionPromptModal() string {
	title := "New Collection"
	hint := "Enter name"
	boxColor := blue

	switch m.promptMode {
	case promptAddRequest:
		title = "New Request"
		hint = "Enter name"
	case promptAddEnv:
		title = "New Environment"
		hint = "Enter name"
	case promptRename:
		title = "Rename Collection"
		hint = "Enter new name"
	case promptDeleteConfirm:
		title = "Delete Collection"
		hint = "Type 'yes' to confirm"
		boxColor = red
	case promptDeleteTiny:
		// Tiny confirmation: a compact yes/no-style prompt with no text input.
		name := m.promptTargetID
		if col := m.selectedCollection(); col != nil {
			name = col.Name
		}
		var sb strings.Builder
		sb.WriteString(titleStyle.Render("Delete Collection") + "\n\n")
		sb.WriteString("Delete " + lipgloss.NewStyle().Bold(true).Render(name) + "?\n\n")
		if m.statusErr != "" {
			sb.WriteString(errorStyle.Render("✗ "+m.statusErr) + "\n\n")
		}
		sb.WriteString(mutedStyle.Render(m.renderHints([]hintItem{
			{Label: helpLabelConfirm, Actions: []string{"sidebar_delete"}},
			{Label: helpLabelCancel, Actions: []string{keybindings.ActionImportCancel}},
		})))
		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(red).
			Padding(1, 2).
			Width(min(m.width-4, 40)).
			Render(sb.String())
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	}

	var sb strings.Builder
	sb.WriteString(titleStyle.Render(title) + "\n\n")
	sb.WriteString(m.promptInput.View() + "\n")
	sb.WriteString(mutedStyle.Render("["+hint+"]") + "\n")

	if m.statusErr != "" {
		sb.WriteString("\n" + errorStyle.Render("✗ "+m.statusErr) + "\n")
	}

	sb.WriteString("\n" + mutedStyle.Render(m.renderHints([]hintItem{
		{Label: helpLabelConfirm, Actions: []string{"import_confirm"}},
		{Label: helpLabelCancel, Actions: []string{keybindings.ActionImportCancel}},
	})))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(boxColor).
		Padding(1, 2).
		Width(min(m.width-4, 60)).
		Render(sb.String())

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}
