package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/keybindings"
	"github.com/crazy-vedic/quark/internal/schedule"
	"github.com/crazy-vedic/quark/internal/store"
)

// Update implements tea.Model — routes every message to the correct handler.
//
//nolint:gocyclo // Bubble Tea keeps the top-level message dispatcher centralized.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.activeField == bodyField {
			m = m.resizeBodyTextarea()
		}
		return m, nil

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case collectionsLoadedMsg:
		m.collections = msg.collections
		m.colCursor = 0
		// Auto-load requests for the first collection.
		if len(m.collections) > 0 && m.reader != nil {
			id := m.collections[0].ID
			m.expanded[id] = true
			return m, loadRequestsCmd(m.ctx, m.reader, id)
		}
		return m, nil

	case requestsLoadedMsg:
		m.collectionRequests[msg.collectionID] = msg.requests
		// If this is the currently selected collection, also set m.requests
		// for the Enter handler on a request.
		if m.activeCollectionID() == msg.collectionID {
			m.requests = msg.requests
		}
		// Also try to load persisted active env for this collection.
		if m.activeEnvStore != nil {
			return m, loadActiveEnvCmd(m.ctx, m.activeEnvStore, msg.collectionID)
		}
		return m, nil

	case errLoadMsg:
		m.err = fmt.Errorf("load: %w", msg.err)
		return m, nil

	case searchResultsMsg:
		m.searchResults = msg.hits
		m.searchCursor = 0
		m.searchScroll = 0
		m.searched = true // BUG-008: mark that at least one search has completed
		return m, nil

	case executionHistoryLoadedMsg:
		if m.activeRequest == nil || msg.requestID != m.activeRequest.ID {
			return m, nil
		}
		m.executions = msg.executions
		m.execCursor = 0
		return m, nil

	case tea.KeyMsg:
		if m.debugLog != nil {
			fmt.Fprintf(
				m.debugLog,
				"[%s] key=%q type=%d type_name=%q runes=%q rune_codes=%s alt=%v paste=%v focus=%d mode=%d colCursor=%d reqCursor=%d requests=%d expanded=%v\n",
				time.Now().
					Format("15:04:05.000"),
				msg.String(),
				msg.Type,
				msg.Type.String(),
				string(msg.Runes),
				debugRuneCodes(msg.Runes),
				msg.Alt,
				msg.Paste,
				m.focus,
				m.mode,
				m.colCursor,
				m.reqCursor,
				len(m.requests),
				m.expanded,
			)
		}
		// Global shortcuts that work in all modes.
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyEsc:
			if m.mode == normalMode {
				m = m.handleEsc()
				return m, nil
			}
		}

		// Mode-specific routing.
		switch m.mode {
		case searchMode:
			return m.handleSearchKey(msg)
		case helpMode:
			return m.handleHelpKey(msg)
		case envMode:
			return m.handleEnvKey(msg)
		case importMode:
			return m.handleImportKey(msg)
		case collectionPromptMode:
			return m.handleCollectionPromptKey(msg)
		case scheduleMode:
			return m.handleScheduleKey(msg)
		default:
			return m.handleNormalKey(msg)
		}

	case httpResponseMsg:
		return m.handleHTTPResponse(msg)

	case httpErrMsg:
		return m.handleHTTPErr(msg)
	case envSavedMsg:
		m = m.status("success", "Environment saved")
		m.envEditor.dirty = false
		// Mark all variables as saved so the unsaved indicator (*) disappears.
		for i := range m.envEditor.vars {
			m.envEditor.vars[i].Saved = true
		}
		// Invalidate cached env name — saved env may be the active one.
		m.cachedEnvColID = ""
		return m, nil
	case envSaveErrMsg:
		m.envEditor.saveErr = fmt.Sprintf("Save failed: %v", msg.err)
		return m, nil
	case envCreatedMsg:
		m = m.status("success", "Environment created")
		if m.envReader != nil {
			return m.openEnvEditor()
		}
		return m, nil
	case envLoadedMsg:
		if msg.envID != "" {
			m.activeEnv[msg.collectionID] = msg.envID
		}
		// Invalidate cached env name on active-env change.
		m.cachedEnvColID = ""
		return m, nil
	case promptCompletedMsg:
		m = m.closeCollectionPrompt()
		if msg.inner == nil {
			return m, nil
		}
		return m.Update(msg.inner)
	case collectionSavedMsg:
		if m.mode == collectionPromptMode {
			m = m.closeCollectionPrompt()
		}
		if m.lister != nil {
			return m, loadCollectionsCmd(m.ctx, m.lister)
		}
		return m, nil
	case collectionSavedErrMsg:
		m = m.status("error", fmt.Sprintf("Collection failed: %v", msg.err))
		return m, nil
	case scheduledRunSavedMsg:
		m = m.closeSchedulePrompt()
		m = m.status("success", "Scheduled for "+msg.runAt.Format(time.RFC3339))
		return m.armScheduleTimer()
	case scheduledRunSaveErrMsg:
		m = m.status("error", fmt.Sprintf("Schedule failed: %v", msg.err))
		return m, nil
	case scheduledRunMissedMsg:
		if msg.seq != m.scheduleTimerSeq {
			return m, nil
		}
		m = m.status(
			"error",
			fmt.Sprintf("We missed executing your scheduled request %q", msg.name),
		)
		return m, scheduleWakeAfterCmd(m.ctx, 15*time.Second, msg.seq)
	case scheduledRunWakeMsg:
		if msg.seq != m.scheduleTimerSeq {
			return m, nil
		}
		return m, executeDueScheduledRunsCmd(
			m.ctx,
			m.scheduler,
			m.lister,
			m.reader,
			m.executor,
			m.envReader,
			m.activeEnvStore,
			m.activeEnv,
			m.now,
		)
	case scheduledRunBackgroundResultMsg:
		m, historyCmd := m.applyScheduledRunBackgroundResult(msg)
		m, timerCmd := m.armScheduleTimer()
		return m, tea.Batch(historyCmd, timerCmd)

	}
	// Delegate to active inline fields for non-key messages (cursor blink, etc.).
	if m.activeField == urlField {
		var cmd tea.Cmd
		m.urlInput, cmd = m.urlInput.Update(msg)
		// Auto-detect curl paste.
		if strings.HasPrefix(strings.TrimSpace(m.urlInput.Value()), "curl") {
			return m.triggerCurlImport(m.urlInput.Value())
		}
		return m, cmd
	}
	if m.activeField == bodyField {
		var cmd tea.Cmd
		m.bodyTextarea, cmd = m.bodyTextarea.Update(msg)
		return m, cmd
	}
	if m.activeField == authField {
		if !m.authEditor.editing {
			return m, nil
		}
		var cmd tea.Cmd
		switch m.authEditor.currentRow() {
		case authRowToken:
			m.authEditor.tokenInput, cmd = m.authEditor.tokenInput.Update(msg)
		case authRowUsername:
			m.authEditor.usernameInput, cmd = m.authEditor.usernameInput.Update(msg)
		case authRowPassword:
			m.authEditor.passwordInput, cmd = m.authEditor.passwordInput.Update(msg)
		case authRowAPIKeyName:
			m.authEditor.apiKeyNameInput, cmd = m.authEditor.apiKeyNameInput.Update(msg)
		case authRowAPIKeyValue:
			m.authEditor.apiKeyValueInput, cmd = m.authEditor.apiKeyValueInput.Update(msg)
		}
		return m, cmd
	}
	if m.activeField == headersField && m.headerEditing {
		var cmd tea.Cmd
		if m.headerKeyInput.Focused() {
			m.headerKeyInput, cmd = m.headerKeyInput.Update(msg)
		} else {
			m.headerValueInput, cmd = m.headerValueInput.Update(msg)
		}
		return m, cmd
	}

	return m, nil
}

