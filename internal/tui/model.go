// Package tui implements the bubbletea TUI for Quark.
package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/keybindings"
	"github.com/crazy-vedic/quark/internal/search"
	"github.com/crazy-vedic/quark/internal/store"
)

// --- Layout enums ---

// paneID identifies which pane has keyboard focus.
type paneID int

const (
	sidebarPane paneID = iota
	requestPane
	responsePane
)

// modeID identifies the current overlay/modal mode.
type modeID int

const (
	normalMode modeID = iota
	searchMode
	helpMode
	importMode
	envMode
	collectionPromptMode
	scheduleMode
)

// promptType identifies which collection prompt is active.
type promptType int

const (
	promptNone promptType = iota
	promptAdd
	promptRename
	promptDeleteConfirm
	promptDeleteTiny
	promptAddRequest
	promptAddEnv
)

// helpEditState tracks the sub-state of the interactive help overlay.
type helpEditState int

const (
	helpViewing helpEditState = iota
	helpRecording
	helpError
	helpConfirmResetAll
)

// requestField identifies which sub-field is active in the request pane.
type requestField int

const (
	noneField requestField = iota
	urlField
	bodyField
	headersField
	authField
)

// responseTabID identifies which tab is active in the response pane.
type responseTabID int

const (
	bodyTab responseTabID = iota
	headersTab
	rawTab
)

// --- Message types ---

// httpResponseMsg is sent when an HTTP request completes successfully.
type httpResponseMsg struct {
	requestID string
	result    *exec.ExecuteResult
}

// httpErrMsg is sent when an HTTP request fails.
type httpErrMsg struct {
	requestID string
	err       error
}

// collectionsLoadedMsg carries the result of ListCollections.
type collectionsLoadedMsg struct{ collections []*domain.Collection }

// requestsLoadedMsg carries requests for a collection.
type requestsLoadedMsg struct {
	collectionID string
	requests     []*domain.Request
}

// errLoadMsg carries a data-loading error.
type errLoadMsg struct{ err error }

// searchResultsMsg carries search hits.
type searchResultsMsg struct{ hits []*search.SearchHit }

// executionHistoryLoadedMsg carries persisted executions for a request.
type executionHistoryLoadedMsg struct {
	requestID  string
	executions []*domain.Execution
}

// collectionSavedMsg is sent when a collection is saved successfully.
type collectionSavedMsg struct{}

// collectionSavedErrMsg is sent when a collection save fails.
type collectionSavedErrMsg struct{ err error }

// promptCompletedMsg wraps a successful prompt action so prompt teardown
// happens in one place before the follow-up success message is handled.
type promptCompletedMsg struct{ inner tea.Msg }

type scheduledRunSavedMsg struct {
	runAt time.Time
}

type scheduledRunSaveErrMsg struct{ err error }

type scheduledRunWakeMsg struct {
	seq int
}

type scheduledRunMissedMsg struct {
	seq  int
	name string
}

type scheduledRunBackgroundResultMsg struct {
	sent   []scheduledRunSuccess
	failed []scheduledRunFailure
}

type scheduledRunSuccess struct {
	requestID string
	label     string
	name      string
	result    *exec.ExecuteResult
}

type scheduledRunFailure struct {
	name string
	err  error
}

// --- Model ---

