package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/search"
	"github.com/crazy-vedic/quark/internal/store"
)

// Test-support message constructors used by integration harnesses outside
// internal/tui. They expose model inputs without requiring same-directory tests.
func HttpResponseMsg(result *exec.ExecuteResult) httpResponseMsg {
	return httpResponseMsg{result: result}
}

func HttpErrMsg(err error) httpErrMsg { return httpErrMsg{err: err} }

func CollectionsLoadedMsg(cols []*domain.Collection) collectionsLoadedMsg {
	return collectionsLoadedMsg{collections: cols}
}

func RequestsLoadedMsg(colID string, reqs []*domain.Request) requestsLoadedMsg {
	return requestsLoadedMsg{collectionID: colID, requests: reqs}
}

func ErrLoadMsg(err error) errLoadMsg { return errLoadMsg{err: err} }

func SearchResultsMsg(hits []*search.SearchHit) searchResultsMsg {
	return searchResultsMsg{hits: hits}
}

func ExecutionHistoryLoadedMsg(requestID string, executions []*domain.Execution) tea.Msg {
	return executionHistoryLoadedMsg{requestID: requestID, executions: executions}
}

func ScheduledRunWakeMsg(seq int) tea.Msg { return scheduledRunWakeMsg{seq: seq} }

func ScheduledRunMissedMsg(seq int, name string) tea.Msg {
	return scheduledRunMissedMsg{seq: seq, name: name}
}

func ScheduledRunBackgroundResultMsg(sent []string, failedName string, err error) tea.Msg {
	msg := scheduledRunBackgroundResultMsg{}
	for _, name := range sent {
		msg.sent = append(msg.sent, scheduledRunSuccess{name: name})
	}
	if failedName != "" || err != nil {
		msg.failed = []scheduledRunFailure{{name: failedName, err: err}}
	}
	return msg
}

const (
	SidebarPane  = sidebarPane
	RequestPane  = requestPane
	ResponsePane = responsePane

	NormalMode           = normalMode
	SearchMode           = searchMode
	HelpMode             = helpMode
	ImportMode           = importMode
	EnvMode              = envMode
	CollectionPromptMode = collectionPromptMode
	ScheduleMode         = scheduleMode

	PromptNone          = promptNone
	PromptAdd           = promptAdd
	PromptRename        = promptRename
	PromptDeleteConfirm = promptDeleteConfirm
	PromptDeleteTiny    = promptDeleteTiny
	PromptAddRequest    = promptAddRequest
	PromptAddEnv        = promptAddEnv

	BodyTab    = bodyTab
	HeadersTab = headersTab
	RawTab     = rawTab
)

const (
	NoneField    = noneField
	URLField     = urlField
	BodyField    = bodyField
	HeadersField = headersField
	AuthField    = authField
)

func (m Model) WithLoading(v bool) Model   { m.loading = v; return m }
func (m Model) WithCancel(fn func()) Model { m.cancel = fn; return m }
func (m Model) WithFocus(p paneID) Model   { m.focus = p; return m }
func (m Model) WithMode(mo modeID) Model   { m.mode = mo; return m }

func (m Model) WithURLValue(v string) Model {
	m.urlInput.SetValue(v)
	return m
}

func (m Model) WithCollections(c []*domain.Collection) Model {
	m.collections = c
	return m
}

func (m Model) WithRequests(r []*domain.Request) Model { m.requests = r; return m }

func (m Model) WithCollectionRequests(c map[string][]*domain.Request) Model {
	m.collectionRequests = c
	return m
}

func (m Model) WithColCursor(n int) Model             { m.colCursor = n; return m }
func (m Model) WithMethod(s string) Model             { m.method = s; return m }
func (m Model) WithResponseTab(t responseTabID) Model { m.responseTab = t; return m }
func (m Model) WithActiveField(f requestField) Model  { m.activeField = f; return m }

func (m Model) WithActiveRequest(r *domain.Request) Model {
	m.activeRequest = r
	return m
}