// --- Global handlers ---

func (m Model) handleEsc() Model {
	switch m.mode {
	case searchMode:
		return m.closeSearch()
	case helpMode:
		return m.closeHelp()
	case envMode:
		return m.closeEnvEditor()
	case importMode:
		return m.closeImport()
	case collectionPromptMode:
		return m.closeCollectionPrompt()
	}
	// In normal mode: cancel in-flight request.
	if m.loading && m.cancel != nil {
		m.cancel()
		m.cancel = nil
		m.loading = false
	}
	// Blur active inline field and revert changes.
	return m.cancelActiveFieldEdit()
}

// --- Normal mode ---

func (m Model) handleNormalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// BUG-010: dismiss tmux warning on first interaction.
	m.showTmuxWarning = false

	// When any inline field is active, all keys go directly to the request handler.
	// This prevents 'q', '/', '?' etc. from firing global shortcuts mid-edit.
	if m.activeField != noneField {
		return m.handleRequestKey("", msg)
	}

	if action, ok := m.resolver.Resolve(0, int(m.focus), msg); ok {
		return m.dispatchNormalAction(action)
	}
	// Key didn't map to any action — intentional no-op.
	return m, nil
}

// dispatchNormalAction routes a resolver action in normal mode.
// It mirrors the exact logic from the hardcoded switch above.
func (m Model) dispatchNormalAction(action string) (tea.Model, tea.Cmd) {
	switch action {
	case "quit":
		return m, tea.Quit
	case "help":
		return m.openHelp(), nil
	case keybindings.ActionSearch:
		return m.openSearch()
	case keybindings.ActionFocusSidebar:
		m.focus = sidebarPane
		m.activeField = noneField
		return m, nil
	case keybindings.ActionFocusRequest:
		m.focus = requestPane
		return m, nil
	case keybindings.ActionFocusResponse:
		m.focus = responsePane
		m.activeField = noneField
		return m, nil
	case "pane_next":
		m.focus = (m.focus + 1) % 3
		m.activeField = noneField
		return m, nil
	case "pane_prev":
		m.focus = (m.focus + 2) % 3
		m.activeField = noneField
		return m, nil
	case keybindings.ActionEnvOpen:
		return m.openEnvEditor()
	case "env_next":
		return m.cycleEnv(1)
	case "env_prev":
		return m.cycleEnv(-1)
	}

	// Pane-specific actions.
	switch m.focus {
	case sidebarPane:
		return m.handleSidebarAction(action)
	case requestPane:
		return m.handleRequestAction(action)
	case responsePane:
		return m.handleResponseAction(action)
	}
	return m, nil
}

// handleSidebarAction is the action-based version of handleSidebarKey.
func (m Model) handleSidebarAction(action string) (tea.Model, tea.Cmd) {
	switch action {
	case "cursor_down":
		return m.sidebarDown()
	case "cursor_up":
		return m.sidebarUp()
	case "expand":
		colID := m.activeCollectionID()
		if colID == "" {
			return m, nil
		}
		// If a request is selected, load it and shift focus.
		if m.expanded[colID] && m.reqCursor >= 0 {
			reqs := m.collectionRequests[colID]
			if m.reqCursor < len(reqs) {
				m.requests = reqs
				m.focus = requestPane
				return m.selectRequest(reqs[m.reqCursor])
			}
		}
		// Otherwise toggle expand/collapse.
		if m.expanded[colID] {
			m.expanded[colID] = false
			delete(m.collectionRequests, colID)
			m.reqCursor = -1
		} else {
			m.expanded[colID] = true
			if m.reader != nil {
				return m, loadRequestsCmd(m.ctx, m.reader, colID)
			}
		}
		return m, nil
	case "collapse":
		colID := m.activeCollectionID()
		if colID != "" {
			m.expanded[colID] = false
			delete(m.collectionRequests, colID)
			m.reqCursor = -1
		}
		return m, nil
	case "add":
		return m.enterCollectionPrompt(promptAdd, "")
	case "add_request":
		colID := m.activeCollectionID()
		if colID == "" {
			return m.status("error", "Select a collection first"), nil
		}
		return m.enterCollectionPrompt(promptAddRequest, colID)
	case "delete":
		colID := m.activeCollectionID()
		if colID == "" {
			return m.status("error", "Select a collection first"), nil
		}
		return m.enterCollectionPrompt(promptDeleteTiny, colID)
	case "rename":
		colID := m.activeCollectionID()
		if colID == "" {
			return m.status("error", "Select a collection first"), nil
		}
		return m.enterCollectionPrompt(promptRename, colID)
	}
	return m, nil
}

// handleRequestAction is the action-based version of handleRequestKey.
func (m Model) handleRequestAction(action string) (tea.Model, tea.Cmd) {
	switch action {
	case keybindings.ActionEditURL:
		return m.beginURLEdit()
	case keybindings.ActionMethodNext:
		return m.cycleMethod(nextMethod)
	case keybindings.ActionMethodPrev:
		return m.cycleMethod(prevMethod)
	case keybindings.ActionSendRequest:
		if m.loading || m.executor == nil {
			return m, nil
		}
		req := m.activePaneRequest()
		return m.startRequest(req)
	case keybindings.ActionScheduleRun:
		return m.openSchedulePrompt()
	case keybindings.ActionEditBody:
		return m.beginBodyEdit(), nil
	case keybindings.ActionEnvOpen:
		return m.openEnvEditor()
	case "env_next":
		return m.cycleEnv(1)
	case "env_prev":
		return m.cycleEnv(-1)
	case "edit_auth":
		return m.beginAuthEdit(), nil
	case keybindings.ActionEditHeaders:
		return m.beginHeadersEdit(), nil
	}
	return m, nil
}

