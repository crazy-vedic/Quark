package tui

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/keybindings"
	"github.com/crazy-vedic/quark/internal/search"
	"github.com/crazy-vedic/quark/internal/store"
)

func interactionModel() Model { return New(Deps{Config: config.Default("")}) }

func TestUpdateAndViewHelpers(t *testing.T) {
	m := interactionModel()
	m.debugCurl("curl %s", "https://example.test")
	require.NotEmpty(t, debugHeaderKeys(http.Header{
		"Z-Last":  []string{"1"},
		"A-First": []string{"2"},
	}))
	require.Equal(t, "[65,128640]", debugRuneCodes([]rune{'A', '🚀'}))
	require.Equal(t, "[]", debugRuneCodes(nil))
	m.mode = helpMode
	require.Equal(t, normalMode, m.closeHelp().mode)
	require.Equal(t, "\n  invalid request \r\n", cleanError("\n  invalid request \r\n"))
	require.Equal(t, "  \n ", cleanError("  \n "))
	require.True(t, isBinaryBody([]byte{0, 1, 2}))
	require.False(t, isBinaryBody([]byte("plain")))
	require.Equal(
		t,
		[]string{"Content-Type", "A-Test", "z-test"},
		sortedHeaderKeys(http.Header{"z-test": nil, "Content-Type": nil, "A-Test": nil}),
	)
	require.Equal(t, 0, headerPriority("content-type"))
	require.Less(t, headerPriority("content-type"), headerPriority("x-custom"))
	firstTime := time.Date(2025, 1, 1, 1, 0, 0, 0, time.UTC)
	secondTime := time.Date(2025, 1, 1, 23, 0, 0, 0, time.UTC)
	require.True(t, sameLocalDay(firstTime, secondTime))
	firstTime = time.Date(2025, 1, 1, 1, 0, 0, 0, time.UTC)
	secondTime = time.Date(2025, 1, 2, 1, 0, 0, 0, time.UTC)
	require.False(t, sameLocalDay(firstTime, secondTime))
	require.Equal(t, 2, lineVisualRows("a\nb", 20))
	require.Equal(t, 2, visualRows("a\nb", 20))
	require.Equal(t, "", clipToRows("a", 1, 0))
	require.Equal(t, "", truncate("abc", 0))
	require.False(t, isCredentialHeader("X-Request-ID"))
	require.True(t, isCredentialHeader("Authorization"))
	require.Equal(t, []string{"a", "b"}, dedupeHintKeys([]string{"a", "a", "b"}))
	require.Equal(t, "quit", helpActionLabel("quit"))
	searchMatch := highlightSearchMatch(
		"Payment request", "pay", lipgloss.NewStyle(), lipgloss.NewStyle(),
	)
	require.NotEmpty(t, searchMatch)
	keys := []string{"b", "a", "b"}
	sortHeaderKeys(keys)
	require.Equal(t, []string{"a", "b", "b"}, keys)
}

func TestModelAndLayoutBranches(t *testing.T) {
	m := interactionModel()
	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "TRACE", "OTHER"} {
		next := nextMethod(method)
		prev := prevMethod(method)
		require.NotEmpty(t, next)
		require.NotEmpty(t, prev)
	}
	m.collections = []*domain.Collection{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}}
	m.colCursor = 9
	require.Nil(t, m.selectedCollection())
	m.colCursor = 0
	require.Equal(t, "a", m.selectedCollection().ID)
	require.Equal(t, []string{"a", "b"}, m.allCollectionIDs())
	m.forceDim = DimAuto
	m.width = 100
	m.height = 30
	require.Equal(t, DimWide, m.effectiveDim())
	require.Equal(t, DimWide, m.applyDimSticky().stickyDim)
	m.width = 0
	m.height = 0
	require.Equal(t, DimAbsurd, m.effectiveDim())
	m = interactionModel().WithActiveRequest(&domain.Request{ID: "r", Method: "GET", URL: "https://example.test", Body: "one\ntwo"})
	m.width = 80
	m.height = 16
	require.NotEmpty(t, m.currentLayout().mode.String())
	_ = m.resizeBodyTextarea()
	for _, mode := range []DimMode{DimWide, DimNarrow, DimTiny, DimAbsurd, DimAuto, DimMode(99)} {
		l := layoutFor(70, 20, mode, sidebarPane)
		require.GreaterOrEqual(t, l.width, 0)
		_, _ = l.paneAt(-1, -1)
	}
	require.True(t, normalLayoutFor(0, 0).mode == DimAbsurd)
}

func TestMouseNoOpAndHitTestBranches(t *testing.T) {
	m := interactionModel()
	for _, msg := range []tea.MouseMsg{
		{Shift: true},
		{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft},
		{Button: tea.MouseButtonWheelLeft},
		{X: 999, Y: 999, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft},
	} {
		_, _ = m.handleMouse(msg)
	}
	m.mode = viewerMode
	m.width = 0
	m.height = 0
	for _, msg := range []tea.MouseMsg{{Shift: true}, {Y: 0, Button: tea.MouseButtonWheelDown}, {Y: 99, Button: tea.MouseButtonWheelUp}} {
		m = m.handleViewerMouse(msg)
	}
	m.mode = normalMode
	m.width = 100
	m.height = 30
	_, _ = m.handleSidebarClick(tea.MouseMsg{X: 999, Y: 999})
	_, _ = m.handleResponseClick(tea.MouseMsg{X: 999, Y: 999})
	_, _ = m.handleSidebarWheel(tea.MouseMsg{Button: tea.MouseButtonWheelLeft})
	require.False(t, isMouseWheel(tea.MouseMsg{Button: tea.MouseButtonLeft}))
	require.True(t, isMouseWheel(tea.MouseMsg{Button: tea.MouseButtonWheelDown}))
	_, _ = m.sidebarListHitAt(0, 0)
	_, _, _ = m.sidebarTreeRowScreenPos(0)
	_, _ = m.sidebarRowIndexForCollection(99)
	_, _ = m.sidebarRowIndexForRequest(99, 99)
	_, _ = m.responseHistoryHitAt(0, 0, m.currentLayout())
}