func (m Model) WithPromptMode(p promptType) Model   { m.promptMode = p; return m }
func (m Model) WithPromptTargetID(id string) Model  { m.promptTargetID = id; return m }
func (m Model) WithPromptInputValue(v string) Model { m.promptInput.SetValue(v); return m }
func (m Model) WithHelpCursor(n int) Model          { m.helpCursor = n; return m }
func (m Model) WithHelpScrollOffset(n int) Model    { m.helpScrollOffset = n; return m }
func (m Model) WithSearchInputValue(v string) Model { m.searchInput.SetValue(v); return m }

func (m Model) WithImportPreview(p *curl.ImportResult) Model {
	m.importPreview = p
	return m
}

func (m Model) WithScheduleTimerSeq(seq int) Model {
	m.scheduleTimerSeq = seq
	return m
}

func (m Model) WithSearchCancel(fn func()) Model {
	m.searchCancel = fn
	return m
}

func (m Model) WithResponse(r *exec.ExecuteResult) Model {
	m.response = r
	return m
}

func (m Model) WithExecutions(executions []*domain.Execution) Model {
	m.executions = executions
	return m
}

func (m Model) WithExecCursor(n int) Model { m.execCursor = n; return m }

func (m Model) HistoryPopupView(width, rows int) string {
	return m.viewExecutionHistoryPopup(width, rows)
}

func (m Model) Loading() bool                                    { return m.loading }
func (m Model) Err() error                                       { return m.err }
func (m Model) StatusErr() string                                { return m.statusErr }
func (m Model) StatusSuccess() string                            { return m.statusSuccess }
func (m Model) ValidationErr() string                            { return m.validationErr }
func (m Model) Response() *exec.ExecuteResult                    { return m.response }
func (m Model) Executions() []*domain.Execution                  { return m.executions }
func (m Model) ExecCursor() int                                  { return m.execCursor }
func (m Model) Width() int                                       { return m.width }
func (m Model) Height() int                                      { return m.height }
func (m Model) Focus() paneID                                    { return m.focus }
func (m Model) Mode() modeID                                     { return m.mode }
func (m Model) Collections() []*domain.Collection                { return m.collections }
func (m Model) Requests() []*domain.Request                      { return m.requests }
func (m Model) CollectionRequests() map[string][]*domain.Request { return m.collectionRequests }
func (m Model) IsExpanded(colID string) bool                     { return m.expanded[colID] }
func (m Model) ColCursor() int                                   { return m.colCursor }
func (m Model) ReqCursor() int                                   { return m.reqCursor }
func (m Model) SidebarOffset() int                               { return m.sidebarOffset }

// SidebarCollectionClickPos returns terminal coordinates for clicking a collection
// row's name area (not the disclosure icon).
func (m Model) SidebarCollectionClickPos(colIndex int) (x, y int, ok bool) {
	rowIndex, ok := m.sidebarRowIndexForCollection(colIndex)
	if !ok {
		return 0, 0, false
	}
	return m.sidebarTreeRowScreenPos(rowIndex)
}

// SidebarCollectionDisclosurePos returns terminal coordinates for clicking a
// collection row's expand/collapse icon.
func (m Model) SidebarCollectionDisclosurePos(colIndex int) (x, y int, ok bool) {
	rowIndex, ok := m.sidebarRowIndexForCollection(colIndex)
	if !ok {
		return 0, 0, false
	}
	x, y, ok = m.sidebarTreeRowScreenPos(rowIndex)
	if !ok {
		return 0, 0, false
	}
	layout := normalLayoutFor(m.width, m.height)
	content := m.sidebarContentRect(layout)
	x = content.left + sidebarDisclosureStartX
	return x, y, true
}

// SidebarRequestClickPos returns terminal coordinates for clicking a request row.
func (m Model) SidebarRequestClickPos(colIndex, reqIndex int) (x, y int, ok bool) {
	rowIndex, ok := m.sidebarRowIndexForRequest(colIndex, reqIndex)
	if !ok {
		return 0, 0, false
	}
	return m.sidebarTreeRowScreenPos(rowIndex)
}

// SidebarMoreBelowPos returns terminal coordinates for the "more below" indicator.
func (m Model) SidebarMoreBelowPos() (x, y int, ok bool) {
	rows, start, end := m.sidebarListWindow()
	if end >= len(rows) {
		return 0, 0, false
	}
	layout := normalLayoutFor(m.width, m.height)
	content := m.sidebarContentRect(layout)
	listLine := end - start
	if start > 0 {
		listLine++
	}
	y = content.top + 1 + listLine
	x = content.left + 3
	return x, y, true
}