// handleResponseAction routes resolver actions for the response pane.
func (m Model) handleResponseAction(action string) (tea.Model, tea.Cmd) {
	switch action {
	case "history_next":
		if m.execCursor < len(m.executions)-1 {
			m.execCursor++
		}
	case "history_prev":
		if m.execCursor > 0 {
			m.execCursor--
		}
	case "retry_request":
		if m.loading || m.executor == nil {
			return m, nil
		}
		req, err := m.retryableRawRequest()
		if err != nil {
			return m.status("error", err.Error()), nil
		}
		return m.startRawRequest(req)
	case "tab_body":
		m.responseTab = bodyTab
	case "tab_headers":
		m.responseTab = headersTab
	case "tab_raw":
		m.responseTab = rawTab
	case "tab_next":
		if m.responseTab == rawTab {
			m.responseTab = bodyTab
		} else {
			m.responseTab++
		}
	case "tab_prev":
		if m.responseTab == bodyTab {
			m.responseTab = rawTab
		} else {
			m.responseTab--
		}
	}
	return m, nil
}

type executionRequestSnapshot struct {
	Method     string `json:"method"`
	URL        string `json:"url"`
	Headers    string `json:"headers"`
	AuthType   string `json:"auth_type,omitempty"`
	AuthConfig string `json:"auth_config,omitempty"`
	Body       string `json:"body"`
}

func (m Model) retryableRawRequest() (*domain.Request, error) {
	if ex := m.selectedExecution(); ex != nil {
		var snapshot executionRequestSnapshot
		if err := json.Unmarshal([]byte(ex.RequestSnapshot), &snapshot); err != nil {
			return nil, fmt.Errorf("retry unavailable: invalid execution snapshot")
		}

		req := &domain.Request{
			ID:         ex.RequestID,
			Method:     snapshot.Method,
			URL:        snapshot.URL,
			Headers:    snapshot.Headers,
			AuthType:   snapshot.AuthType,
			AuthConfig: snapshot.AuthConfig,
			Body:       snapshot.Body,
		}
		if m.activeRequest != nil {
			req.CollectionID = m.activeRequest.CollectionID
			req.Name = m.activeRequest.Name
		}
		if req.Method == "" {
			req.Method = "GET"
		}
		return req, nil
	}

	req := m.activePaneRequest()
	if req == nil || strings.TrimSpace(req.URL) == "" {
		return nil, fmt.Errorf("retry unavailable: no request to replay")
	}
	return req, nil
}

// --- Sidebar ---

// sidebarDown moves cursor down through the full sidebar tree.
// Navigation goes: collection → (if expanded) its requests → next collection.
func (m Model) sidebarDown() (tea.Model, tea.Cmd) {
	if len(m.collections) == 0 {
		return m, nil
	}
	colID := m.activeCollectionID()
	expanded := colID != "" && m.expanded[colID]

	// If we're on the collection itself and it's expanded, enter its first request.
	if m.reqCursor == -1 && expanded {
		if reqs := m.collectionRequests[colID]; len(reqs) > 0 {
			m.reqCursor = 0
			m.requests = reqs
			return m, nil
		}
	}
	// If we're on a request and there are more requests below, move within requests.
	if m.reqCursor >= 0 && m.reqCursor < len(m.requests)-1 {
		m.reqCursor++
		return m, nil
	}
	// At end of requests (or on unexpanded collection), move to next collection.
	if m.colCursor < len(m.collections)-1 {
		m.colCursor++
		m.reqCursor = -1
		m = m.ensureSidebarCollectionVisible()
		// If the new collection is already expanded, load its requests into m.requests
		// so Enter on a request works immediately.
		newColID := m.activeCollectionID()
		if newColID != "" && m.expanded[newColID] {
			m.requests = m.collectionRequests[newColID]
		}
	}
	return m, nil
}

// sidebarUp moves cursor up through the full sidebar tree.
// Navigation goes: request → previous request (or collection) → previous collection's last request.
func (m Model) sidebarUp() (tea.Model, tea.Cmd) {
	if len(m.collections) == 0 {
		return m, nil
	}
	// If we're on a request and not at the first one, move up within requests.
	if m.reqCursor > 0 {
		m.reqCursor--
		return m, nil
	}
	// If we're on the first request (or collection with no requests), go to collection itself.
	if m.reqCursor == 0 {
		m.reqCursor = -1
		return m, nil
	}
	// We're on a collection — move to previous collection.
	if m.colCursor > 0 {
		m.colCursor--
		m = m.ensureSidebarCollectionVisible()
		prevColID := m.activeCollectionID()
		if prevColID != "" && m.expanded[prevColID] {
			if reqs := m.collectionRequests[prevColID]; len(reqs) > 0 {
				m.reqCursor = len(reqs) - 1
				m.requests = reqs
				return m, nil
			}
		}
		m.reqCursor = -1
	}
	return m, nil
}

// --- Request pane ---

func (m Model) handleRequestKey(_ string, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Inline field routing — when active, all keys go to the field.
	switch m.activeField {
	case urlField:
		var cmd tea.Cmd
		m.urlInput, cmd = m.urlInput.Update(msg)
		if strings.HasPrefix(strings.TrimSpace(m.urlInput.Value()), "curl") {
			return m.triggerCurlImport(m.urlInput.Value())
		}
		if msg.Type == tea.KeyEnter {
			return m.finishURLEdit()
		}
		return m, cmd
	case bodyField:
		return m.handleBodyFieldKey(msg)
	case authField:
		return m.handleAuthFieldKey(msg)
	case headersField:
		return m.handleHeadersFieldKey(msg)
	}

	// Alt+Enter (macOS cmd+enter) sends request.
	if msg.Type == tea.KeyEnter && msg.Alt {
		if m.loading || m.executor == nil {
			return m, nil
		}
		req := m.activePaneRequest()
		return m.startRequest(req)
	}

	// Dead code removed: inline field routing (urlField/bodyField/headersField)
	// above handles all active-field keys. Remaining actions (u/m/M/s/b/h) are
	// dispatched via the resolver through handleRequestAction when focus is on
	// the request pane and no field is active.
	return m, nil
}

// triggerCurlImport detects a curl command pasted into the URL field and
// opens the import preview modal.
func (m Model) triggerCurlImport(raw string) (Model, tea.Cmd) {
	if m.importer == nil {
		return m, nil
	}
	result, err := m.importer.Parse(strings.NewReader(strings.TrimSpace(raw)))
	if err != nil {
		return m, nil
	}
	// Populate request pane with parsed values.
	m.urlInput.SetValue(result.URL)
	m.method = result.Method
	return m.openImport(result)
}