func TestCommandAndEnvironmentBranches(t *testing.T) {
	m := interactionModel()
	m.mode = searchMode
	m.searchInput.SetValue(">unknown")
	require.True(t, m.isCommandPalette())
	_ = m.filterCommandPalette("unmatched")
	m.commands = nil
	_, _ = m.executeSelectedCommand()
	m.mode = normalMode
	_, _ = m.openSearch()
	m = interactionModel().WithMode(helpMode)
	m = m.closeHelp()
	require.Equal(t, normalMode, m.mode)
	_, _ = m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	_, _ = m.handleHelpKey(tea.KeyMsg{Type: tea.KeyEsc})
	require.Empty(t, helpResetAllDiffs(keybindings.DefaultKeybindings()))
	require.Empty(t, parseHeadersJSON("not-json"))
	require.NotEmpty(t, parseHeadersJSON(`{"X":"one","Y":"two"}`))
	_, _ = m.handleCollectionPromptKey(tea.KeyMsg{Type: tea.KeyEsc})
	_, _ = m.handleRequestKey("", tea.KeyMsg{Type: tea.KeyEsc})
	_, _ = m.handleSearchKey(tea.KeyMsg{Type: tea.KeyEsc})
	_, _ = m.handleScheduleKey(tea.KeyMsg{Type: tea.KeyEsc})
	_, _ = m.handleImportKey(tea.KeyMsg{Type: tea.KeyEsc})
	_, _ = m.handleBodyFieldKey(tea.KeyMsg{Type: tea.KeyEsc})
	_, _ = m.handleAuthFieldKey(tea.KeyMsg{Type: tea.KeyEsc})
	_, _ = m.handleHeadersFieldKey(tea.KeyMsg{Type: tea.KeyEsc})
	require.NotContains(t, strings.ToLower(m.View()), "panic")
}

type failingRequestWriter struct {
	store.RequestWriter
	err error
}

func (w failingRequestWriter) SaveRequest(ctx context.Context, req *domain.Request) error {
	if w.err != nil {
		return w.err
	}
	return w.RequestWriter.SaveRequest(ctx, req)
}
func (w failingRequestWriter) DeleteRequest(ctx context.Context, id string) error {
	if w.err != nil {
		return w.err
	}
	return w.RequestWriter.DeleteRequest(ctx, id)
}

type fakeSearcher struct {
	hits []*search.SearchHit
	err  error
}

func (s fakeSearcher) Search(context.Context, string, string) (*search.SearchResult, error) {
	return &search.SearchResult{Hits: s.hits}, s.err
}

type fakeExecutor struct{ err error }

func (e fakeExecutor) Execute(context.Context, *domain.Request) (*exec.ExecuteResult, error) {
	if e.err != nil {
		return nil, e.err
	}
	return &exec.ExecuteResult{Status: "200 OK"}, nil
}

func TestModelCommands(t *testing.T) {
	ctx := context.Background()
	st, err := store.New(t.TempDir() + "/interaction.db")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	col := &domain.Collection{ID: "interaction-col", Name: "Interaction"}
	require.NoError(t, st.SaveCollection(ctx, col))
	req := &domain.Request{
		ID: "interaction-req", CollectionID: col.ID, Name: "Get", Method: "GET",
		URL: "https://example.test",
	}
	require.NoError(t, st.SaveRequest(ctx, req))
	require.IsType(t, collectionsLoadedMsg{}, loadCollectionsCmd(ctx, st)())
	require.IsType(t, requestsLoadedMsg{}, loadRequestsCmd(ctx, st, col.ID)())
	msg := loadExecutionHistoryCmd(ctx, nil, "")()
	require.Equal(t, executionHistoryLoadedMsg{requestID: ""}, msg)
	require.IsType(t, executionHistoryLoadedMsg{}, loadExecutionHistoryCmd(ctx, st, req.ID)())
	require.IsType(t, envLoadedMsg{}, loadActiveEnvCmd(ctx, st, col.ID)())
	require.IsType(t, searchResultsMsg{}, searchCmd(ctx, nil, col.ID, "q")())
	require.IsType(t, searchResultsMsg{}, searchAllCmd(ctx, nil, []string{col.ID}, "q")())
	require.IsType(t, searchResultsMsg{}, searchCmd(ctx, fakeSearcher{}, col.ID, "q")())
	allSearchResult := searchAllCmd(ctx, fakeAllSearcher{}, []string{col.ID}, "q")()
	require.IsType(t, searchResultsMsg{}, allSearchResult)
	require.IsType(t, requestsLoadedMsg{}, saveRequestCmd(ctx, st, st, req)())
	rolled := false
	failingWriter := failingRequestWriter{RequestWriter: st, err: errors.New("write")}
	rollback := func() { rolled = true }
	msg = saveRequestCmdWithRollback(ctx, failingWriter, st, req, rollback)()
	require.IsType(t, errLoadMsg{}, msg)
	require.True(t, rolled)
	require.IsType(t, httpResponseMsg{}, dispatchCmd(ctx, fakeExecutor{}, req)())
	require.IsType(t, httpErrMsg{}, dispatchCmd(ctx, fakeExecutor{err: errors.New("exec")}, req)())
	require.IsType(t, scheduledRunSaveErrMsg{}, saveScheduledRunCmd(ctx, nil, req.ID, time.Now())())
	require.IsType(t, scheduledRunSavedMsg{}, saveScheduledRunCmd(ctx, st, req.ID, time.Now())())
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	require.Nil(t, scheduleWakeAfterCmd(cancelled, time.Second, 1)())
	require.Nil(t, scheduleNextWakeCmd(ctx, nil, nil, time.Now, 1)())
	require.Equal(t, "scheduled request", scheduledRunName(ctx, nil, nil))
	require.Equal(t, req.Name, scheduledRunName(ctx, st, &domain.ScheduledRun{RequestID: req.ID}))
	require.Equal(t, "Interaction / Get", scheduledRunSuccessLabel(ctx, st, req))
	dueRunsCmd := executeDueScheduledRunsCmd(ctx, nil, nil, nil, nil, nil, nil, nil, time.Now)
	require.NotNil(t, dueRunsCmd())
	_, cmd := interactionModel().armScheduleTimer()
	require.Nil(t, cmd)
}

