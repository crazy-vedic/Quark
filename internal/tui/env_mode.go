package tui

import (
	"context"
	"fmt"
	"sort"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/keybindings"
	"github.com/crazy-vedic/quark/internal/store"
)

// envDBTimeout is the context deadline for environment DB reads.
// Defined here as an alias of store.EnvDBTimeout to avoid a direct dependency
// on the constant location; if the store value changes, this tracks it.
var envDBTimeout = store.EnvDBTimeout

const defaultEnvironmentName = "default"

// envVar is a key-value pair in an environment.
type envVar struct {
	Key   string
	Value string
	Saved bool // true if loaded from the store; false if added or modified in the editor
}

// envTab represents a tab in the env editor.
type envTab struct {
	ID           string
	Name         string
	IsGlobal     bool
	CollectionID string
}

// envEditor holds the state for the environment editor modal.
type envEditor struct {
	active    bool
	tabs      []envTab
	tabIdx    int
	vars      []envVar
	varCursor int
	scroll    int
	editing   bool
	editKey   textinput.Model
	editVal   textinput.Model
	dirty     bool
	saveErr   string
}

// envSavedMsg is sent when an environment is saved successfully.
type envSavedMsg struct{}

// envCreatedMsg is sent when a new environment is created successfully.
type envCreatedMsg struct{}

// envSaveErrMsg is sent when an environment save fails.
type envSaveErrMsg struct{ err error }

// envLoadedMsg carries the persisted active env for a collection (or empty if none).
type envLoadedMsg struct {
	collectionID string
	envID        string
}

// openEnvEditor opens the env editor for the current collection.
func (m Model) openEnvEditor() (Model, tea.Cmd) {
	if m.envReader == nil {
		return m.status("error", "Environment reader not available"), nil
	}

	colID := m.activeCollectionID()
	if colID == "" {
		return m.status("error", "Select a collection first"), nil
	}

	ctx, cancel := context.WithTimeout(m.ctx, envDBTimeout)
	defer cancel()

	// Load global env.
	globalEnv, err := m.envReader.GetGlobalEnvironment(ctx)
	if err != nil {
		m = m.status("error", fmt.Sprintf("Load global env: %v", err))
		return m, nil
	}

	// Load collection envs.
	colEnvs, err := m.envReader.ListCollectionEnvironments(ctx, colID)
	if err != nil {
		m = m.status("error", fmt.Sprintf("Load envs: %v", err))
		return m, nil
	}

	// Build tabs: global first, then collection envs.
	tabs := []envTab{
		{ID: globalEnv.ID, Name: "Global", IsGlobal: true},
	}
	for _, e := range colEnvs {
		tabs = append(tabs, envTab{
			ID:           e.ID,
			Name:         e.Name,
			CollectionID: e.CollectionID,
		})
	}

	// Select the active env tab.
	activeID := m.activeEnv[colID]
	tabIdx := 0
	for i, t := range tabs {
		if t.ID == activeID {
			tabIdx = i
			break
		}
	}

	m.mode = envMode
	m.envEditor = envEditor{
		active: true,
		tabs:   tabs,
		tabIdx: tabIdx,
	}

	m = m.loadEnvEditorVars()
	return m, nil
}

func (m Model) closeEnvEditor() Model {
	m.mode = normalMode
	m.envEditor = envEditor{}
	return m
}

// loadEnvEditorVars loads the variables for the current tab.
func (m Model) loadEnvEditorVars() Model {
	if !m.envEditor.active || m.envReader == nil {
		return m
	}

	tab := m.envEditor.tabs[m.envEditor.tabIdx]
	ctx, cancel := context.WithTimeout(m.ctx, envDBTimeout)
	defer cancel()

	env, err := m.envReader.GetEnvironment(ctx, tab.ID)
	if err != nil {
		m.envEditor.saveErr = fmt.Sprintf("Load env: %v", err)
		return m
	}

	vars := env.Vars()
	pairs := make([]envVar, 0, len(vars))
	for k, v := range vars {
		pairs = append(pairs, envVar{Key: k, Value: v, Saved: true})
	}
	// Sort for stable display.
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].Key < pairs[j].Key
	})

	m.envEditor.vars = pairs
	m.envEditor.varCursor = 0
	m.envEditor.scroll = 0
	m.envEditor.dirty = false
	m.envEditor.saveErr = ""
	return m
}