// --- Search modal ---

func (m Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Resolver lookup for overlay mode.
	if action, ok := m.resolver.Resolve(1, 0, msg); ok {
		switch action {
		case "select":
			if m.isCommandPalette() {
				return m.executeSelectedCommand()
			}
			if len(m.searchResults) > 0 && m.searchCursor < len(m.searchResults) {
				hit := m.searchResults[m.searchCursor]
				m.focus = requestPane
				var cmd tea.Cmd
				m, cmd = m.selectRequest(hit.Request)
				m = m.closeSearch()
				return m, cmd
			}
			return m.closeSearch(), nil
		case keybindings.ActionNavigateDown:
			if m.isCommandPalette() {
				if len(m.commands) > 0 && m.searchCursor < len(m.commands)-1 {
					m.searchCursor++
				}
				return m, nil
			}
			if len(m.searchResults) > 0 && m.searchCursor < len(m.searchResults)-1 {
				m.searchCursor++
				m = m.ensureSearchCursorVisible()
			}
			return m, nil
		case keybindings.ActionNavigateUp:
			if m.isCommandPalette() {
				if m.searchCursor > 0 {
					m.searchCursor--
				}
				return m, nil
			}
			m.searchCursor--
			if m.searchCursor < 0 {
				m.searchCursor = 0
			}
			m = m.ensureSearchCursorVisible()
			return m, nil
		case keybindings.ActionCancel:
			return m.closeSearch(), nil
		}
	}

	// Route character input to the search textinput.
	beforeValue := m.searchInput.Value()
	beforeCursor := m.searchInput.Position()
	var inputCmd tea.Cmd
	m.searchInput, inputCmd = m.searchInput.Update(msg)
	if m.debugLog != nil {
		fmt.Fprintf(
			m.debugLog,
			"[%s] search_input key=%q before=%q before_cursor=%d after=%q after_cursor=%d\n",
			time.Now().Format("15:04:05.000"),
			msg.String(),
			beforeValue,
			beforeCursor,
			m.searchInput.Value(),
			m.searchInput.Position(),
		)
	}
	query := m.searchInput.Value()
	if m.isCommandPaletteQuery(query) {
		m.commands = filterCommandPalette(strings.TrimSpace(strings.TrimPrefix(query, ">")))
		m.searchResults = nil
		m.searchCursor = 0
		m.searchScroll = 0
		m.searched = true
		return m, inputCmd
	}
	// dispatchSearch cancels any stale search goroutine before starting a new one.
	var searchM Model
	var searchC tea.Cmd
	searchM, searchC = m.dispatchSearch(query)
	m = searchM
	return m, tea.Batch(inputCmd, searchC)
}

func debugRuneCodes(runes []rune) string {
	if len(runes) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(runes))
	for _, r := range runes {
		parts = append(parts, strconv.Itoa(int(r)))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func (m Model) openHelp() Model {
	m.mode = helpMode
	return m
}

func (m Model) closeHelp() Model {
	m.mode = normalMode
	m.helpCursor = 0
	m.helpScrollOffset = 0
	m.helpEditState = helpViewing
	m.helpEditAction = ""
	m.helpEditErrMsg = ""
	return m
}

func (m Model) openSearch() (Model, tea.Cmd) {
	m.mode = searchMode
	m.searchInput.SetValue("")
	m.searchInput.Focus()
	m.searchResults = nil
	m.commands = nil
	m.searchCursor = 0
	m.searchScroll = 0
	m.searchCancel = nil
	m.searched = false
	return m, textinput.Blink
}

func (m Model) closeSearch() Model {
	if m.searchCancel != nil {
		m.searchCancel()
		m.searchCancel = nil
	}
	m.mode = normalMode
	m.searchInput.Blur()
	m.searchInput.SetValue("")
	m.searchResults = nil
	m.commands = nil
	m.searchCursor = 0
	m.searchScroll = 0
	m.searched = false
	return m
}

func (m Model) openSchedulePrompt() (Model, tea.Cmd) {
	if m.activePaneRequest() == nil || strings.TrimSpace(m.activePaneRequest().ID) == "" {
		return m.status("error", "Select a saved request first"), nil
	}
	m.mode = scheduleMode
	m.scheduleInput.SetValue("")
	m.scheduleInput.Focus()
	return m, textinput.Blink
}

func (m Model) closeSchedulePrompt() Model {
	m.mode = normalMode
	m.scheduleInput.Blur()
	m.scheduleInput.SetValue("")
	return m
}

func (m Model) handleScheduleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if action, ok := m.resolver.Resolve(7, 0, msg); ok {
		switch action {
		case keybindings.ActionConfirm:
			req := m.activePaneRequest()
			if req == nil || req.ID == "" {
				return m.closeSchedulePrompt().status("error", "Select a saved request first"), nil
			}
			runAt, err := schedule.ParseWhen(m.scheduleInput.Value(), m.now())
			if err != nil {
				return m.status("error", err.Error()), nil
			}
			return m.closeSchedulePrompt(), saveScheduledRunCmd(m.ctx, m.scheduler, req.ID, runAt)
		case keybindings.ActionCancel:
			return m.closeSchedulePrompt(), nil
		}
	}
	var cmd tea.Cmd
	m.scheduleInput, cmd = m.scheduleInput.Update(msg)
	return m, cmd
}

func (m Model) applyScheduledRunBackgroundResult(
	msg scheduledRunBackgroundResultMsg,
) (Model, tea.Cmd) {
	m = m.statusScheduledRunResult(msg)
	if m.activeRequest == nil {
		return m, nil
	}
	for _, sent := range msg.sent {
		if sent.requestID != m.activeRequest.ID || sent.result == nil {
			continue
		}
		if m.response != nil {
			_ = m.response.Cleanup()
		}
		m.response = sent.result
		m.responseTab = bodyTab
		m.execCursor = 0
		return m, loadExecutionHistoryCmd(m.ctx, m.executionReader, sent.requestID)
	}
	return m, nil
}

func (m Model) statusScheduledRunResult(msg scheduledRunBackgroundResultMsg) Model {
	switch {
	case len(msg.failed) > 0:
		first := msg.failed[0]
		if first.err == nil {
			return m.status("error", fmt.Sprintf("Scheduled request %q failed", first.name))
		}
		return m.status(
			"error",
			fmt.Sprintf("Scheduled request %q failed: %v", first.name, first.err),
		)
	case len(msg.sent) == 1:
		label := msg.sent[0].label
		if strings.TrimSpace(label) == "" {
			label = msg.sent[0].name
		}
		return m.status("success", fmt.Sprintf("Sent %q in the background", label))
	case len(msg.sent) > 1:
		return m.status(
			"success",
			fmt.Sprintf("Sent %d scheduled requests in the background", len(msg.sent)),
		)
	default:
		return m
	}
}