type fakeAllSearcher struct{}

func (fakeAllSearcher) SearchAll(context.Context, string, []string) (*search.SearchResult, error) {
	return &search.SearchResult{}, nil
}

type failingAllSearcher struct{}

func (failingAllSearcher) SearchAll(
	context.Context, string, []string,
) (*search.SearchResult, error) {
	return nil, errors.New("search all")
}

type failingCollectionLister struct{}

func (failingCollectionLister) ListCollections(context.Context) ([]*domain.Collection, error) {
	return nil, errors.New("collections")
}

type failingRequestReader struct{ store.RequestReader }

func (f failingRequestReader) ListRequests(context.Context, string) ([]*domain.Request, error) {
	return nil, errors.New("requests")
}
func (f failingRequestReader) ListExecutionsByRequest(
	context.Context, string,
) ([]*domain.Execution, error) {
	return nil, errors.New("history")
}

type failingActiveEnvironmentStore struct{}

func (failingActiveEnvironmentStore) SetActiveEnvironment(context.Context, string, string) error {
	return errors.New("active environment")
}
func (failingActiveEnvironmentStore) GetActiveEnvironment(context.Context, string) (string, error) {
	return "", errors.New("active environment")
}

func TestModelCommandFailuresAndSelectionReset(t *testing.T) {
	ctx := context.Background()
	require.IsType(t, errLoadMsg{}, loadCollectionsCmd(ctx, failingCollectionLister{})())
	require.IsType(t, errLoadMsg{}, loadRequestsCmd(ctx, failingRequestReader{}, "collection")())
	failedHistory := loadExecutionHistoryCmd(ctx, failingRequestReader{}, "request")()
	require.IsType(t, errLoadMsg{}, failedHistory)
	failedEnvironment := loadActiveEnvCmd(ctx, failingActiveEnvironmentStore{}, "collection")()
	require.IsType(t, envLoadedMsg{}, failedEnvironment)
	failedSearch := searchCmd(ctx, fakeSearcher{err: errors.New("search")}, "collection", "query")()
	require.IsType(t, errLoadMsg{}, failedSearch)
	failedSearchAll := searchAllCmd(ctx, failingAllSearcher{}, []string{"collection"}, "query")()
	require.IsType(t, errLoadMsg{}, failedSearchAll)
	failedSave := saveRequestCmd(
		ctx,
		failingRequestWriter{err: errors.New("save")},
		nil,
		&domain.Request{},
	)()
	require.IsType(t, errLoadMsg{}, failedSave)

	m := interactionModel()
	m.response = &exec.ExecuteResult{Body: []byte("old")}
	m.executions = []*domain.Execution{{ID: "old"}}
	m.execCursor = 1
	selected, cmd := m.selectRequest(nil)
	require.Nil(t, cmd)
	require.Nil(t, selected.response)
	require.Empty(t, selected.executions)
	selected.activeRequest = &domain.Request{ID: "old", Method: "GET", URL: "https://old.test"}
	selected.response = &exec.ExecuteResult{Body: []byte("old")}
	selectedRequest := &domain.Request{ID: "new", Method: "POST", URL: "https://new.test"}
	selected, _ = selected.selectRequest(selectedRequest)
	require.Nil(t, selected.response)
	require.Equal(t, 0, selected.execCursor)
	require.Empty(t, selected.activeValidationErr())
	require.Nil(t, interactionModel().allCollectionIDs())
}

func TestHelpKeyRecordingResetAndErrorBranches(t *testing.T) {
	configDir := t.TempDir()
	m := New(Deps{Config: config.Default(configDir), ConfigDir: configDir})
	m.mode = helpMode
	updated, _ := m.handleHelpKey(tea.KeyMsg{Type: tea.KeyDown})
	m = updatedCertificateModel(t, updated)
	updated, _ = m.handleHelpKey(tea.KeyMsg{Type: tea.KeyUp})
	m = updatedCertificateModel(t, updated)
	updated, _ = m.handleHelpKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updatedCertificateModel(t, updated)
	require.Equal(t, helpRecording, m.helpEditState)
	m.helpEditAction = "quit"
	updated, _ = m.handleHelpKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Z'}})
	m = updatedCertificateModel(t, updated)
	require.Equal(t, helpViewing, m.helpEditState)

	m.helpEditState = helpRecording
	m.helpEditAction = "quit"
	updated, _ = m.handleHelpKey(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updatedCertificateModel(t, updated)
	require.Equal(t, helpViewing, m.helpEditState)

	m.helpEditState = helpRecording
	m.helpEditAction = "not-an-action"
	updated, _ = m.handleHelpKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updatedCertificateModel(t, updated)
	require.Equal(t, helpError, m.helpEditState)
	updated, _ = m.handleHelpKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updatedCertificateModel(t, updated)
	require.Equal(t, helpViewing, m.helpEditState)

	badConfig := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(badConfig, []byte("file"), 0o600))
	m.configDir = badConfig
	m.helpEditState = helpRecording
	m.helpEditAction = "help_close"
	updated, _ = m.handleHelpKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	require.Equal(t, helpError, updatedCertificateModel(t, updated).helpEditState)

	m.configDir = configDir
	m.helpEditState = helpConfirmResetAll
	updated, _ = m.handleHelpKey(tea.KeyMsg{Type: tea.KeyEsc})
	require.Equal(t, helpViewing, updatedCertificateModel(t, updated).helpEditState)
	m.helpEditState = helpConfirmResetAll
	updated, _ = m.handleHelpKey(tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, helpViewing, updatedCertificateModel(t, updated).helpEditState)
}