// Model is the root bubbletea model for Quark's TUI.
// It is a value type — all Update methods use value receivers, making it
// goroutine-safe through the bubbletea message loop.
type Model struct {
	// --- Dependencies (injected via Deps — all narrow interfaces) ---
	lister          store.CollectionLister
	reader          store.RequestReader
	writer          store.RequestWriter
	colWriter       store.CollectionWriter // for add/rename/delete collections
	executionReader store.ExecutionReader
	executor        RequestExecutor         // narrow interface; *exec.Executor satisfies this
	searcher        RequestSearcher         // narrow interface; *search.Searcher satisfies this
	importer        CurlImporter            // narrow interface; *curl.Importer satisfies this
	envReader       store.EnvironmentReader // narrow interface; *store.Store satisfies this
	envWriter       store.EnvironmentWriter // narrow interface; *store.Store satisfies this
	activeEnvStore  store.ActiveEnvironmentStore
	scheduler       store.ScheduledRunStore
	cfg             config.Config
	ctx             context.Context // root context; cancelled when main signals shutdown
	now             func() time.Time

	// --- Layout ---
	width, height int
	focus         paneID
	mode          modeID

	// --- Help overlay editor state ---
	helpCursor       int
	helpEditState    helpEditState
	helpEditAction   string
	helpEditErrMsg   string
	helpScrollOffset int

	// --- Sidebar state ---
	collections   []*domain.Collection
	colCursor     int
	expanded      map[string]bool // collectionID → expanded
	requests      []*domain.Request
	reqCursor     int
	sidebarOffset int // BUG-011: scroll offset for long collection lists
	// collectionRequests stores loaded requests for all expanded collections.
	// Key is collection ID; value is the list of requests.
	collectionRequests map[string][]*domain.Request

	// --- Request pane state ---
	activeRequest *domain.Request // currently selected request (template)
	urlInput      textinput.Model
	method        string
	activeField   requestField // which inline field is active in the request pane
	preEditURL    string       // URL value saved when editing starts; restored on Esc
	preEditBody   string       // Body value saved when editing starts; restored on Esc

	// --- Response state ---
	response    *exec.ExecuteResult
	responseTab responseTabID
	executions  []*domain.Execution
	execCursor  int

	// --- In-flight request ---
	cancel  context.CancelFunc
	loading bool

	// --- Error / success state ---
	err                   error             // unexpected / fatal error
	requestValidationErrs map[string]string // request ID → Tier 1 validation error (invalid URL etc.)
	statusErr             string            // Tier 2 — timeout, retryable
	statusSuccess         string            // Tier 3 — body/headers saved, etc.

	// --- Search modal ---
	searchInput   textinput.Model
	searchResults []*search.SearchHit
	searchCursor  int
	searchScroll  int
	searchCancel  context.CancelFunc // cancel func for the in-flight search goroutine
	searched      bool               // true once the first searchResultsMsg arrives
	commands      []commandPaletteItem

	// --- Import modal ---
	importPreview *curl.ImportResult
	importName    textinput.Model
	importColID   string

	// --- Collection prompt modal ---
	promptMode     promptType
	promptInput    textinput.Model
	promptTargetID string // collection ID for rename/delete; empty for add

	// --- Schedule modal ---
	scheduleInput    textinput.Model
	scheduleTimerSeq int

	// --- Body / Header inline editor ---
	bodyTextarea     textarea.Model
	headerPairs      []headerPair
	headerCursor     int
	headerEditing    bool
	headerKeyInput   textinput.Model
	headerValueInput textinput.Model
	authEditor       authEditor

	// --- Misc ---
	tmuxDetected    bool
	tmuxWarning     string
	showTmuxWarning bool // BUG-010: true only until first keypress after startup

	// DebugLog, when non-nil, receives a timestamped line for every key message.
	debugLog *os.File

	// configDir is the directory where config.toml and the DB are stored.
	configDir string
	// --- Environment state ---
	activeEnv      map[string]string // collectionID → envID
	cachedEnvName  string            // cached name for the active env of the current collection
	cachedEnvColID string            // collection ID the cached name is valid for
	envEditor      envEditor         // env editor modal state

	// resolver maps keys to actions. Immutable after construction; replaced on rebinding.
	resolver *keybindings.Resolver
}

// headerPair is a single key-value header entry in the header editor.
type headerPair struct {
	Key   string
	Value string
}

type authRowID int

const (
	authRowType authRowID = iota
	authRowToken
	authRowUsername
	authRowPassword
	authRowAPIKeyIn
	authRowAPIKeyName
	authRowAPIKeyValue
)

type authEditor struct {
	authType         string
	apiKeyIn         string
	cursor           int
	editing          bool
	tokenInput       textinput.Model
	usernameInput    textinput.Model
	passwordInput    textinput.Model
	apiKeyNameInput  textinput.Model
	apiKeyValueInput textinput.Model
}

