package tui

import (
	"fmt"
	"strings"

	"github.com/crazy-vedic/quark/internal/keybindings"
)

// Absurd floor: below this the UI shows a "too small" message instead of panes.
const (
	MinTerminalWidth  = 24
	MinTerminalHeight = 8
	minMainW          = 10
	paneBorderPad     = 4 // two 2-col borders between sidebar and main (wide mode)

	// Auto dim breakpoints (either axis can trip a denser mode).
	dimWideMinW   = 80
	dimWideMinH   = 18
	dimNarrowMinW = 48
	dimNarrowMinH = 14
	dimTinyMinW   = 24
	dimTinyMinH   = 8
)

// DimMode is the TUI density / layout tier.
type DimMode int

const (
	// DimAuto means "choose from terminal size" when used as ForceDim.
	DimAuto DimMode = iota
	DimWide
	DimNarrow
	DimTiny
	DimAbsurd
)

// String returns the CLI / debug name for a dim mode.
func (d DimMode) String() string {
	switch d {
	case DimWide:
		return "wide"
	case DimNarrow:
		return "narrow"
	case DimTiny:
		return "tiny"
	case DimAbsurd:
		return "absurd"
	default:
		return "auto"
	}
}

// ParseDimMode parses a --dim flag value.
func ParseDimMode(s string) (DimMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "wide":
		return DimWide, nil
	case "narrow":
		return DimNarrow, nil
	case "tiny":
		return DimTiny, nil
	case "absurd":
		return DimAbsurd, nil
	case "", "auto":
		return DimAuto, nil
	default:
		return DimAuto, fmt.Errorf("invalid dim %q (want wide|narrow|tiny|absurd)", s)
	}
}

// dimFromSize picks the layout tier from terminal dimensions.
func dimFromSize(width, height int) DimMode {
	if width < dimTinyMinW || height < dimTinyMinH {
		return DimAbsurd
	}
	if width < dimNarrowMinW || height < dimNarrowMinH {
		return DimTiny
	}
	if width < dimWideMinW || height < dimWideMinH {
		return DimNarrow
	}
	return DimWide
}

// terminalTooSmall reports whether the terminal is in the absurd tier (auto).
func terminalTooSmall(width, height int) bool {
	return dimFromSize(width, height) == DimAbsurd
}

// normalLayout mirrors the dimension math in View() so mouse hit-testing
// and resize logic stay aligned with rendered pane geometry.
type normalLayout struct {
	width, height int
	mode          DimMode
	focus         paneID // used by tiny mode rects / paneAt

	sidebarW        int
	mainW           int
	sidebarInnerH   int
	rightInnerTotal int
	requestH        int
	responseH       int
}

type layoutRect struct {
	left, top, right, bottom int
}

func (r layoutRect) contains(x, y int) bool {
	return x >= r.left && x <= r.right && y >= r.top && y <= r.bottom
}

// pickSidebarW chooses the largest sidebar from the shrink ladder that leaves
// at least minMainW columns for the main pane. Mid-size terminals (<80)
// prefer 20 so the request/response panes stay usable.
func pickSidebarW(width int) int {
	candidates := []int{26, 20, 14, 10}
	preferred := 26
	if width < 80 {
		preferred = 20
	}

	start := 0
	for i, c := range candidates {
		if c == preferred {
			start = i
			break
		}
	}
	for _, sidebarW := range candidates[start:] {
		if width-sidebarW-paneBorderPad >= minMainW {
			return sidebarW
		}
	}
	for _, sidebarW := range candidates {
		if width-sidebarW-paneBorderPad >= 1 {
			return sidebarW
		}
	}
	return candidates[len(candidates)-1]
}

// layoutFor builds pane geometry for the given density mode and focus.
func layoutFor(width, height int, mode DimMode, focus paneID) normalLayout {
	if mode == DimAuto {
		mode = dimFromSize(width, height)
	}
	switch mode {
	case DimNarrow:
		return layoutStacked(width, height)
	case DimTiny:
		return layoutTiny(width, height, focus)
	case DimAbsurd:
		return normalLayout{width: width, height: height, mode: DimAbsurd, focus: focus}
	default:
		return layoutWide(width, height)
	}
}

// normalLayoutFor builds auto-dim layout (wide/narrow/tiny) for tests and
// callers that do not carry focus. Tiny defaults to sidebar focus.
func normalLayoutFor(width, height int) normalLayout {
	return layoutFor(width, height, dimFromSize(width, height), sidebarPane)
}