type commandPaletteItem struct {
	Title  string
	Action string
}

var commandPaletteItems = []commandPaletteItem{
	{Title: "Focus sidebar", Action: keybindings.ActionFocusSidebar},
	{Title: "Focus request", Action: keybindings.ActionFocusRequest},
	{Title: "Focus response", Action: keybindings.ActionFocusResponse},
	{Title: "Send request", Action: keybindings.ActionSendRequest},
	{Title: "Schedule request", Action: keybindings.ActionScheduleRun},
	{Title: "Open help", Action: "help"},
}

func (m Model) isCommandPalette() bool {
	return m.isCommandPaletteQuery(m.searchInput.Value())
}

func (m Model) isCommandPaletteQuery(query string) bool {
	return strings.HasPrefix(strings.TrimSpace(query), ">")
}

func filterCommandPalette(query string) []commandPaletteItem {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return append([]commandPaletteItem(nil), commandPaletteItems...)
	}
	var out []commandPaletteItem
	for _, item := range commandPaletteItems {
		title := strings.ToLower(item.Title)
		action := strings.ReplaceAll(item.Action, "_", " ")
		if strings.Contains(title, query) || strings.Contains(action, query) {
			out = append(out, item)
		}
	}
	return out
}

func (m Model) executeSelectedCommand() (tea.Model, tea.Cmd) {
	if len(m.commands) == 0 || m.searchCursor >= len(m.commands) {
		return m.closeSearch(), nil
	}
	action := m.commands[m.searchCursor].Action
	m = m.closeSearch()
	switch action {
	case "help":
		return m.openHelp(), nil
	case keybindings.ActionSendRequest:
		m.focus = requestPane
		return m.handleRequestAction(action)
	case keybindings.ActionScheduleRun:
		m.focus = requestPane
		return m.handleRequestAction(action)
	case keybindings.ActionFocusSidebar,
		keybindings.ActionFocusRequest,
		keybindings.ActionFocusResponse:
		return m.dispatchNormalAction(action)
	}
	return m, nil
}

func (m Model) openImport(preview *curl.ImportResult) (Model, tea.Cmd) {
	m.importPreview = preview
	m.importColID = m.activeCollectionID()
	m.mode = importMode
	m.importName.Focus()
	return m, textinput.Blink
}

func (m Model) closeImport() Model {
	m.mode = normalMode
	m.importPreview = nil
	m.importName.Blur()
	m.activeField = noneField
	m.urlInput.Blur()
	return m
}

func (m Model) ensureSidebarCollectionVisible() Model {
	rows, selectedRow := m.buildSidebarRows()
	m.sidebarOffset = adjustListViewport(listViewport{
		Scroll:      m.sidebarOffset,
		SelectedRow: selectedRow,
		TotalRows:   len(rows),
		VisibleRows: m.sidebarVisible(),
	})
	return m
}

const (
	helpModalMarginLines      = 4
	helpMinimumBoxHeight      = 8
	helpHeaderLines           = 2
	helpInstructionLines      = 2
	helpFooterLines           = 2
	helpBorderAndPaddingLines = 4
	helpRecordingHintLines    = 2
	helpErrorHintLines        = 2
	helpMinimumVisibleRows    = 3
)

func (m Model) helpMaxLines() int {
	boxHeight := m.height - helpModalMarginLines
	if boxHeight < helpMinimumBoxHeight {
		boxHeight = helpMinimumBoxHeight
	}
	// Reserve vertical budget for the title, instructions, footer, and modal chrome.
	overhead := helpHeaderLines + helpInstructionLines + helpFooterLines + helpBorderAndPaddingLines
	if m.helpEditState == helpRecording {
		overhead += helpRecordingHintLines
	}
	if m.helpEditState == helpError {
		overhead += helpErrorHintLines
	}
	maxLines := boxHeight - overhead
	if maxLines < helpMinimumVisibleRows {
		maxLines = helpMinimumVisibleRows
	}
	return maxLines
}

func (m Model) adjustHelpScroll(entries []keybindings.Entry, direction int) Model {
	rows, selectedRow := buildHelpRows(entries, m.helpCursor)
	if len(rows) == 0 {
		m.helpCursor = 0
		m.helpScrollOffset = 0
		return m
	}

	if m.helpCursor < 0 {
		m.helpCursor = 0
	}
	if m.helpCursor >= len(entries) {
		m.helpCursor = len(entries) - 1
	}
	if m.helpScrollOffset < 0 {
		m.helpScrollOffset = 0
	}
	maxLines := m.helpMaxLines()
	pad := max(1, maxLines/4)
	if pad*2 >= maxLines {
		pad = 1
	}
	m.helpScrollOffset = adjustListViewport(listViewport{
		Scroll:      m.helpScrollOffset,
		SelectedRow: selectedRow,
		TotalRows:   len(rows),
		VisibleRows: maxLines,
		Direction:   direction,
		LeadingPad:  pad,
		TrailingPad: pad,
	})
	return m
}

// --- Help overlay ---