// Deps holds all TUI dependencies.
type Deps struct {
	Lister          store.CollectionLister
	Reader          store.RequestReader
	Writer          store.RequestWriter
	ColWriter       store.CollectionWriter // for add/rename/delete collections
	ExecutionReader store.ExecutionReader
	EnvReader       store.EnvironmentReader
	EnvWriter       store.EnvironmentWriter
	ActiveEnvStore  store.ActiveEnvironmentStore
	Scheduler       store.ScheduledRunStore
	Executor        RequestExecutor // accepts *exec.Executor or any fake
	Searcher        RequestSearcher // accepts *search.Searcher or any fake
	Importer        CurlImporter    // accepts *curl.Importer or any fake
	Config          config.Config
	Resolver        *keybindings.Resolver
	// Ctx is the root context for all background operations. If nil,
	// context.Background() is used. Pass main.go's signal-aware context
	// so TUI goroutines are cancelled when the process receives SIGINT/SIGTERM.
	Ctx context.Context
	// Now returns the current time for scheduling. If nil, time.Now is used.
	Now func() time.Time
	// DebugLog, if set, receives a timestamped line for every key message.
	DebugLog *os.File
	// ConfigDir is the directory where config.toml and the DB are stored.
	// Used when persisting keybinding changes.
	ConfigDir string
}

// New constructs the root TUI model and detects multiplexer conflicts.
func New(deps Deps) Model {
	urlInput := textinput.New()
	urlInput.Placeholder = "https://..."
	urlInput.CharLimit = 2048

	searchInput := textinput.New()
	searchInput.Placeholder = "search requests..."
	searchInput.CharLimit = 256

	importName := textinput.New()
	importName.Placeholder = "request name"
	importName.CharLimit = 128

	promptInput := textinput.New()
	promptInput.CharLimit = 128

	scheduleInput := textinput.New()
	scheduleInput.Placeholder = "10m, in 1h, 2026-06-25 18:30"
	scheduleInput.CharLimit = 64

	bodyTA := textarea.New()
	bodyTA.Placeholder = "Enter request body..."
	bodyTA.SetHeight(5)

	headerKeyInput := textinput.New()
	headerKeyInput.Placeholder = "Header key"
	headerKeyInput.CharLimit = 128

	headerValueInput := textinput.New()
	headerValueInput.Placeholder = "Header value"
	headerValueInput.CharLimit = 2048

	rootCtx := deps.Ctx
	if rootCtx == nil {
		rootCtx = context.Background()
	}
	now := deps.Now
	if now == nil {
		now = time.Now
	}

	m := Model{
		lister:                deps.Lister,
		reader:                deps.Reader,
		writer:                deps.Writer,
		colWriter:             deps.ColWriter,
		executionReader:       deps.ExecutionReader,
		executor:              deps.Executor,
		searcher:              deps.Searcher,
		importer:              deps.Importer,
		cfg:                   deps.Config,
		ctx:                   rootCtx,
		method:                deps.Config.UI.DefaultMethod,
		urlInput:              urlInput,
		searchInput:           searchInput,
		importName:            importName,
		promptInput:           promptInput,
		bodyTextarea:          bodyTA,
		headerKeyInput:        headerKeyInput,
		headerValueInput:      headerValueInput,
		expanded:              make(map[string]bool),
		collectionRequests:    make(map[string][]*domain.Request),
		requestValidationErrs: make(map[string]string),
		reqCursor:             -1, // start on collection, not on a request
		focus:                 sidebarPane,
		debugLog:              deps.DebugLog,
		configDir:             deps.ConfigDir,
		resolver:              resolverOrDefault(deps.Resolver, deps.Config),
		envReader:             deps.EnvReader,
		envWriter:             deps.EnvWriter,
		activeEnvStore:        deps.ActiveEnvStore,
		scheduler:             deps.Scheduler,
		activeEnv:             make(map[string]string),
		now:                   now,
		scheduleInput:         scheduleInput,
		scheduleTimerSeq:      1,
	}

	// Detect tmux/screen keybinding conflicts.
	if os.Getenv("TMUX") != "" {
		m.tmuxDetected = true
		m.showTmuxWarning = true // BUG-010: show briefly on startup
		m.tmuxWarning = "tmux detected: some Ctrl+w bindings may be intercepted"
	} else if os.Getenv("STY") != "" {
		m.tmuxDetected = true
		m.showTmuxWarning = true
		m.tmuxWarning = "GNU screen detected: some Ctrl+w bindings may be intercepted"
	}

	return m
}