// handleEnvKey handles key presses in the env editor modal.
func (m Model) handleEnvKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Editing sub-mode takes precedence over the resolver.
	if m.envEditor.editing {
		return m.handleEnvEditKey(msg)
	}

	// Resolver lookup: when wired, the resolver is the single source of truth.
	if action, ok := m.resolver.Resolve(6, 0, msg); ok {
		return m.dispatchEnvAction(action)
	}
	// Key didn't map to any action — intentional no-op.
	return m, nil
}

// dispatchEnvAction routes a resolver action in the env editor.
func (m Model) dispatchEnvAction(action string) (tea.Model, tea.Cmd) {
	switch action {
	case keybindings.ActionCancel:
		return m.closeEnvEditor(), nil
	case "tab_prev":
		if len(m.envEditor.tabs) > 0 {
			m.envEditor.tabIdx = (m.envEditor.tabIdx - 1 + len(m.envEditor.tabs)) % len(m.envEditor.tabs)
			m = m.loadEnvEditorVars()
		}
		return m, nil
	case "tab_next":
		if len(m.envEditor.tabs) > 0 {
			m.envEditor.tabIdx = (m.envEditor.tabIdx + 1) % len(m.envEditor.tabs)
			m = m.loadEnvEditorVars()
		}
		return m, nil
	case keybindings.ActionNavigateDown:
		if m.envEditor.varCursor < len(m.envEditor.vars)-1 {
			m.envEditor.varCursor++
			m = m.ensureEnvCursorVisible()
		}
		return m, nil
	case keybindings.ActionNavigateUp:
		if m.envEditor.varCursor > 0 {
			m.envEditor.varCursor--
			m = m.ensureEnvCursorVisible()
		}
		return m, nil
	case "add":
		return m.envAddVar()
	case "delete":
		return m.envDeleteVar()
	case "edit":
		return m.envEditVar()
	case "save":
		return m.saveEnvEditor()
	case "create_env":
		return m.envCreateEnv()
	}
	return m, nil
}

func (m Model) envAddVar() (tea.Model, tea.Cmd) {
	m.envEditor.vars = append(m.envEditor.vars, envVar{Saved: false})
	m.envEditor.varCursor = len(m.envEditor.vars) - 1
	m = m.ensureEnvCursorVisible()
	m.envEditor.editing = true
	m.envEditor.editKey = textinput.New()
	m.envEditor.editVal = textinput.New()
	m.envEditor.editKey.Focus()
	return m, textinput.Blink
}

func (m Model) envDeleteVar() (tea.Model, tea.Cmd) {
	if len(m.envEditor.vars) > 0 && m.envEditor.varCursor < len(m.envEditor.vars) {
		m.envEditor.vars = append(
			m.envEditor.vars[:m.envEditor.varCursor],
			m.envEditor.vars[m.envEditor.varCursor+1:]...,
		)
		if m.envEditor.varCursor >= len(m.envEditor.vars) && m.envEditor.varCursor > 0 {
			m.envEditor.varCursor--
		}
		m = m.ensureEnvCursorVisible()
		m.envEditor.dirty = true
	}
	return m, nil
}

func (m Model) envEditVar() (tea.Model, tea.Cmd) {
	if len(m.envEditor.vars) > 0 && m.envEditor.varCursor < len(m.envEditor.vars) {
		m.envEditor.editing = true
		m.envEditor.editKey = textinput.New()
		m.envEditor.editVal = textinput.New()
		m.envEditor.editKey.SetValue(m.envEditor.vars[m.envEditor.varCursor].Key)
		m.envEditor.editVal.SetValue(m.envEditor.vars[m.envEditor.varCursor].Value)
		m.envEditor.editKey.Focus()
		return m, textinput.Blink
	}
	return m, nil
}