func (m Model) handleHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle recording state first — capture the next keypress.
	if m.helpEditState == helpRecording {
		switch msg.String() {
		case m.cfg.Keybindings.HelpClose:
			m.helpEditState = helpViewing
			m.helpEditAction = ""
			return m, nil
		case m.cfg.Keybindings.HelpUnbind:
			msg = tea.KeyMsg{}
		}

		key := msg.String()
		// Apply change.
		newBinds, err := keybindings.RecordBinding(m.cfg.Keybindings, m.helpEditAction, key)
		if err != nil {
			m.helpEditState = helpError
			m.helpEditErrMsg = err.Error()
			return m, nil
		}

		// Persist.
		if err := config.SaveKeybindings(m.configDir, newBinds); err != nil {
			m.helpEditState = helpError
			m.helpEditErrMsg = fmt.Sprintf("save failed: %v", err)
			return m, nil
		}

		// Update config in model.
		m.cfg.Keybindings = newBinds
		// Rebuild resolver.
		m.resolver = keybindings.NewResolver(newBinds)
		action := m.helpEditAction
		m.helpEditState = helpViewing
		m.helpEditAction = ""
		if key == "" {
			m = m.status("success", fmt.Sprintf("Unbound %s", action))
		} else {
			m = m.status("success", fmt.Sprintf("Bound %s -> %s", action, key))
		}
		return m, nil
	}

	if m.helpEditState == helpConfirmResetAll {
		if msg.Type == tea.KeyEnter {
			defaults := keybindings.DefaultKeybindings()
			if err := config.SaveKeybindings(m.configDir, defaults); err != nil {
				m.helpEditState = helpError
				m.helpEditErrMsg = fmt.Sprintf("save failed: %v", err)
				return m, nil
			}
			m.cfg.Keybindings = defaults
			m.resolver = keybindings.NewResolver(defaults)
			m.helpEditState = helpViewing
			m.helpCursor = 0
			m.helpScrollOffset = 0
			m = m.status("success", "Reset all keybindings to defaults")
			return m, nil
		}
		if action, ok := m.resolver.Resolve(2, 0, msg); ok && action == keybindings.ActionClose {
			m.helpEditState = helpViewing
			return m, nil
		}
		return m, nil
	}

	// Error state — any key dismisses.
	if m.helpEditState == helpError {
		m.helpEditState = helpViewing
		m.helpEditErrMsg = ""
		return m, nil
	}

	// Viewing state — navigate, edit, reset, or close.
	entries := keybindings.ListEntries(m.cfg.Keybindings)

	if action, ok := m.resolver.Resolve(2, 0, msg); ok {
		switch action {
		case "quit":
			return m, tea.Quit
		case keybindings.ActionClose:
			m.mode = normalMode
			m.helpCursor = 0
			m.helpScrollOffset = 0
			return m, nil
		case keybindings.ActionNavigateUp:
			if m.helpCursor > 0 {
				m.helpCursor--
			}
			return m.adjustHelpScroll(entries, -1), nil
		case keybindings.ActionNavigateDown:
			if m.helpCursor < len(entries)-1 {
				m.helpCursor++
			}
			return m.adjustHelpScroll(entries, 1), nil
		case "edit":
			if m.helpCursor >= 0 && m.helpCursor < len(entries) {
				m.helpEditState = helpRecording
				m.helpEditAction = entries[m.helpCursor].Action
				m.helpEditErrMsg = ""
			}
			return m, nil
		case "reset":
			if m.helpCursor >= 0 && m.helpCursor < len(entries) {
				action := entries[m.helpCursor].Action
				defaults := keybindings.DefaultKeybindings()
				key := keybindings.GetAction(defaults, action)
				newBinds, err := keybindings.RecordBinding(m.cfg.Keybindings, action, key)
				if err != nil {
					m.helpEditState = helpError
					m.helpEditErrMsg = err.Error()
					return m, nil
				}
				if err := config.SaveKeybindings(m.configDir, newBinds); err != nil {
					m.helpEditState = helpError
					m.helpEditErrMsg = fmt.Sprintf("save failed: %v", err)
					return m, nil
				}
				m.cfg.Keybindings = newBinds
				m.resolver = keybindings.NewResolver(newBinds)
				m = m.status("success", fmt.Sprintf("Reset %s to %s", action, key))
			}
			return m, nil
		case "reset_all":
			if len(helpResetAllDiffs(m.cfg.Keybindings)) == 0 {
				m = m.status("success", "All keybindings already match defaults")
				return m, nil
			}
			m.helpEditState = helpConfirmResetAll
			return m, nil
		}
	}

	return m, nil
}

func helpResetAllDiffs(current keybindings.Keybindings) []string {
	defaults := keybindings.DefaultKeybindings()
	currentEntries := keybindings.ListEntries(current)
	diffs := make([]string, 0)
	for _, entry := range currentEntries {
		defaultKey := keybindings.GetAction(defaults, entry.Action)
		if entry.Key == defaultKey {
			continue
		}

		currentKey := "(unbound)"
		if entry.Key != "" {
			currentKey = keybindings.FormatKey(entry.Key)
		}
		defaultLabel := "(unbound)"
		if defaultKey != "" {
			defaultLabel = keybindings.FormatKey(defaultKey)
		}
		diffs = append(
			diffs,
			fmt.Sprintf("%s: %s -> %s", helpActionLabel(entry.Action), currentKey, defaultLabel),
		)
	}
	return diffs
}

// --- Import modal ---

func (m Model) handleImportKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Resolver lookup for overlay mode.
	if action, ok := m.resolver.Resolve(3, 0, msg); ok {
		switch action {
		case keybindings.ActionConfirm:
			var saveCmd tea.Cmd
			if m.importPreview != nil && m.writer != nil {
				name := strings.TrimSpace(m.importName.Value())
				if name == "" {
					name = m.importPreview.URL
				}
				req := &domain.Request{
					CollectionID: m.importColID,
					Name:         name,
					Method:       m.importPreview.Method,
					URL:          m.importPreview.URL,
				}
				saveCmd = saveRequestCmd(m.ctx, m.writer, m.reader, req)
			}
			return m.closeImport(), saveCmd
		case keybindings.ActionCancel:
			return m.closeImport(), nil
		}
	}

	var cmd tea.Cmd
	m.importName, cmd = m.importName.Update(msg)
	return m, cmd
}

// --- HTTP response/error handlers ---

func (m Model) handleHTTPResponse(msg httpResponseMsg) (tea.Model, tea.Cmd) {
	if msg.requestID != "" && m.activeRequest != nil && m.activeRequest.ID != msg.requestID {
		m.loading = false
		m.cancel = nil
		return m, nil
	}
	m.loading = false
	m.cancel = nil
	m.err = nil
	m = m.clearStatus()
	m = m.clearRequestValidationErr(msg.requestID)
	if m.response != nil {
		_ = m.response.Cleanup() // release any temp file from previous response
	}
	m.response = msg.result
	m.responseTab = bodyTab
	m.focus = responsePane
	if msg.requestID == "" {
		return m, nil
	}
	return m, loadExecutionHistoryCmd(m.ctx, m.executionReader, msg.requestID)
}

func (m Model) handleHTTPErr(msg httpErrMsg) (tea.Model, tea.Cmd) {
	if msg.requestID != "" && (m.activeRequest == nil || m.activeRequest.ID != msg.requestID) {
		m.loading = false
		m.cancel = nil
		if errors.Is(msg.err, exec.ErrInvalidURL) {
			m = m.setRequestValidationErr(msg.requestID, msg.err.Error())
		}
		return m, nil
	}
	m.loading = false
	m.cancel = nil
	m = m.clearStatus()
	historyCmd := tea.Cmd(nil)
	if msg.requestID != "" {
		historyCmd = loadExecutionHistoryCmd(m.ctx, m.executionReader, msg.requestID)
	}

	if errors.Is(msg.err, exec.ErrInvalidURL) {
		requestID := msg.requestID
		if requestID == "" && m.activeRequest != nil {
			requestID = m.activeRequest.ID
		}
		m = m.setRequestValidationErr(requestID, msg.err.Error())
		return m, historyCmd
	}
	if errors.Is(msg.err, exec.ErrRequestCancelled) {
		return m, historyCmd
	}
	if errors.Is(msg.err, exec.ErrTimeout) {
		m = m.status("error", "Request timed out. Press [⌘+Enter] to retry.")
		return m, historyCmd
	}
	m.err = fmt.Errorf("unexpected error: %w", msg.err)
	return m, historyCmd
}