// resolverOrDefault returns a resolver built from cfg.Keybindings when r is nil.
// Guarantees tests that pass Deps{} still get a resolver so hardcoded fallbacks
// are never reached at runtime.
func resolverOrDefault(r *keybindings.Resolver, cfg config.Config) *keybindings.Resolver {
	if r != nil {
		return r
	}
	binds := cfg.Keybindings
	if binds.Quit == "" {
		binds = keybindings.DefaultKeybindings()
	}
	return keybindings.NewResolver(binds)
}

// Init loads collections on startup.
// Returns nil if lister is not injected (e.g. in unit tests that drive Update directly).
func (m Model) Init() tea.Cmd {
	var cmds []tea.Cmd
	if m.lister != nil {
		cmds = append(cmds, loadCollectionsCmd(m.ctx, m.lister))
	}
	if m.scheduler != nil {
		cmds = append(cmds, scheduleNextWakeCmd(
			m.ctx,
			m.scheduler,
			m.reader,
			m.now,
			m.scheduleTimerSeq,
		))
	}
	return tea.Batch(cmds...)
}

// --- Commands ---

func loadCollectionsCmd(ctx context.Context, lister store.CollectionLister) tea.Cmd {
	return func() tea.Msg {
		cols, err := lister.ListCollections(ctx)
		if err != nil {
			return errLoadMsg{err: err}
		}
		return collectionsLoadedMsg{collections: cols}
	}
}

func loadRequestsCmd(ctx context.Context, reader store.RequestReader, collectionID string) tea.Cmd {
	return func() tea.Msg {
		reqs, err := reader.ListRequests(ctx, collectionID)
		if err != nil {
			return errLoadMsg{err: err}
		}
		return requestsLoadedMsg{collectionID: collectionID, requests: reqs}
	}
}

func loadExecutionHistoryCmd(
	ctx context.Context,
	reader store.ExecutionReader,
	requestID string,
) tea.Cmd {
	return func() tea.Msg {
		if reader == nil || requestID == "" {
			return executionHistoryLoadedMsg{requestID: requestID}
		}
		executions, err := reader.ListExecutionsByRequest(ctx, requestID)
		if err != nil {
			return errLoadMsg{err: fmt.Errorf("load executions: %w", err)}
		}
		return executionHistoryLoadedMsg{requestID: requestID, executions: executions}
	}
}

// loadActiveEnvCmd queries the store for the persisted active env of a collection.
func loadActiveEnvCmd(
	ctx context.Context,
	s store.ActiveEnvironmentStore,
	collectionID string,
) tea.Cmd {
	return func() tea.Msg {
		id, err := s.GetActiveEnvironment(ctx, collectionID)
		if err != nil {
			return envLoadedMsg{collectionID: collectionID} // empty envID: no persisted state
		}
		return envLoadedMsg{collectionID: collectionID, envID: id}
	}
}

// status sets a status message of the given level and clears all other status fields.
// level is one of: "error", "success", "warn".
func (m Model) status(level, msg string) Model {
	switch level {
	case "error", "warn":
		m.statusErr = msg
		m.statusSuccess = ""
	case "success":
		m.statusSuccess = msg
		m.statusErr = ""
	}
	return m
}

// unsavedRequestKey is the validation-error map key used for the request
// currently in the pane when it has no persisted ID yet (new/unsaved request).
const unsavedRequestKey = "\x00unsaved"

// activeRequestKey returns the validation-error map key for the active request,
// falling back to a sentinel when no saved request is selected.
func (m Model) activeRequestKey() string {
	if m.activeRequest == nil || m.activeRequest.ID == "" {
		return unsavedRequestKey
	}
	return m.activeRequest.ID
}