func TestNormalPaneActionBranches(t *testing.T) {
	ctx := context.Background()
	st, err := store.New(t.TempDir() + "/actions.db")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	collection := &domain.Collection{ID: "action-col", Name: "Actions"}
	require.NoError(t, st.SaveCollection(ctx, collection))
	request := &domain.Request{
		ID: "action-req", CollectionID: collection.ID, Name: "Get", Method: "GET",
		URL: "https://example.test", Headers: "{}", Body: "{}",
	}
	require.NoError(t, st.SaveRequest(ctx, request))
	newModel := func() Model {
		m := New(Deps{
			Config: config.Default(t.TempDir()), Ctx: ctx,
			Reader: st, Writer: st, ColWriter: st,
			EnvReader: st, EnvWriter: st, ActiveEnvStore: st,
			Scheduler: st, Executor: fakeExecutor{},
		})
		m.collections = []*domain.Collection{collection}
		m.collectionRequests = map[string][]*domain.Request{collection.ID: {request}}
		m.activeRequest = request
		m.focus = requestPane
		return m
	}
	for _, action := range []string{
		keybindings.ActionEditURL, keybindings.ActionMethodNext, keybindings.ActionMethodPrev,
		keybindings.ActionSendRequest, keybindings.ActionScheduleRun, keybindings.ActionEditBody,
		keybindings.ActionEnvOpen, keybindings.ActionEnvNext, keybindings.ActionEnvPrev,
		keybindings.ActionEditAuth, keybindings.ActionEditHeaders,
	} {
		updated, _ := newModel().handleRequestAction(action)
		require.NotNil(t, updated, action)
	}
	updated, _ := interactionModel().handleRequestAction(keybindings.ActionDeleteRequest)
	require.Contains(t, updatedCertificateModel(t, updated).statusErr, "Select a request")
	updated, _ = newModel().handleRequestAction(keybindings.ActionDeleteRequest)
	require.Equal(t, promptDeleteTiny, updatedCertificateModel(t, updated).promptMode)

	for _, action := range []string{"history_next", "history_prev", "tab_body", "tab_headers", "tab_raw", "tab_next", "tab_prev"} {
		_, _ = newModel().handleResponseAction(action)
	}
	updated, _ = newModel().handleResponseAction("retry_request")
	require.Contains(t, updatedCertificateModel(t, updated).statusErr, "retry unavailable")
	raw := newModel()
	raw.executions = []*domain.Execution{{
		RequestID:       request.ID,
		RequestSnapshot: `{"method":"GET","url":"https://snapshot.test","headers":"{}","body":""}`,
	}}
	updated, cmd := raw.handleResponseAction("retry_request")
	require.NotNil(t, updated)
	require.NotNil(t, cmd)

	for _, action := range []string{"cursor_down", "cursor_up", "expand", "collapse", "add", "add_request", deleteToken, "rename", keybindings.ActionImportPostman} {
		updated, _ := newModel().handleSidebarAction(action)
		require.NotNil(t, updated, action)
	}
	updated, _ = interactionModel().handleSidebarAction("add_request")
	require.Contains(t, updatedCertificateModel(t, updated).statusErr, "Select a collection")
}

func TestClientCertificateKeyBranches(t *testing.T) {
	dir := t.TempDir()
	manager := &certificateManagerStub{}
	m := New(Deps{
		Config: config.Default(dir), ConfigDir: dir,
		CertificateManager: manager,
	}).openClientCertModal()
	m.clientCerts = []config.ClientCertificate{
		{Host: "one.test", File: "one.pem"},
		{Host: "two.test", File: "two.pem"},
	}
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyDown}, {Type: tea.KeyUp}, {Type: tea.KeyEnter}, {Type: tea.KeyDown},
		{Type: tea.KeyRunes, Runes: []rune{'e'}}, {Type: tea.KeyEsc},
		{Type: tea.KeyRunes, Runes: []rune{'d'}},
	} {
		updated, _ := m.handleClientCertKey(key)
		m = updatedCertificateModel(t, updated)
	}
	require.Len(t, m.clientCerts, 1)
	m.clientCertCursor = 99
	_, _ = m.handleClientCertKey(tea.KeyMsg{Type: tea.KeyDown})
	_, _ = m.deleteClientCert()
	m.clientCertCursor = 0
	manager.err = errors.New("reload")
	updated, _ := m.deleteClientCert()
	require.Contains(t, updatedCertificateModel(t, updated).clientCertError, "reload")
	manager.err = nil
	m.clientCertEditing = true
	m.clientCertField = clientCertHostField
	for i := 0; i < clientCertFieldCount; i++ {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
		m = updatedCertificateModel(t, updated)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	require.NotNil(t, updated)
}