func (m Model) envCreateEnv() (tea.Model, tea.Cmd) {
	collectionID := m.activeCollectionID()
	if collectionID == "" {
		return m, nil
	}
	m.mode = collectionPromptMode
	m.promptMode = promptAddEnv
	m.promptTargetID = collectionID
	m.promptInput.SetValue("")
	m.promptInput.Placeholder = "Environment name"
	m.promptInput.Focus()
	return m, textinput.Blink
}

func (m Model) handleEnvEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case m.cfg.Keybindings.EnvEditSwitchField:
		if m.envEditor.editKey.Focused() {
			m.envEditor.editKey.Blur()
			m.envEditor.editVal.Focus()
		} else {
			m.envEditor.editVal.Blur()
			m.envEditor.editKey.Focus()
		}
		return m, nil
	case m.cfg.Keybindings.EnvEditConfirm:
		if m.envEditor.varCursor < len(m.envEditor.vars) {
			m.envEditor.vars[m.envEditor.varCursor] = envVar{
				Key:   m.envEditor.editKey.Value(),
				Value: m.envEditor.editVal.Value(),
				Saved: false,
			}
		}
		m.envEditor.editing = false
		m.envEditor.editKey.Blur()
		m.envEditor.editVal.Blur()
		m.envEditor.dirty = true
		return m, nil
	case m.cfg.Keybindings.EnvCancel:
		m.envEditor.editing = false
		m.envEditor.editKey.Blur()
		m.envEditor.editVal.Blur()
		return m, nil
	}

	var cmd tea.Cmd
	if m.envEditor.editKey.Focused() {
		m.envEditor.editKey, cmd = m.envEditor.editKey.Update(msg)
	} else {
		m.envEditor.editVal, cmd = m.envEditor.editVal.Update(msg)
	}
	return m, cmd
}

func (m Model) ensureEnvCursorVisible() Model {
	m.envEditor.scroll = adjustListViewport(listViewport{
		Scroll:      m.envEditor.scroll,
		SelectedRow: m.envEditor.varCursor,
		TotalRows:   len(m.envEditor.vars),
		VisibleRows: m.envVisibleRows(),
	})
	return m
}

func (m Model) saveEnvEditor() (Model, tea.Cmd) {
	if m.envWriter == nil {
		m.envEditor.saveErr = "Environment writer not available"
		return m, nil
	}

	tab := m.envEditor.tabs[m.envEditor.tabIdx]
	vars := make(map[string]string, len(m.envEditor.vars))
	for _, v := range m.envEditor.vars {
		if v.Key != "" {
			vars[v.Key] = v.Value
		}
	}

	env := &domain.Environment{
		ID:           tab.ID,
		CollectionID: tab.CollectionID,
		Name:         tab.Name,
	}
	env.SetVars(vars)

	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, envDBTimeout)
		defer cancel()
		if err := m.envWriter.SaveEnvironment(ctx, env); err != nil {
			return envSaveErrMsg{err: err}
		}
		return envSavedMsg{}
	}
}