func (m Model) activeValidationErr() string {
	if m.requestValidationErrs == nil {
		return ""
	}
	return m.requestValidationErrs[m.activeRequestKey()]
}

func (m Model) setRequestValidationErr(requestID, msg string) Model {
	if requestID == "" {
		requestID = unsavedRequestKey
	}
	if m.requestValidationErrs == nil {
		m.requestValidationErrs = make(map[string]string)
	}
	if msg == "" {
		delete(m.requestValidationErrs, requestID)
	} else {
		m.requestValidationErrs[requestID] = msg
	}
	return m
}

func (m Model) clearRequestValidationErr(requestID string) Model {
	return m.setRequestValidationErr(requestID, "")
}

func searchCmd(ctx context.Context, s RequestSearcher, collectionID, query string) tea.Cmd {
	return func() tea.Msg {
		if s == nil {
			return searchResultsMsg{}
		}
		result, err := s.Search(ctx, collectionID, query)
		if err != nil {
			return errLoadMsg{err: fmt.Errorf("search: %w", err)}
		}
		return searchResultsMsg{hits: result.Hits}
	}
}

type allCollectionsSearcher interface {
	SearchAll(
		ctx context.Context,
		query string,
		collectionIDs []string,
	) (*search.SearchResult, error)
}

func searchAllCmd(
	ctx context.Context,
	s allCollectionsSearcher,
	collectionIDs []string,
	query string,
) tea.Cmd {
	return func() tea.Msg {
		if s == nil {
			return searchResultsMsg{}
		}
		result, err := s.SearchAll(ctx, query, collectionIDs)
		if err != nil {
			return errLoadMsg{err: fmt.Errorf("search: %w", err)}
		}
		return searchResultsMsg{hits: result.Hits}
	}
}

// saveRequestCmd wraps a SaveRequest call in a tea.Cmd so it runs in a goroutine.
// On success it returns a requestsLoadedMsg with the collectionID so the sidebar reloads.
func saveRequestCmd(
	ctx context.Context,
	w store.RequestWriter,
	reader store.RequestReader,
	req *domain.Request,
) tea.Cmd {
	return func() tea.Msg {
		if err := w.SaveRequest(ctx, req); err != nil {
			return errLoadMsg{err: fmt.Errorf("save request: %w", err)}
		}
		if reader == nil || req.CollectionID == "" {
			return requestsLoadedMsg{}
		}
		reqs, err := reader.ListRequests(ctx, req.CollectionID)
		if err != nil {
			return errLoadMsg{err: fmt.Errorf("reload requests: %w", err)}
		}
		return requestsLoadedMsg{collectionID: req.CollectionID, requests: reqs}
	}
}

// dispatchCmd follows Go convention: context.Context is the first parameter.
// Closes over immutable values only — never a *Model pointer.
func dispatchCmd(ctx context.Context, executor RequestExecutor, req *domain.Request) tea.Cmd {
	return func() tea.Msg {
		result, err := executor.Execute(ctx, req)
		if err != nil {
			return httpErrMsg{requestID: req.ID, err: err}
		}
		return httpResponseMsg{requestID: req.ID, result: result}
	}
}

func saveScheduledRunCmd(
	ctx context.Context,
	scheduler store.ScheduledRunWriter,
	requestID string,
	runAt time.Time,
) tea.Cmd {
	return func() tea.Msg {
		if scheduler == nil {
			return scheduledRunSaveErrMsg{err: fmt.Errorf("scheduler unavailable")}
		}
		run := &domain.ScheduledRun{
			ID:        uuid.New().String(),
			RequestID: requestID,
			RunAt:     runAt,
			Status:    domain.ScheduledRunPending,
		}
		if err := scheduler.SaveScheduledRun(ctx, run); err != nil {
			return scheduledRunSaveErrMsg{err: err}
		}
		return scheduledRunSavedMsg{runAt: runAt}
	}
}