func TestViewPaneStates(t *testing.T) {
	m := interactionModel()
	m.width, m.height = 120, 40
	m.activeRequest = &domain.Request{
		ID: "r", CollectionID: "c", Name: "Request", Method: "GET",
		URL: "https://example.test", Headers: `{"X-Test":"yes"}`, Body: "body",
	}
	m.response = &exec.ExecuteResult{
		StatusCode: 200, Status: "200 OK", Size: 15,
		Body:    []byte("{\"ok\":true}"),
		Headers: map[string][]string{"Content-Type": {"application/json"}},
	}
	for _, dim := range []DimMode{DimWide, DimNarrow, DimTiny, DimAbsurd, DimAuto} {
		m.forceDim = dim
		m.focus = requestPane
		require.NotEmpty(t, m.View())
		m.focus = responsePane
		require.NotEmpty(t, m.View())
	}
	m.forceDim = DimWide
	for _, tab := range []responseTabID{bodyTab, headersTab, rawTab} {
		m.responseTab = tab
		require.NotEmpty(t, m.viewResponsePane(80, 20))
		require.NotEmpty(t, m.viewResponseBody())
		require.NotEmpty(t, m.viewResponseHeaders())
	}
	require.NotEmpty(t, m.viewAuthEditor())
	require.NotEmpty(t, m.viewHelp())
	require.NotEmpty(t, m.viewImportModal())
	m.importPreview = &curl.ImportResult{
		Method: "POST", URL: "https://example.test",
		Headers: http.Header{"X-Test": {"yes"}}, Body: "payload",
	}
	require.NotEmpty(t, m.viewImportModal())
	m.importError = "invalid preview"
	m.importPreview = &curl.ImportResult{
		Method: "POST", URL: "https://example.test", Security: curl.Dangerous,
		Headers: http.Header{"Authorization": {"secret"}},
		Body:    strings.Repeat("body", 140),
		Certificate: &curl.CertificateSpec{
			Type: "PEM", File: "client.pem", KeyFile: "client.key", CAFile: "ca.pem",
		},
		Warnings: []string{"review this import"},
	}
	require.NotEmpty(t, m.viewImportModal())
	require.NotEmpty(t, m.viewLiveResponseStatus())
	m.response = nil
	m.executions = []*domain.Execution{
		{
			ID: "e1", RequestID: "r", StatusCode: 200, ResponseBody: "ok",
			ResponseHeaders: `{"X-Test":"yes"}`, CompletedAt: time.Now().Add(-time.Hour),
		},
		{
			ID: "e2", RequestID: "r", StatusCode: 500, ResponseBody: "bad",
			ResponseHeaders: "{}", CompletedAt: time.Now(),
		},
	}
	m.execCursor = 1
	require.NotEmpty(t, m.viewExecutionHistoryPopup(80, 20))
	require.NotEmpty(t, m.viewExecutionBody(m.executions[1]))
	require.NotEmpty(t, m.viewExecutionHeaders(m.executions[1]))
	require.NotEmpty(t, m.viewExecutionStatus(m.executions[1]))
	require.NotEmpty(t, m.viewStatusBar("override"))
	require.NotEmpty(t, m.formatResponseTextContent())
	require.NotEmpty(t, m.requestBodyPreviewSourceKey())
	require.NotEmpty(t, m.responseTextSourceKey())
	m.mode = searchMode
	m.searchResults = []*search.SearchHit{
		{Request: m.activeRequest, Collection: &domain.Collection{Name: "Collection"}, Score: 1},
	}
	require.NotEmpty(t, m.viewSearchModal())
	require.NotEmpty(t, m.renderSearchHit(m.searchResults[0], "req", 50))
	require.NotEmpty(t, m.collectionNameForSearchHit(m.searchResults[0]))
	m.mode = normalMode
	m.collections = []*domain.Collection{{ID: "c", Name: "Collection"}}
	m.promptMode = promptAdd
	m.promptInput.SetValue("new")
	require.NotEmpty(t, m.viewCollectionPromptModal())
	for _, prompt := range []promptType{promptAddRequest, promptAddEnv, promptRename, promptDeleteConfirm, promptDeleteTiny} {
		m.promptMode = prompt
		m.promptTargetID = "missing"
		m.promptTargetCollectionID = ""
		require.NotEmpty(t, m.viewCollectionPromptModal())
	}
	m.promptMode = promptDeleteTiny
	m.promptTargetCollectionID = "c"
	require.Contains(t, m.viewCollectionPromptModal(), "Delete Request")
}

func TestMouseGeometryBranches(t *testing.T) {
	m := interactionModel()
	m.width = 100
	m.height = 30
	layout := m.currentLayout()
	_ = m.requestBodyTextRect(layout)
	m.activeField = bodyField
	_ = m.requestBodyTextRect(layout)
	m.activeField = noneField
	m.activeRequest = &domain.Request{ID: "r", URL: "https://example.test"}
	collection := &domain.Collection{ID: "mouse-col", Name: "Mouse"}
	request := &domain.Request{
		ID: "mouse-req", CollectionID: collection.ID, Name: "Request",
		Method: "GET", URL: "https://example.test",
	}
	m.collections = []*domain.Collection{collection}
	m.collectionRequests = map[string][]*domain.Request{collection.ID: {request}}
	_, _ = m.selectSidebarRequest(99, 0)
	_, _ = m.selectSidebarRequest(0, 99)
	_, _ = m.selectSidebarRequest(0, 0)
	_ = m.requestBodyPreviewRect(layout)
	m.response = &exec.ExecuteResult{Status: "200 OK", Body: []byte("response")}
	_ = m.responseTextRect(layout)
	m.executions = []*domain.Execution{{ID: "new"}, {ID: "old"}}
	m.execCursor = 1
	_, _ = m.responseHistoryHitAt(80, 20, layout)
	_, _ = m.handleSidebarClick(tea.MouseMsg{X: 5, Y: 5})
	require.NotEmpty(t, layout)
}