// cycleEnv cycles the active environment for the current collection and returns a
// command that persists the selection to the active-env store.
// direction: +1 for next, -1 for prev.
func (m Model) cycleEnv(direction int) (Model, tea.Cmd) {
	colID := m.activeCollectionID()
	if colID == "" || m.envReader == nil {
		return m, nil
	}

	ctx, cancel := context.WithTimeout(m.ctx, envDBTimeout)
	defer cancel()

	envs, err := m.envReader.ListCollectionEnvironments(ctx, colID)
	if err != nil || len(envs) == 0 {
		return m, nil
	}

	// Find current active env index.
	activeID := m.activeEnv[colID]
	currentIdx := 0
	for i, e := range envs {
		if e.ID == activeID {
			currentIdx = i
			break
		}
	}

	// Cycle.
	newIdx := (currentIdx + direction + len(envs)) % len(envs)
	newEnv := envs[newIdx]
	m.activeEnv[colID] = newEnv.ID
	// Invalidate cached name so next render resolves the new env.
	m.cachedEnvColID = ""
	m.cachedEnvName = newEnv.Name

	var cmd tea.Cmd
	if m.activeEnvStore != nil {
		cmd = func() tea.Msg {
			ctx2, cancel2 := context.WithTimeout(m.ctx, envDBTimeout)
			defer cancel2()
			if err := m.activeEnvStore.SetActiveEnvironment(ctx2, colID, newEnv.ID); err != nil {
				return errLoadMsg{err: fmt.Errorf("save active env: %w", err)}
			}
			return envLoadedMsg{collectionID: colID, envID: newEnv.ID}
		}
	}
	return m, cmd
}

// activeEnvName returns the name of the active environment for the current
// collection. The name is cached on the Model and refreshed lazily when the
// active env or collection changes — this avoids synchronous DB queries in the
// render path (called 60fps from viewRequestPane).
func (m Model) activeEnvName() string {
	colID := m.activeCollectionID()
	if colID == "" || m.envReader == nil {
		return ""
	}

	// Return cached value if still valid.
	if m.cachedEnvColID == colID {
		return m.cachedEnvName
	}

	activeID := m.activeEnv[colID]
	if activeID == "" {
		// Check if there's a default env.
		ctx, cancel := context.WithTimeout(m.ctx, envDBTimeout)
		defer cancel()
		envs, err := m.envReader.ListCollectionEnvironments(ctx, colID)
		if err != nil || len(envs) == 0 {
			m.cachedEnvName = ""
			m.cachedEnvColID = colID
			return ""
		}
		for _, e := range envs {
			if e.Name == defaultEnvironmentName {
				m.cachedEnvName = defaultEnvironmentName
				m.cachedEnvColID = colID
				return defaultEnvironmentName
			}
		}
		m.cachedEnvName = envs[0].Name
		m.cachedEnvColID = colID
		return envs[0].Name
	}

	ctx, cancel := context.WithTimeout(m.ctx, envDBTimeout)
	defer cancel()
	env, err := m.envReader.GetEnvironment(ctx, activeID)
	if err != nil {
		m.cachedEnvName = ""
		m.cachedEnvColID = colID
		return ""
	}
	m.cachedEnvName = env.Name
	m.cachedEnvColID = colID
	return env.Name
}

// resolveEnvVars delegates to exec.ResolveEnvVars, the single source of truth
// for env resolution (also used by the CLI executor's makeVariableResolver).
func resolveEnvVars(
	ctx context.Context,
	envReader store.EnvironmentReader,
	activeEnv map[string]string,
	collectionID string,
) (colEnv, globalEnv map[string]string) {
	if envReader == nil {
		return nil, nil
	}
	return exec.ResolveEnvVars(ctx, envReader, activeEnv[collectionID], collectionID)
}

// dispatchWithEnvCmd dispatches an HTTP request with variable substitution.
func dispatchWithEnvCmd(
	ctx context.Context,
	executor RequestExecutor,
	envReader store.EnvironmentReader,
	activeEnv map[string]string,
	req *domain.Request,
) tea.Cmd {
	return func() tea.Msg {
		if envReader != nil {
			colEnv, globalEnv := resolveEnvVars(ctx, envReader, activeEnv, req.CollectionID)
			if colEnv != nil || globalEnv != nil {
				interpolated, err := exec.InterpolateRequest(req, colEnv, globalEnv)
				if err != nil {
					return httpErrMsg{requestID: req.ID, err: err}
				}
				req = interpolated
			}
		}
		result, err := executor.Execute(ctx, req)
		if err != nil {
			return httpErrMsg{requestID: req.ID, err: err}
		}
		return httpResponseMsg{requestID: req.ID, result: result}
	}
}