// RequestMethodBadgeClickPos returns terminal coordinates for clicking the method badge.
func (m Model) RequestMethodBadgeClickPos() (x, y int, ok bool) {
	if m.width <= 0 || m.height <= 0 {
		return 0, 0, false
	}
	layout := normalLayoutFor(m.width, m.height)
	chrome := m.requestPaneChromeRects(layout)
	return chrome.methodBadge.left, chrome.methodBadge.top, true
}

// RequestURLLineClickPos returns terminal coordinates for clicking the URL line.
func (m Model) RequestURLLineClickPos() (x, y int, ok bool) {
	if m.width <= 0 || m.height <= 0 {
		return 0, 0, false
	}
	layout := normalLayoutFor(m.width, m.height)
	chrome := m.requestPaneChromeRects(layout)
	return chrome.urlLine.left + 2, chrome.urlLine.top, true
}

// RequestURLTextClickPosAtColumn returns coordinates for clicking the URL text at
// the given display column offset from the start of the URL text region.
func (m Model) RequestURLTextClickPosAtColumn(col int) (x, y int, ok bool) {
	if m.width <= 0 || m.height <= 0 {
		return 0, 0, false
	}
	layout := normalLayoutFor(m.width, m.height)
	chrome := m.requestPaneChromeRects(layout)
	x = chrome.urlLine.left + col
	if x > chrome.urlLine.right {
		x = chrome.urlLine.right
	}
	return x, chrome.urlLine.top, true
}

// URLCursorPosition returns the current URL textinput cursor byte offset.
func (m Model) URLCursorPosition() int {
	return m.urlInput.Position()
}

// HeaderKeyInputClickPos returns coordinates for clicking the header key input.
func (m Model) HeaderKeyInputClickPos() (x, y int, ok bool) {
	if m.width <= 0 || m.height <= 0 || !m.headerEditing {
		return 0, 0, false
	}
	layout := normalLayoutFor(m.width, m.height)
	ll := m.requestPaneLineLayout(layout)
	content := layout.requestContentRect()
	return content.left + 2, ll.headerKeyInputY, true
}

// HeaderValueInputClickPos returns coordinates for clicking the header value input.
func (m Model) HeaderValueInputClickPos() (x, y int, ok bool) {
	return m.HeaderValueInputClickPosAtColumn(2)
}

// HeaderValueInputClickPosAtColumn returns coordinates for clicking the header
// value input at the given display column.
func (m Model) HeaderValueInputClickPosAtColumn(col int) (x, y int, ok bool) {
	if m.width <= 0 || m.height <= 0 || !m.headerEditing {
		return 0, 0, false
	}
	layout := normalLayoutFor(m.width, m.height)
	ll := m.requestPaneLineLayout(layout)
	content := layout.requestContentRect()
	return content.left + col, ll.headerValInputY, true
}

// AuthRowClickPos returns coordinates for clicking an auth editor row.
func (m Model) AuthRowClickPos(rowIndex int) (x, y int, ok bool) {
	if m.width <= 0 || m.height <= 0 || m.activeField != authField {
		return 0, 0, false
	}
	rows := m.authEditor.rows()
	if rowIndex < 0 || rowIndex >= len(rows) {
		return 0, 0, false
	}
	layout := normalLayoutFor(m.width, m.height)
	ll := m.requestPaneLineLayout(layout)
	content := layout.requestContentRect()
	x = authRowTextLeft(content.left, rows[rowIndex]) + 2
	y = ll.editorContentY + 3 + rowIndex
	return x, y, true
}

// BodyTextareaClickPos returns coordinates for clicking a body editor line/column.
func (m Model) BodyTextareaClickPos(displayLine, col int) (x, y int, ok bool) {
	if m.width <= 0 || m.height <= 0 || m.activeField != bodyField {
		return 0, 0, false
	}
	layout := normalLayoutFor(m.width, m.height)
	ll := m.requestPaneLineLayout(layout)
	if displayLine < 0 || displayLine >= m.bodyTextarea.Height() {
		return 0, 0, false
	}
	return m.bodyTextareaTextLeft(layout) + col, ll.editorContentY + displayLine, true
}