func layoutWide(width, height int) normalLayout {
	sidebarW := pickSidebarW(width)
	mainW := width - sidebarW - paneBorderPad
	if mainW < 1 {
		mainW = 1
	}

	sidebarInnerH := height - 3
	if sidebarInnerH < 1 {
		sidebarInnerH = 1
	}

	rightInnerTotal := height - 5
	if rightInnerTotal < 2 {
		rightInnerTotal = 2
	}
	requestH := rightInnerTotal / 2
	responseH := rightInnerTotal - requestH

	return normalLayout{
		width:           width,
		height:          height,
		mode:            DimWide,
		sidebarW:        sidebarW,
		mainW:           mainW,
		sidebarInnerH:   sidebarInnerH,
		rightInnerTotal: rightInnerTotal,
		requestH:        requestH,
		responseH:       responseH,
	}
}

func layoutStacked(width, height int) normalLayout {
	// Full-width panes stacked above the status bar.
	innerW := width - 2
	if innerW < 1 {
		innerW = 1
	}

	available := height - 1 // status row
	if available < 3 {
		available = 3
	}
	// Split outer heights; prefer giving leftover rows to the response pane.
	o1 := available / 3
	o2 := available / 3
	o3 := available - o1 - o2
	if o1 < 1 {
		o1 = 1
	}
	if o2 < 1 {
		o2 = 1
	}
	if o3 < 1 {
		o3 = 1
	}

	sidebarInnerH := max(1, o1-2)
	requestH := max(1, o2-2)
	responseH := max(1, o3-2)

	return normalLayout{
		width:           width,
		height:          height,
		mode:            DimNarrow,
		sidebarW:        innerW,
		mainW:           innerW,
		sidebarInnerH:   sidebarInnerH,
		rightInnerTotal: requestH + responseH,
		requestH:        requestH,
		responseH:       responseH,
	}
}

func layoutTiny(width, height int, focus paneID) normalLayout {
	innerW := width - 2
	if innerW < 1 {
		innerW = 1
	}
	innerH := height - 3 // border + status
	if innerH < 1 {
		innerH = 1
	}
	return normalLayout{
		width:           width,
		height:          height,
		mode:            DimTiny,
		focus:           focus,
		sidebarW:        innerW,
		mainW:           innerW,
		sidebarInnerH:   innerH,
		rightInnerTotal: innerH,
		requestH:        innerH,
		responseH:       innerH,
	}
}

func (l normalLayout) sidebarOuterH() int {
	switch l.mode {
	case DimNarrow:
		return l.sidebarInnerH + 2
	case DimTiny:
		return l.height - 1
	default:
		return l.sidebarInnerH + 2
	}
}

func (l normalLayout) requestOuterH() int {
	return l.requestH + 2
}

func (l normalLayout) responseOuterH() int {
	return l.responseH + 2
}

func (l normalLayout) sidebarRect() layoutRect {
	switch l.mode {
	case DimNarrow:
		outerH := l.sidebarOuterH()
		return layoutRect{left: 0, top: 0, right: l.width - 1, bottom: outerH - 1}
	case DimTiny:
		if l.focus != sidebarPane {
			return layoutRect{left: -1, top: -1, right: -1, bottom: -1}
		}
		return layoutRect{left: 0, top: 0, right: l.width - 1, bottom: l.height - 2}
	default:
		outerW := l.sidebarW + 2
		outerH := l.sidebarInnerH + 2
		return layoutRect{left: 0, top: 0, right: outerW - 1, bottom: outerH - 1}
	}
}

func (l normalLayout) requestRect() layoutRect {
	switch l.mode {
	case DimNarrow:
		top := l.sidebarOuterH()
		outerH := l.requestOuterH()
		return layoutRect{left: 0, top: top, right: l.width - 1, bottom: top + outerH - 1}
	case DimTiny:
		if l.focus != requestPane {
			return layoutRect{left: -1, top: -1, right: -1, bottom: -1}
		}
		return layoutRect{left: 0, top: 0, right: l.width - 1, bottom: l.height - 2}
	default:
		left := l.sidebarW + 2
		outerW := l.mainW + 2
		outerH := l.requestH + 2
		return layoutRect{left: left, top: 0, right: left + outerW - 1, bottom: outerH - 1}
	}
}