func scheduleWakeAfterCmd(ctx context.Context, delay time.Duration, seq int) tea.Cmd {
	if delay < 0 {
		delay = 0
	}
	return func() tea.Msg {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			return scheduledRunWakeMsg{seq: seq}
		}
	}
}

func scheduleNextWakeCmd(
	ctx context.Context,
	scheduler store.ScheduledRunReader,
	reader store.RequestReader,
	now func() time.Time,
	seq int,
) tea.Cmd {
	return func() tea.Msg {
		if scheduler == nil {
			return nil
		}
		run, err := scheduler.NextPendingScheduledRun(ctx)
		if err != nil {
			return errLoadMsg{err: fmt.Errorf("schedule next wake: %w", err)}
		}
		if run == nil {
			return nil
		}
		nowTime := now()
		if run.RunAt.Before(nowTime) {
			return scheduledRunMissedMsg{
				seq:  seq,
				name: scheduledRunName(ctx, reader, run),
			}
		}
		return scheduleWakeAfterCmd(ctx, run.RunAt.Sub(nowTime), seq)()
	}
}

func scheduledRunName(
	ctx context.Context,
	reader store.RequestReader,
	run *domain.ScheduledRun,
) string {
	if reader == nil || run == nil || run.RequestID == "" {
		return "scheduled request"
	}
	req, err := reader.GetRequest(ctx, run.RequestID)
	if err != nil || req == nil || strings.TrimSpace(req.Name) == "" {
		return run.RequestID
	}
	return req.Name
}

func scheduledRunSuccessLabel(
	ctx context.Context,
	lister store.CollectionLister,
	req *domain.Request,
) string {
	if req == nil {
		return "scheduled request"
	}
	reqName := strings.TrimSpace(req.Name)
	if reqName == "" {
		reqName = req.ID
	}
	colName := strings.TrimSpace(req.CollectionID)
	if lister != nil && req.CollectionID != "" {
		if cols, err := lister.ListCollections(ctx); err == nil {
			for _, col := range cols {
				if col != nil && col.ID == req.CollectionID && strings.TrimSpace(col.Name) != "" {
					colName = strings.TrimSpace(col.Name)
					break
				}
			}
		}
	}
	if colName == "" {
		return reqName
	}
	return colName + " / " + reqName
}

func executeDueScheduledRunsCmd(
	ctx context.Context,
	scheduler store.ScheduledRunStore,
	lister store.CollectionLister,
	reader store.RequestReader,
	executor RequestExecutor,
	envReader store.EnvironmentReader,
	activeEnvStore store.ActiveEnvironmentStore,
	activeEnv map[string]string,
	now func() time.Time,
) tea.Cmd {
	activeEnvSnapshot := cloneActiveEnv(activeEnv)
	return func() tea.Msg {
		if scheduler == nil || reader == nil || executor == nil {
			return scheduledRunBackgroundResultMsg{
				failed: []scheduledRunFailure{{
					name: "scheduled request",
					err:  fmt.Errorf("scheduler unavailable"),
				}},
			}
		}
		runs, err := scheduler.ListDueScheduledRuns(ctx, now())
		if err != nil {
			return scheduledRunBackgroundResultMsg{
				failed: []scheduledRunFailure{{name: "scheduled request", err: err}},
			}
		}
		result := scheduledRunBackgroundResultMsg{}
		for _, run := range runs {
			name := scheduledRunName(ctx, reader, run)
			req, err := reader.GetRequest(ctx, run.RequestID)
			if err != nil {
				if saveErr := markScheduledRunFailed(ctx, scheduler, run, err); saveErr != nil {
					err = errors.Join(err, saveErr)
				}
				result.failed = append(result.failed, scheduledRunFailure{name: name, err: err})
				continue
			}
			run.Status = domain.ScheduledRunRunning
			run.LastError = ""
			if err := scheduler.SaveScheduledRun(ctx, run); err != nil {
				result.failed = append(result.failed, scheduledRunFailure{name: name, err: err})
				continue
			}
			execReq, err := interpolateScheduledRequest(
				ctx,
				envReader,
				activeEnvStore,
				activeEnvSnapshot,
				req,
			)
			var execResult *exec.ExecuteResult
			if err == nil {
				execResult, err = executor.Execute(ctx, execReq)
			}
			if err != nil {
				if saveErr := markScheduledRunFailed(ctx, scheduler, run, err); saveErr != nil {
					err = errors.Join(err, saveErr)
				}
				result.failed = append(result.failed, scheduledRunFailure{name: name, err: err})
				continue
			}
			run.Status = domain.ScheduledRunCompleted
			run.LastError = ""
			if err := scheduler.SaveScheduledRun(ctx, run); err != nil {
				result.failed = append(result.failed, scheduledRunFailure{name: name, err: err})
				continue
			}
			result.sent = append(result.sent, scheduledRunSuccess{
				requestID: req.ID,
				label:     scheduledRunSuccessLabel(ctx, lister, req),
				name:      name,
				result:    execResult,
			})
		}
		return result
	}
}