// --- Inline body / header field handlers ---

func (m Model) handleBodyFieldKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Resolver lookup for body editor mode.
	if action, ok := m.resolver.Resolve(4, 0, msg); ok {
		switch action {
		case "save":
			return m.saveBody()
		case "insert_newline":
			m.bodyTextarea.InsertString("\n")
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.bodyTextarea, cmd = m.bodyTextarea.Update(msg)
	return m, cmd
}

func (m Model) saveBody() (Model, tea.Cmd) {
	if m.activeRequest == nil {
		return m, nil
	}
	m = m.finishBodyEdit()
	return m, saveRequestCmd(m.ctx, m.writer, m.reader, m.activeRequest)
}

func (m Model) handleAuthFieldKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.authEditor.editing {
		return m.handleAuthFieldEdit(msg)
	}

	if action, ok := m.resolver.Resolve(8, 0, msg); ok {
		switch action {
		case "save":
			return m.saveAuth()
		case keybindings.ActionNavigateDown:
			m.authEditor.cursor++
			m.authEditor.clampCursor()
			return m, nil
		case keybindings.ActionNavigateUp:
			m.authEditor.cursor--
			m.authEditor.clampCursor()
			return m, nil
		case "edit":
			switch m.authEditor.currentRow() {
			case authRowType:
				m.authEditor.cycleType(1)
			case authRowAPIKeyIn:
				m.authEditor.cycleAPIKeyIn(1)
			default:
				m.authEditor.beginEdit()
				if m.authEditor.editing {
					return m, textinput.Blink
				}
			}
			return m, nil
		case "option_next":
			switch m.authEditor.currentRow() {
			case authRowType:
				m.authEditor.cycleType(1)
			case authRowAPIKeyIn:
				m.authEditor.cycleAPIKeyIn(1)
			}
			return m, nil
		case "option_prev":
			switch m.authEditor.currentRow() {
			case authRowType:
				m.authEditor.cycleType(-1)
			case authRowAPIKeyIn:
				m.authEditor.cycleAPIKeyIn(-1)
			}
			return m, nil
		}
	}
	return m, nil
}

func (m Model) handleAuthFieldEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyTab {
		return m, nil
	}
	if msg.Type == tea.KeyEnter {
		m.authEditor.endEdit()
		return m, nil
	}
	var cmd tea.Cmd
	switch m.authEditor.currentRow() {
	case authRowToken:
		m.authEditor.tokenInput, cmd = m.authEditor.tokenInput.Update(msg)
	case authRowUsername:
		m.authEditor.usernameInput, cmd = m.authEditor.usernameInput.Update(msg)
	case authRowPassword:
		m.authEditor.passwordInput, cmd = m.authEditor.passwordInput.Update(msg)
	case authRowAPIKeyName:
		m.authEditor.apiKeyNameInput, cmd = m.authEditor.apiKeyNameInput.Update(msg)
	case authRowAPIKeyValue:
		m.authEditor.apiKeyValueInput, cmd = m.authEditor.apiKeyValueInput.Update(msg)
	}
	return m, cmd
}

func (m Model) saveAuth() (Model, tea.Cmd) {
	if m.activeRequest == nil {
		return m, nil
	}
	m = m.finishAuthEdit()
	return m, saveRequestCmd(m.ctx, m.writer, m.reader, m.activeRequest)
}

func (m Model) handleHeadersFieldKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Editing sub-mode takes precedence over the resolver. When editing a
	// single pair, keys go directly to the textinputs (Enter confirms, Tab
	// switches field, etc.) so the resolver can't intercept them.
	if m.headerEditing {
		return m.handleHeaderFieldEdit(msg)
	}

	// Resolver lookup for header editor mode (list mode only).
	if action, ok := m.resolver.Resolve(5, 0, msg); ok {
		switch action {
		case "save":
			return m.saveHeaders()
		case keybindings.ActionNavigateDown:
			if m.headerCursor < len(m.headerPairs)-1 {
				m.headerCursor++
			}
			return m, nil
		case keybindings.ActionNavigateUp:
			if m.headerCursor > 0 {
				m.headerCursor--
			}
			return m, nil
		case "add_pair":
			m.headerPairs = append(m.headerPairs, headerPair{})
			m.headerCursor = len(m.headerPairs) - 1
			return m.beginHeaderPairEdit(headerPair{})
		case "delete_pair":
			if len(m.headerPairs) > 0 && m.headerCursor < len(m.headerPairs) {
				m.headerPairs = append(
					m.headerPairs[:m.headerCursor], m.headerPairs[m.headerCursor+1:]...,
				)
				if m.headerCursor >= len(m.headerPairs) && m.headerCursor > 0 {
					m.headerCursor--
				}
			}
			return m, nil
		case "edit_pair":
			if len(m.headerPairs) > 0 && m.headerCursor < len(m.headerPairs) {
				return m.beginHeaderPairEdit(m.headerPairs[m.headerCursor])
			}
			return m, nil
		}
	}
	return m, nil
}

func (m Model) handleHeaderFieldEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyTab {
		if m.headerKeyInput.Focused() {
			m.headerKeyInput.Blur()
			m.headerValueInput.Focus()
		} else {
			m.headerValueInput.Blur()
			m.headerKeyInput.Focus()
		}
		return m, nil
	}
	if msg.Type == tea.KeyEnter {
		return m.finishHeaderPairEdit(), nil
	}
	var cmd tea.Cmd
	if m.headerKeyInput.Focused() {
		m.headerKeyInput, cmd = m.headerKeyInput.Update(msg)
	} else {
		m.headerValueInput, cmd = m.headerValueInput.Update(msg)
	}
	return m, cmd
}

func (m Model) saveHeaders() (Model, tea.Cmd) {
	if m.activeRequest == nil {
		return m, nil
	}
	headersMap := make(map[string]string, len(m.headerPairs))
	for _, p := range m.headerPairs {
		if p.Key != "" {
			headersMap[p.Key] = p.Value
		}
	}
	headersJSON, err := json.Marshal(headersMap)
	if err != nil {
		m = m.status("error", "Failed to marshal headers: "+err.Error())
		return m, nil
	}
	m.activeRequest.Headers = string(headersJSON)
	m.headerPairs = nil
	m.activeField = noneField
	m = m.status("success", "Headers updated")
	return m, saveRequestCmd(m.ctx, m.writer, m.reader, m.activeRequest)
}