func (l normalLayout) responseRect() layoutRect {
	switch l.mode {
	case DimNarrow:
		top := l.sidebarOuterH() + l.requestOuterH()
		outerH := l.responseOuterH()
		return layoutRect{left: 0, top: top, right: l.width - 1, bottom: top + outerH - 1}
	case DimTiny:
		if l.focus != responsePane {
			return layoutRect{left: -1, top: -1, right: -1, bottom: -1}
		}
		return layoutRect{left: 0, top: 0, right: l.width - 1, bottom: l.height - 2}
	default:
		left := l.sidebarW + 2
		top := l.requestH + 2
		outerW := l.mainW + 2
		outerH := l.responseH + 2
		return layoutRect{left: left, top: top, right: left + outerW - 1, bottom: top + outerH - 1}
	}
}

func (l normalLayout) requestContentRect() layoutRect {
	r := l.requestRect()
	return layoutRect{
		left:   r.left + 1,
		top:    r.top + 1,
		right:  r.right - 1,
		bottom: r.bottom - 1,
	}
}

func (l normalLayout) responseContentRect() layoutRect {
	r := l.responseRect()
	return layoutRect{
		left:   r.left + 1,
		top:    r.top + 1,
		right:  r.right - 1,
		bottom: r.bottom - 1,
	}
}

type responsePaneTabRects struct {
	body, headers, raw layoutRect
	tabBarY            int
}

// responsePaneTabRects returns hit targets for the Body/Headers/Raw tab labels.
// Coordinates match the plain-text layout of viewTabBar (ANSI styling ignored).
func (m Model) responsePaneTabRects(layout normalLayout) responsePaneTabRects {
	content := layout.responseContentRect()
	y := content.top + 1 // skip title row
	if m.selectedExecution() != nil {
		y += 2 // Run N/M line + status
	} else if m.response != nil {
		y++ // live status
	}

	tabs := []struct {
		action string
		label  string
		set    func(*responsePaneTabRects, layoutRect)
	}{
		{"tab_body", "Body", func(r *responsePaneTabRects, rect layoutRect) { r.body = rect }},
		{
			"tab_headers",
			"Headers",
			func(r *responsePaneTabRects, rect layoutRect) { r.headers = rect },
		},
		{"tab_raw", "Raw", func(r *responsePaneTabRects, rect layoutRect) { r.raw = rect }},
	}

	out := responsePaneTabRects{tabBarY: y}
	x := content.left + 2 // leading indent in viewTabBar
	for i, t := range tabs {
		if i > 0 {
			x += 2 // join separator
		}
		key := keybindings.FormatKey(keybindings.GetAction(m.cfg.Keybindings, t.action))
		plain := "[" + key + "] " + t.label
		w := len(plain)
		rect := layoutRect{left: x, top: y, right: x + w - 1, bottom: y}
		t.set(&out, rect)
		x += w
	}
	return out
}

type requestPaneChromeRects struct {
	methodBadge layoutRect
	urlLine     layoutRect
	sendButton  layoutRect
}

func (m Model) requestPaneChromeRects(layout normalLayout) requestPaneChromeRects {
	content := layout.requestContentRect()
	badgeW := methodBadgeWidth(m.method)
	methodY := content.top + 1
	urlLeft := content.left + badgeW + 2 // badge + "  " separator

	sendW := requestSendButtonWidth()
	rightW := m.requestTitleRightChromeWidth()
	sendLeft := content.right - rightW + 1

	return requestPaneChromeRects{
		methodBadge: layoutRect{
			left:   content.left,
			top:    methodY,
			right:  content.left + badgeW - 1,
			bottom: methodY,
		},
		urlLine: layoutRect{
			left:   urlLeft,
			top:    methodY,
			right:  content.right,
			bottom: methodY,
		},
		sendButton: layoutRect{
			left:   sendLeft,
			top:    content.top,
			right:  sendLeft + sendW - 1,
			bottom: content.top,
		},
	}
}

// paneAt maps terminal coordinates to a normal-mode pane. Returns false when
// the layout is unset, the point is outside pane regions, or it falls on the
// status bar row.
func (l normalLayout) paneAt(x, y int) (paneID, bool) {
	if l.width <= 0 || l.height <= 0 || l.mode == DimAbsurd {
		return 0, false
	}
	if y >= l.height-1 {
		return 0, false
	}

	switch {
	case l.sidebarRect().contains(x, y):
		return sidebarPane, true
	case l.requestRect().contains(x, y):
		return requestPane, true
	case l.responseRect().contains(x, y):
		return responsePane, true
	default:
		return 0, false
	}
}