func interpolateScheduledRequest(
	ctx context.Context,
	envReader store.EnvironmentReader,
	activeEnvStore store.ActiveEnvironmentStore,
	activeEnv map[string]string,
	req *domain.Request,
) (*domain.Request, error) {
	if envReader == nil {
		return req, nil
	}
	activeEnvID := activeEnv[req.CollectionID]
	if activeEnvStore != nil {
		if persistedID, err := activeEnvStore.GetActiveEnvironment(
			ctx,
			req.CollectionID,
		); err == nil {
			activeEnvID = persistedID
		}
	}
	colEnv, globalEnv := exec.ResolveEnvVars(ctx, envReader, activeEnvID, req.CollectionID)
	if colEnv == nil && globalEnv == nil {
		return req, nil
	}
	return exec.InterpolateRequest(req, colEnv, globalEnv)
}

func markScheduledRunFailed(
	ctx context.Context,
	scheduler store.ScheduledRunWriter,
	run *domain.ScheduledRun,
	err error,
) error {
	run.Status = domain.ScheduledRunFailed
	run.LastError = err.Error()
	return scheduler.SaveScheduledRun(ctx, run)
}

func cloneActiveEnv(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (m Model) armScheduleTimer() (Model, tea.Cmd) {
	if m.scheduler == nil {
		return m, nil
	}
	m.scheduleTimerSeq++
	return m, scheduleNextWakeCmd(
		m.ctx,
		m.scheduler,
		m.reader,
		m.now,
		m.scheduleTimerSeq,
	)
}

// startRequest cancels any in-flight request and dispatches a new one.
func (m Model) startRequest(req *domain.Request) (Model, tea.Cmd) {
	if m.cancel != nil {
		m.cancel()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.loading = true
	m.err = nil
	m.statusErr = ""
	if req != nil {
		m = m.clearRequestValidationErr(req.ID)
	}
	return m, dispatchWithEnvCmd(ctx, m.executor, m.envReader, m.activeEnv, req)
}

// startRawRequest dispatches a request without mutating or interpolating the
// editable request template shown in the request pane.
func (m Model) startRawRequest(req *domain.Request) (Model, tea.Cmd) {
	if m.cancel != nil {
		m.cancel()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.loading = true
	m.err = nil
	m.statusErr = ""
	if req != nil {
		m = m.clearRequestValidationErr(req.ID)
	}
	return m, dispatchCmd(ctx, m.executor, req)
}

// dispatchSearch cancels any stale search goroutine, then dispatches a new one.
// Using context cancellation prevents nondeterministic result ordering.
func (m Model) dispatchSearch(query string) (Model, tea.Cmd) {
	if m.searchCancel != nil {
		m.searchCancel()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.searchCancel = cancel
	collectionIDs := m.allCollectionIDs()
	if searchAller, ok := m.searcher.(allCollectionsSearcher); ok && len(collectionIDs) > 0 {
		return m, searchAllCmd(ctx, searchAller, collectionIDs, query)
	}
	colID := m.activeCollectionID()
	return m, searchCmd(ctx, m.searcher, colID, query)
}

// activePaneRequest builds a *domain.Request from the current request pane state.
func (m Model) activePaneRequest() *domain.Request {
	base := m.activeRequest
	url := strings.TrimSpace(m.urlInput.Value())
	method := m.method
	if method == "" {
		method = "GET"
	}

	r := &domain.Request{
		Method: method,
		URL:    url,
	}
	if base != nil {
		r.ID = base.ID
		r.CollectionID = base.CollectionID
		r.Name = base.Name
		r.Headers = base.Headers
		r.AuthType = base.AuthType
		r.AuthConfig = base.AuthConfig
		r.Body = base.Body
	}
	return r
}

func (m Model) selectRequest(req *domain.Request) (Model, tea.Cmd) {
	previousID := ""
	if m.activeRequest != nil {
		previousID = m.activeRequest.ID
	}

	m.activeRequest = req
	if req == nil {
		if m.response != nil {
			_ = m.response.Cleanup()
		}
		m.response = nil
		m.executions = nil
		m.execCursor = 0
		return m, nil
	}

	m.urlInput.SetValue(req.URL)
	m.method = req.Method

	if req.ID != previousID {
		if m.response != nil {
			_ = m.response.Cleanup()
		}
		m.response = nil
		m.executions = nil
		m.execCursor = 0
		m = m.clearStatus()
	}

	return m, loadExecutionHistoryCmd(m.ctx, m.executionReader, req.ID)
}

// activeCollectionID returns the ID of the currently cursor-selected collection.
func (m Model) activeCollectionID() string {
	if len(m.collections) == 0 || m.colCursor >= len(m.collections) {
		return ""
	}
	return m.collections[m.colCursor].ID
}

func (m Model) allCollectionIDs() []string {
	if len(m.collections) == 0 {
		return nil
	}
	ids := make([]string, 0, len(m.collections))
	for _, col := range m.collections {
		if col == nil || col.ID == "" {
			continue
		}
		ids = append(ids, col.ID)
	}
	return ids
}

// selectedCollection returns the collection currently under the cursor, or nil.
func (m Model) selectedCollection() *domain.Collection {
	if len(m.collections) == 0 || m.colCursor >= len(m.collections) {
		return nil
	}
	return m.collections[m.colCursor]
}

// sidebarVisible returns the approximate number of visible collection rows
// based on terminal height. Used for scroll offset calculations (BUG-011).
func (m Model) sidebarVisible() int {
	v := m.height - 5 // header + borders + status bar
	if v < 1 {
		return 1
	}
	return v
}

// resizeBodyTextarea sets the body textarea to fill the exact remaining space
// in the request pane. Called on body activation and window resize.
func (m Model) resizeBodyTextarea() Model {
	layout := normalLayoutFor(m.width, m.height)
	requestInnerH := layout.requestH
	if requestInnerH < 1 {
		requestInnerH = 1
	}

	// Count fixed lines in the request pane (everything except the body).
	// Always: title + method/URL + blank + separator + hints = 5
	fixedLines := 5
	if m.activeRequest != nil {
		fixedLines++ // request name line
	}
	if m.activeValidationErr() != "" {
		fixedLines++ // validation error line
	}
	if m.loading {
		fixedLines++ // loading indicator line
	}

	avail := requestInnerH - fixedLines
	if avail < 3 {
		avail = 3
	}
	m.bodyTextarea.SetHeight(avail)

	innerW := layout.mainW - 2 // subtract border
	if innerW < 5 {
		innerW = 5
	}
	m.bodyTextarea.SetWidth(innerW)

	return m
}

// httpMethods is the cycling order for the method key (m).
var httpMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}

// nextMethod returns the next method in the cycle after current.
func nextMethod(current string) string {
	for i, m := range httpMethods {
		if m == current {
			return httpMethods[(i+1)%len(httpMethods)]
		}
	}
	return "GET"
}

// prevMethod returns the previous method in the cycle before current.
func prevMethod(current string) string {
	for i, m := range httpMethods {
		if m == current {
			return httpMethods[(i-1+len(httpMethods))%len(httpMethods)]
		}
	}
	return "GET"
}

// HelpCursor returns the current cursor position in the help overlay.
func (m Model) HelpCursor() int { return m.helpCursor }