func TestUpdateMessageDispatchBranches(t *testing.T) {
	ctx := context.Background()
	base := interactionModel()
	collection := &domain.Collection{ID: "updates", Name: "Updates"}
	request := &domain.Request{
		ID: "request", CollectionID: collection.ID, Name: "Get",
		Method: "GET", URL: "https://example.test",
	}
	messages := []tea.Msg{
		themeWatchTickMsg{},
		curlClipboardMsg{err: errors.New("clipboard")},
		postmanImportResultMsg{output: " imported ", err: errors.New("import failed")},
		tea.WindowSizeMsg{Width: 80, Height: 24},
		softWindowSizeMsg{Width: 90, Height: 30},
		collectionsLoadedMsg{collections: []*domain.Collection{collection}},
		requestsLoadedMsg{collectionID: collection.ID, requests: []*domain.Request{request}},
		errLoadMsg{err: errors.New("load")},
		searchResultsMsg{hits: []*search.SearchHit{{Request: request}}},
		viewerClipboardMsg{err: errors.New("copy")},
		executionHistoryLoadedMsg{requestID: "stale"},
		envSavedMsg{envID: "env"},
		envSaveErrMsg{err: errors.New("save")},
		envCreatedMsg{},
		envLoadedMsg{collectionID: collection.ID, envID: "env"},
		promptCompletedMsg{},
		collectionSavedMsg{},
		collectionSavedErrMsg{err: errors.New("collection")},
		scheduledRunSaveErrMsg{err: errors.New("schedule")},
		scheduledRunMissedMsg{seq: 99, name: "stale"},
		scheduledRunWakeMsg{seq: 99},
	}
	for _, msg := range messages {
		updated, _ := base.Update(msg)
		require.NotNil(t, updated)
	}

	m := New(Deps{Config: config.Default(""), Ctx: ctx})
	m.collections = []*domain.Collection{collection}
	m.activeRequest = request
	m.activeEnvStore = nil
	updated, _ := m.Update(
		requestsLoadedMsg{collectionID: collection.ID, requests: []*domain.Request{request}},
	)
	require.Equal(t, request.ID, updatedCertificateModel(t, updated).activeRequest.ID)
	updated, _ = m.Update(executionHistoryLoadedMsg{
		requestID: request.ID, executions: []*domain.Execution{{ID: "execution"}},
	})
	require.Len(t, updatedCertificateModel(t, updated).executions, 1)
	updated, _ = m.Update(searchResultsMsg{hits: []*search.SearchHit{{Request: request}}})
	require.True(t, updatedCertificateModel(t, updated).searched)

	postmanModel := base
	postmanModel.mode = postmanImportMode
	postmanModel.postmanBusy = true
	updated, cmd := postmanModel.Update(postmanImportResultMsg{output: " done "})
	require.False(t, updatedCertificateModel(t, updated).postmanBusy)
	require.Nil(t, cmd)

	for _, mode := range []modeID{searchMode, viewerMode, helpMode, envMode, importMode, collectionPromptMode, scheduleMode, clientCertMode, postmanImportMode, normalMode} {
		model := interactionModel()
		model.mode = mode
		model.width, model.height = 100, 30
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
		require.NotNil(t, updated)
	}

	for _, msg := range []tea.Msg{
		envLoadedMsg{collectionID: collection.ID},
		collectionSavedMsg{},
		scheduledRunSavedMsg{runAt: time.Now()},
		scheduledRunBackgroundResultMsg{},
	} {
		updated, _ := m.Update(msg)
		require.NotNil(t, updated)
	}
	_, cmd = m.Update(scheduledRunWakeMsg{seq: m.scheduleTimerSeq})
	require.NotNil(t, cmd)
}

type failingEnvironmentWriter struct{ store.EnvironmentWriter }

func (f failingEnvironmentWriter) SaveEnvironment(context.Context, *domain.Environment) error {
	return errors.New("environment write")
}

type failingCollectionWriter struct{ store.CollectionWriter }

func (f failingCollectionWriter) SaveCollection(context.Context, *domain.Collection) error {
	return errors.New("collection write")
}
func (f failingCollectionWriter) DeleteCollection(context.Context, string) error {
	return errors.New("collection delete")
}

