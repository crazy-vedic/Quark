package tui

// normalLayout mirrors the dimension math in viewNormal() so mouse hit-testing
// and resize logic stay aligned with rendered pane geometry.
type normalLayout struct {
	width, height   int
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

func normalLayoutFor(width, height int) normalLayout {
	sidebarW := 26
	if width < 80 {
		sidebarW = 20
	}

	mainW := width - sidebarW - 4
	if mainW < 10 {
		mainW = 10
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
		sidebarW:        sidebarW,
		mainW:           mainW,
		sidebarInnerH:   sidebarInnerH,
		rightInnerTotal: rightInnerTotal,
		requestH:        requestH,
		responseH:       responseH,
	}
}

func (l normalLayout) sidebarRect() layoutRect {
	outerW := l.sidebarW + 2
	outerH := l.sidebarInnerH + 2
	return layoutRect{
		left:   0,
		top:    0,
		right:  outerW - 1,
		bottom: outerH - 1,
	}
}

func (l normalLayout) requestRect() layoutRect {
	left := l.sidebarW + 2
	outerW := l.mainW + 2
	outerH := l.requestH + 2
	return layoutRect{
		left:   left,
		top:    0,
		right:  left + outerW - 1,
		bottom: outerH - 1,
	}
}

func (l normalLayout) responseRect() layoutRect {
	left := l.sidebarW + 2
	top := l.requestH + 2
	outerW := l.mainW + 2
	outerH := l.responseH + 2
	return layoutRect{
		left:   left,
		top:    top,
		right:  left + outerW - 1,
		bottom: top + outerH - 1,
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
	if l.width <= 0 || l.height <= 0 {
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