// parseHeadersJSON parses a JSON object string into a slice of headerPair.
// Returns empty slice on invalid JSON or empty input.
func parseHeadersJSON(raw string) []headerPair {
	if raw == "" || raw == "{}" {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil
	}
	pairs := make([]headerPair, 0, len(m))
	for k, v := range m {
		pairs = append(pairs, headerPair{Key: k, Value: v})
	}
	return pairs
}

// enterCollectionPrompt switches to collectionPromptMode with the given type and target.
func (m Model) enterCollectionPrompt(pt promptType, targetID string) (tea.Model, tea.Cmd) {
	m.mode = collectionPromptMode
	m.promptMode = pt
	m.promptTargetID = targetID
	m.promptInput.SetValue("")
	// Clear stale status from prior actions (e.g. 'a' with no collection) so it
	// does not appear inside the new prompt overlay.
	m.statusErr = ""
	m.statusSuccess = ""

	switch pt {
	case promptAdd:
		m.promptInput.Placeholder = "Collection name"
	case promptAddRequest:
		m.promptInput.Placeholder = "Request name"
	case promptRename:
		m.promptInput.Placeholder = "New name"
	case promptDeleteConfirm:
		m.promptInput.Placeholder = "Type 'yes' to confirm"
	case promptDeleteTiny:
		m.promptInput.Placeholder = "Press (d) again to confirm"
	}

	m.promptInput.Focus()
	return m, textinput.Blink
}

func (m Model) closeCollectionPrompt() Model {
	m.mode = normalMode
	m.promptMode = promptNone
	m.promptTargetID = ""
	m.promptInput.SetValue("")
	m.promptInput.Blur()
	return m
}

func wrapPromptSuccess(cmd tea.Cmd) tea.Cmd {
	return func() tea.Msg {
		if cmd == nil {
			return nil
		}
		msg := cmd()
		switch msg.(type) {
		case collectionSavedMsg, envCreatedMsg, requestsLoadedMsg:
			return promptCompletedMsg{inner: msg}
		default:
			return msg
		}
	}
}

// handleCollectionPromptKey handles key input in the collection prompt overlay.
func (m Model) handleCollectionPromptKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Tiny prompt: pressing 'd' again in the tiny delete confirmation immediately
	// deletes the collection without requiring Enter or a text input.
	if m.promptMode == promptDeleteTiny && msg.String() == m.cfg.Keybindings.SidebarDelete {
		if m.colWriter == nil {
			m = m.status("error", "Collection writer not available")
			m = m.closeCollectionPrompt()
			return m, nil
		}
		return m, deleteCollectionCmd(m.ctx, m.colWriter, m.promptTargetID)
	}

	if action, ok := m.resolver.Resolve(7, 0, msg); ok {
		switch action {
		case keybindings.ActionCancel:
			return m.closeCollectionPrompt(), nil
		case keybindings.ActionConfirm:
			val := strings.TrimSpace(m.promptInput.Value())

			switch m.promptMode {
			case promptAdd:
				if val == "" {
					m = m.status("error", "Collection name cannot be empty")
					return m, nil
				}
				if m.colWriter == nil {
					m = m.status("error", "Collection writer not available")
					return m, nil
				}
				c := &domain.Collection{Name: val}
				return m, wrapPromptSuccess(saveCollectionCmd(m.ctx, m.colWriter, c))

			case promptRename:
				if val == "" {
					m = m.status("error", "Collection name cannot be empty")
					return m, nil
				}
				if m.colWriter == nil {
					m = m.status("error", "Collection writer not available")
					return m, nil
				}
				c := &domain.Collection{ID: m.promptTargetID, Name: val}
				return m, wrapPromptSuccess(saveCollectionCmd(m.ctx, m.colWriter, c))

			case promptDeleteConfirm:
				if val != "yes" {
					m = m.status("error", "Delete cancelled")
					return m.closeCollectionPrompt(), nil
				}
				if m.colWriter == nil {
					m = m.status("error", "Collection writer not available")
					return m, nil
				}
				return m, wrapPromptSuccess(
					deleteCollectionCmd(m.ctx, m.colWriter, m.promptTargetID),
				)

			case promptDeleteTiny:
				// Handled in the outer switch by checking msg.String() == "d".
				// This case is unreachable from Enter but kept for completeness.
				return m, nil

			case promptAddRequest:
				if val == "" {
					m = m.status("error", "Request name cannot be empty")
					return m, nil
				}
				if m.writer == nil {
					m = m.status("error", "Request writer not available")
					return m, nil
				}
				req := &domain.Request{
					CollectionID: m.promptTargetID,
					Name:         val,
					Method:       m.method,
					URL:          "",
					Headers:      "{}",
					Body:         "",
				}
				return m, wrapPromptSuccess(saveRequestCmd(m.ctx, m.writer, m.reader, req))

			case promptAddEnv:
				if val == "" {
					m = m.status("error", "Environment name cannot be empty")
					return m, nil
				}
				if val == defaultEnvironmentName {
					m = m.status("error", "'default' is a reserved name")
					return m, nil
				}
				if m.envWriter == nil {
					m = m.status("error", "Environment writer not available")
					return m, nil
				}
				env := &domain.Environment{
					ID:           uuid.New().String(),
					CollectionID: m.promptTargetID,
					Name:         val,
					Data:         "{}",
				}
				return m, wrapPromptSuccess(
					saveEnvAndReloadCmd(m.ctx, m.envWriter, env),
				)
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.promptInput, cmd = m.promptInput.Update(msg)
	return m, cmd
}

// saveEnvAndReloadCmd saves an environment and returns a message that triggers a reload.
func saveEnvAndReloadCmd(
	ctx context.Context,
	w store.EnvironmentWriter,
	env *domain.Environment,
) tea.Cmd {
	return func() tea.Msg {
		if err := w.SaveEnvironment(ctx, env); err != nil {
			return collectionSavedErrMsg{err: err}
		}
		// Return success — the caller will reload envs on success.
		return envCreatedMsg{}
	}
}

// saveCollectionCmd wraps a SaveCollection call in a tea.Cmd.
func saveCollectionCmd(
	ctx context.Context,
	w store.CollectionWriter,
	c *domain.Collection,
) tea.Cmd {
	return func() tea.Msg {
		if err := w.SaveCollection(ctx, c); err != nil {
			return collectionSavedErrMsg{err: err}
		}
		return collectionSavedMsg{}
	}
}

// deleteCollectionCmd wraps a DeleteCollection call in a tea.Cmd.
func deleteCollectionCmd(ctx context.Context, w store.CollectionWriter, id string) tea.Cmd {
	return func() tea.Msg {
		if err := w.DeleteCollection(ctx, id); err != nil {
			return collectionSavedErrMsg{err: err}
		}
		return collectionSavedMsg{}
	}
}