func TestInlineEditorsAndWriteCommands(t *testing.T) {
	ctx := context.Background()
	st, err := store.New(t.TempDir() + "/updates.db")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	collection := &domain.Collection{ID: "edit-col", Name: "Edit"}
	require.NoError(t, st.SaveCollection(ctx, collection))
	request := &domain.Request{
		ID: "edit-req", CollectionID: collection.ID, Name: "Get",
		Method: "GET", URL: "https://example.test", Headers: "{}",
	}
	require.NoError(t, st.SaveRequest(ctx, request))
	m := New(Deps{
		Config: config.Default(""), Ctx: ctx, Reader: st, Writer: st,
		ColWriter: st, EnvWriter: st, EnvReader: st,
	})
	m.activeRequest = request
	m.bodyTextarea.SetValue("body updated")
	_, cmd := m.saveBody()
	require.IsType(t, requestsLoadedMsg{}, cmd())

	auth := m.beginAuthEdit()
	auth.authEditor.tokenInput.SetValue("secret")
	_, _ = auth.handleAuthFieldEdit(tea.KeyMsg{Type: tea.KeyTab})
	_, _ = auth.handleAuthFieldEdit(tea.KeyMsg{Type: tea.KeyEnter})
	_, cmd = auth.saveAuth()
	require.IsType(t, requestsLoadedMsg{}, cmd())

	environment := &domain.Environment{CollectionID: collection.ID, Name: "dev", Data: "{}"}
	require.IsType(t, envCreatedMsg{}, saveEnvAndReloadCmd(ctx, st, environment)())
	require.IsType(
		t,
		collectionSavedMsg{},
		saveCollectionCmd(ctx, st, &domain.Collection{Name: "Saved"})(),
	)
	require.IsType(
		t,
		collectionSavedErrMsg{},
		saveCollectionCmd(ctx, failingCollectionWriter{st}, &domain.Collection{Name: "Fail"})(),
	)
	require.IsType(
		t,
		requestsLoadedMsg{},
		deleteRequestCmd(ctx, st, request.ID, collection.ID, nil)(),
	)
	require.IsType(
		t,
		collectionSavedErrMsg{},
		deleteRequestCmd(
			ctx,
			failingRequestWriter{RequestWriter: st, err: errors.New("request delete")},
			request.ID, collection.ID, st,
		)(),
	)
	require.IsType(
		t,
		collectionSavedErrMsg{},
		deleteRequestCmd(ctx, st, "missing", collection.ID, st)(),
	)
	require.IsType(t, collectionSavedMsg{}, deleteCollectionCmd(ctx, st, collection.ID)())
	require.IsType(
		t,
		collectionSavedErrMsg{},
		deleteCollectionCmd(ctx, failingCollectionWriter{st}, "missing")(),
	)
	require.IsType(
		t,
		collectionSavedErrMsg{},
		saveEnvAndReloadCmd(ctx, failingEnvironmentWriter{st}, environment)(),
	)

	for _, message := range []tea.Msg{collectionSavedMsg{}, envCreatedMsg{}, requestsLoadedMsg{collectionID: collection.ID}, errLoadMsg{err: errors.New("other")}} {
		wrapped := wrapPromptSuccess(func() tea.Msg { return message })()
		if _, ok := message.(collectionSavedMsg); ok {
			require.IsType(t, promptCompletedMsg{}, wrapped)
		} else if _, ok := message.(envCreatedMsg); ok {
			require.IsType(t, promptCompletedMsg{}, wrapped)
		} else if _, ok := message.(requestsLoadedMsg); ok {
			require.IsType(t, promptCompletedMsg{}, wrapped)
		} else {
			require.IsType(t, errLoadMsg{}, wrapped)
		}
	}
}

func TestCollectionPromptActions(t *testing.T) {
	ctx := context.Background()
	st, err := store.New(t.TempDir() + "/prompts.db")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	collection := &domain.Collection{ID: "prompt-col", Name: "Prompt"}
	require.NoError(t, st.SaveCollection(ctx, collection))
	request := &domain.Request{
		ID: "prompt-req", CollectionID: collection.ID, Name: "Request",
		Method: "GET", URL: "https://example.test",
	}
	require.NoError(t, st.SaveRequest(ctx, request))
	newModel := func(prompt promptType, target string) Model {
		m := New(Deps{
			Config: config.Default(""), Ctx: ctx, Reader: st,
			Writer: st, ColWriter: st, EnvWriter: st,
		})
		m.mode, m.promptMode, m.promptTargetID = collectionPromptMode, prompt, target
		return m
	}

	m := newModel(promptAdd, "")
	m.promptInput.SetValue("Created")
	_, cmd := m.handleCollectionPromptKey(tea.KeyMsg{Type: tea.KeyEnter})
	require.IsType(t, promptCompletedMsg{}, cmd())

	m = newModel(promptRename, collection.ID)
	m.promptInput.SetValue("Renamed")
	_, cmd = m.handleCollectionPromptKey(tea.KeyMsg{Type: tea.KeyEnter})
	require.IsType(t, promptCompletedMsg{}, cmd())

	m = newModel(promptAddRequest, collection.ID)
	m.promptInput.SetValue("New Request")
	_, cmd = m.handleCollectionPromptKey(tea.KeyMsg{Type: tea.KeyEnter})
	require.IsType(t, promptCompletedMsg{}, cmd())

	m = newModel(promptAddEnv, collection.ID)
	m.promptInput.SetValue("staging")
	_, cmd = m.handleCollectionPromptKey(tea.KeyMsg{Type: tea.KeyEnter})
	require.IsType(t, promptCompletedMsg{}, cmd())
	m.promptInput.SetValue(defaultEnvironmentName)
	updated, _ := m.handleCollectionPromptKey(tea.KeyMsg{Type: tea.KeyEnter})
	require.Contains(t, updatedCertificateModel(t, updated).statusErr, "reserved")

	m = newModel(promptDeleteConfirm, collection.ID)
	m.promptInput.SetValue("cancel")
	updated, cmdModel := m.handleCollectionPromptKey(tea.KeyMsg{Type: tea.KeyEnter})
	require.Nil(t, cmdModel)
	require.Equal(t, normalMode, updatedCertificateModel(t, updated).mode)
	m = newModel(promptDeleteRequestConfirm, request.ID)
	m.promptTargetCollectionID = collection.ID
	m.promptInput.SetValue("yes")
	_, cmd = m.handleCollectionPromptKey(tea.KeyMsg{Type: tea.KeyEnter})
	require.IsType(t, promptCompletedMsg{}, cmd())

	tinyRequest := &domain.Request{
		ID: "tiny-req", CollectionID: collection.ID, Name: "Tiny",
		Method: "GET", URL: "https://example.test",
	}
	require.NoError(t, st.SaveRequest(ctx, tinyRequest))
	m = newModel(promptDeleteTiny, tinyRequest.ID)
	m.promptTargetCollectionID = collection.ID
	_, cmd = m.handleCollectionPromptKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	require.IsType(t, promptCompletedMsg{}, cmd())
	m = newModel(promptDeleteTiny, "missing")
	_, cmd = m.handleCollectionPromptKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	require.IsType(t, collectionSavedErrMsg{}, cmd())
	m = newModel(promptDeleteConfirm, collection.ID)
	m.promptInput.SetValue(deleteToken)
	_, cmd = m.handleCollectionPromptKey(tea.KeyMsg{Type: tea.KeyEnter})
	require.IsType(t, promptCompletedMsg{}, cmd())

	withoutWriters := New(Deps{Config: config.Default(""), Ctx: ctx})
	withoutWriters.mode, withoutWriters.promptMode = collectionPromptMode, promptAdd
	withoutWriters.promptInput.SetValue("missing writer")
	updated, _ = withoutWriters.handleCollectionPromptKey(tea.KeyMsg{Type: tea.KeyEnter})
	require.Contains(t, updatedCertificateModel(t, updated).statusErr, "writer not available")
	withoutWriters.promptMode = promptAddEnv
	withoutWriters.promptInput.SetValue("dev")
	updated, _ = withoutWriters.handleCollectionPromptKey(tea.KeyMsg{Type: tea.KeyEnter})
	require.Contains(t, updatedCertificateModel(t, updated).statusErr, "Environment writer")
}