// RequestSendButtonClickPos returns terminal coordinates for clicking the send button.
func (m Model) RequestSendButtonClickPos() (x, y int, ok bool) {
	if m.width <= 0 || m.height <= 0 {
		return 0, 0, false
	}
	layout := normalLayoutFor(m.width, m.height)
	chrome := m.requestPaneChromeRects(layout)
	center := chrome.sendButton.left + (chrome.sendButton.right-chrome.sendButton.left)/2
	return center, chrome.sendButton.top, true
}

// RequestPaneContentClickPos returns coordinates inside the request pane body area.
func (m Model) RequestPaneContentClickPos() (x, y int, ok bool) {
	if m.width <= 0 || m.height <= 0 {
		return 0, 0, false
	}
	layout := normalLayoutFor(m.width, m.height)
	content := layout.requestContentRect()
	// Below chrome lines (title, method/url, optional auth, blank).
	y = content.top + 5
	if y > content.bottom-2 {
		y = content.bottom - 2
	}
	x = content.left + 4
	return x, y, true
}

func (m Model) Method() string                                   { return m.method }
func (m Model) URLValue() string                                 { return m.urlInput.Value() }
func (m Model) ResponseTab() responseTabID                       { return m.responseTab }
func (m Model) SearchResults() []*search.SearchHit               { return m.searchResults }
func (m Model) SearchInputValue() string                         { return m.searchInput.Value() }
func (m Model) ScheduleTimerSeq() int                            { return m.scheduleTimerSeq }
func (m Model) ScheduleInputValue() string                       { return m.scheduleInput.Value() }
func (m Model) ImportPreview() interface{}                       { return m.importPreview }
func (m Model) TmuxDetected() bool                               { return m.tmuxDetected }
func (m Model) TmuxWarning() string                              { return m.tmuxWarning }
func (m Model) ActiveField() requestField                        { return m.activeField }
func (m Model) ActiveRequest() *domain.Request                   { return m.activeRequest }
func (m Model) BodyValue() string                                { return m.bodyTextarea.Value() }
func (m Model) HeaderPairs() []headerPair                        { return m.headerPairs }
func (m Model) HeaderEditing() bool                              { return m.headerEditing }
func (m Model) AuthEditor() authEditor                           { return m.authEditor }
func (m Model) Searched() bool                                   { return m.searched }
func (m Model) PromptMode() promptType                           { return m.promptMode }
func (m Model) HelpScrollOffset() int                            { return m.helpScrollOffset }
func (m Model) CommandResults() []commandPaletteItem             { return m.commands }
func (m Model) SearchCancel() context.CancelFunc                 { return m.searchCancel }
func (m Model) ModelCtx() context.Context                        { return m.ctx }
func ScheduleWakeAfterCmd(ctx context.Context, delay time.Duration, seq int) tea.Cmd {
	return scheduleWakeAfterCmd(ctx, delay, seq)
}

func (m Model) ActiveEnv() map[string]string { return m.activeEnv }
func (m Model) EnvEditorActive() bool        { return m.envEditor.active }
func (m Model) EnvEditorTabIdx() int         { return m.envEditor.tabIdx }
func (m Model) EnvEditorVars() []envVar      { return m.envEditor.vars }
func (m Model) EnvEditorVarCursor() int      { return m.envEditor.varCursor }
func (m Model) EnvEditorEditing() bool       { return m.envEditor.editing }
func (m Model) EnvEditorSaveErr() string     { return m.envEditor.saveErr }
func EnvSavedMsg() tea.Msg                   { return envSavedMsg{} }
func EnvSaveErrMsg(err error) tea.Msg        { return envSaveErrMsg{err: err} }
func CollectionSavedMsg() tea.Msg            { return collectionSavedMsg{} }
func CollectionSavedErrMsg(err error) tea.Msg {
	return collectionSavedErrMsg{err: err}
}

func (m Model) EnvEditor() envEditor { return m.envEditor }

func (m Model) WithEnvReader(r store.EnvironmentReader) Model { m.envReader = r; return m }
func (m Model) WithEnvWriter(w store.EnvironmentWriter) Model { m.envWriter = w; return m }
func (m Model) WithColWriter(w store.CollectionWriter) Model  { m.colWriter = w; return m }