func TestAuthAndHeaderEditorKeyPaths(t *testing.T) {
	m := interactionModel()
	m.activeRequest = &domain.Request{
		ID: "editor", Method: "GET", URL: "https://example.test",
		AuthType: "bearer", Headers: `{"Accept":"application/json"}`,
	}
	m = m.beginAuthEdit()
	for _, key := range []tea.KeyMsg{{Type: tea.KeyDown}, {Type: tea.KeyUp}, {Type: tea.KeyRight}, {Type: tea.KeyLeft}} {
		_, _ = m.handleAuthFieldKey(key)
	}
	for _, authType := range []string{"bearer", "basic", "apikey"} {
		m.authEditor.authType = authType
		for i := range m.authEditor.rows() {
			m.authEditor.cursor = i
			m.authEditor.editing = false
			_, _ = m.handleAuthFieldKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
			_, _ = m.handleAuthFieldEdit(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
			_, _ = m.handleAuthFieldEdit(tea.KeyMsg{Type: tea.KeyTab})
			_, _ = m.handleAuthFieldEdit(tea.KeyMsg{Type: tea.KeyEnter})
		}
	}
	_, _ = m.handleAuthFieldKey(tea.KeyMsg{Type: tea.KeyEnter})
	_, _ = m.handleAuthFieldKey(tea.KeyMsg{Type: tea.KeyEsc})

	m = m.beginHeadersEdit()
	for _, key := range []tea.KeyMsg{{Type: tea.KeyDown}, {Type: tea.KeyUp}, {Type: tea.KeyRunes, Runes: []rune{'a'}}} {
		_, _ = m.handleHeadersFieldKey(key)
	}
	_, _ = m.handleHeaderFieldEdit(tea.KeyMsg{Type: tea.KeyTab})
	_, _ = m.handleHeaderFieldEdit(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	_, _ = m.handleHeaderFieldEdit(tea.KeyMsg{Type: tea.KeyEnter})
	_, _ = m.handleHeadersFieldKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	_, _ = m.handleHeaderFieldEdit(tea.KeyMsg{Type: tea.KeyEsc})
	_, _ = m.handleHeadersFieldKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	_, _ = m.handleHeadersFieldKey(tea.KeyMsg{Type: tea.KeyEnter})
	_, _ = m.handleHeadersFieldKey(tea.KeyMsg{Type: tea.KeyEsc})
}

func TestHTTPResponseAndErrorStatePaths(t *testing.T) {
	m := interactionModel()
	m.activeRequest = &domain.Request{
		ID: "active", CollectionID: "col", Name: "Get", Method: "GET",
		URL: "https://example.test",
	}
	m.loading = true
	m.cancel = func() {}
	updated, cmd := m.handleHTTPResponse(
		httpResponseMsg{requestID: "stale", result: &exec.ExecuteResult{Status: "200"}},
	)
	require.False(t, updatedCertificateModel(t, updated).loading)
	require.Nil(t, cmd)

	m.loading = true
	updated, cmd = m.handleHTTPResponse(httpResponseMsg{result: &exec.ExecuteResult{Status: "200"}})
	require.False(t, updatedCertificateModel(t, updated).loading)
	require.Nil(t, cmd)

	m.loading = true
	updated, cmd = m.handleHTTPResponse(
		httpResponseMsg{requestID: "active", result: &exec.ExecuteResult{Status: "200"}},
	)
	require.False(t, updatedCertificateModel(t, updated).loading)
	require.NotNil(t, cmd)

	for _, tc := range []struct {
		name string
		req  *domain.Request
		id   string
		err  error
		want string
	}{
		{"stale invalid URL", m.activeRequest, "stale", exec.ErrInvalidURL, ""},
		{"matching invalid URL", m.activeRequest, "active", exec.ErrInvalidURL, ""},
		{"cancelled", m.activeRequest, "active", exec.ErrRequestCancelled, ""},
		{"timeout", m.activeRequest, "active", exec.ErrTimeout, "Request timed out"},
		{"unexpected", m.activeRequest, "active", errors.New("network"), "unexpected error"},
		{"missing request", nil, "active", exec.ErrInvalidURL, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := interactionModel()
			model.activeRequest = tc.req
			model.loading = true
			model.cancel = func() {}
			updated, _ := model.handleHTTPErr(httpErrMsg{requestID: tc.id, err: tc.err})
			got := updatedCertificateModel(t, updated)
			require.False(t, got.loading)
			if tc.want == "Request timed out" {
				require.Contains(t, got.statusErr, tc.want)
			} else if tc.want != "" {
				require.ErrorContains(t, got.err, tc.want)
			}
		})
	}
}
