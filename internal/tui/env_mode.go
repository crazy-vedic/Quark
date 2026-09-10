package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

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
	OwnerPath    string
	ReadOnly     bool
}

type envDraft struct {
	vars      []envVar
	varCursor int
	scroll    int
	dirty     bool
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
	drafts    map[string]envDraft
	childPath string
}

// envSavedMsg is sent when an environment is saved successfully.
type envSavedMsg struct{ envID string }

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

	// Load the validated root-to-child hierarchy shared with runtime resolution.
	scopes, err := exec.LoadEnvironmentHierarchy(ctx, m.envReader, colID)
	if err != nil {
		m = m.status("error", fmt.Sprintf("Load env hierarchy: %v", err))
		return m, nil
	}

	// Build tabs: Global, each ancestor root-to-parent, then child-owned tabs.
	tabs := []envTab{
		{ID: globalEnv.ID, Name: "Global", IsGlobal: true, OwnerPath: "Global", ReadOnly: true},
	}
	pathParts := make([]string, 0, len(scopes))
	for scopeIndex, scope := range scopes {
		pathParts = append(pathParts, scope.Collection.Name)
		ownerPath := strings.Join(pathParts, "/")
		readOnly := scopeIndex != len(scopes)-1
		for _, e := range scope.Environments {
			tabs = append(tabs, envTab{
				ID:           e.ID,
				Name:         e.Name,
				CollectionID: e.CollectionID,
				OwnerPath:    ownerPath,
				ReadOnly:     readOnly,
			})
		}
	}

	// Select the active env tab.
	activeID := m.activeEnv[colID]
	tabIdx := 0
	for i, t := range tabs {
		if !t.ReadOnly && t.ID == activeID {
			tabIdx = i
			break
		}
	}

	m.mode = envMode
	m.envEditor = envEditor{
		active:    true,
		tabs:      tabs,
		tabIdx:    tabIdx,
		drafts:    make(map[string]envDraft),
		childPath: strings.Join(pathParts, "/"),
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
	if draft, ok := m.envEditor.drafts[tab.ID]; ok {
		m.envEditor.vars = cloneEnvVars(draft.vars)
		m.envEditor.varCursor = draft.varCursor
		m.envEditor.scroll = draft.scroll
		m.envEditor.dirty = draft.dirty
		m.envEditor.saveErr = ""
		return m
	}
	ctx, cancel := context.WithTimeout(m.ctx, envDBTimeout)
	defer cancel()

	env, err := m.envReader.GetEnvironment(ctx, tab.ID)
	if err != nil {
		m.envEditor.saveErr = fmt.Sprintf("Load env: %v", err)
		return m
	}

	vars, err := env.DecodeVars()
	if err != nil {
		m.envEditor.saveErr = fmt.Sprintf("Load env: %v", err)
		return m
	}
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
	m = m.stashEnvDraft()
	return m
}

func cloneEnvVars(vars []envVar) []envVar {
	return append([]envVar(nil), vars...)
}

func (m Model) stashEnvDraft() Model {
	if !m.envEditor.active || len(m.envEditor.tabs) == 0 || m.envEditor.tabIdx >= len(m.envEditor.tabs) {
		return m
	}
	if m.envEditor.drafts == nil {
		m.envEditor.drafts = make(map[string]envDraft)
	}
	tab := m.envEditor.tabs[m.envEditor.tabIdx]
	m.envEditor.drafts[tab.ID] = envDraft{
		vars: cloneEnvVars(m.envEditor.vars), varCursor: m.envEditor.varCursor,
		scroll: m.envEditor.scroll, dirty: m.envEditor.dirty,
	}
	return m
}

func (m Model) switchEnvTab(index int) Model {
	m = m.stashEnvDraft()
	m.envEditor.tabIdx = index
	return m.loadEnvEditorVars()
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
			m = m.switchEnvTab((m.envEditor.tabIdx - 1 + len(m.envEditor.tabs)) % len(m.envEditor.tabs))
		}
		return m, nil
	case "tab_next":
		if len(m.envEditor.tabs) > 0 {
			m = m.switchEnvTab((m.envEditor.tabIdx + 1) % len(m.envEditor.tabs))
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
	case deleteToken:
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
	if m.currentEnvTabReadOnly() {
		return m.readOnlyEnvWarning("add variables"), nil
	}
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
	if m.currentEnvTabReadOnly() {
		return m.readOnlyEnvWarning("delete variables"), nil
	}
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
	if m.currentEnvTabReadOnly() {
		return m.copyInheritedEnvVar()
	}
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

func (m Model) currentEnvTabReadOnly() bool {
	return len(m.envEditor.tabs) == 0 || m.envEditor.tabs[m.envEditor.tabIdx].ReadOnly
}

func (m Model) readOnlyEnvWarning(action string) Model {
	tab := m.envEditor.tabs[m.envEditor.tabIdx]
	return m.status("warn", fmt.Sprintf("Cannot %s in read-only env %q", action, envTabLabel(tab)))
}

func envTabLabel(tab envTab) string {
	if tab.IsGlobal {
		return "Global"
	}
	return tab.OwnerPath + "/" + tab.Name
}

func (m Model) copyInheritedEnvVar() (tea.Model, tea.Cmd) {
	if len(m.envEditor.vars) == 0 || m.envEditor.varCursor >= len(m.envEditor.vars) {
		return m, nil
	}
	inherited := m.envEditor.vars[m.envEditor.varCursor]
	childID := m.activeCollectionID()
	defaultIndex := -1
	for i, tab := range m.envEditor.tabs {
		if !tab.ReadOnly && tab.CollectionID == childID && tab.Name == defaultEnvironmentName {
			defaultIndex = i
			break
		}
	}
	if defaultIndex < 0 {
		if m.envWriter == nil {
			return m.status("error", "Environment writer not available"), nil
		}
		ctx, cancel := context.WithTimeout(m.ctx, envDBTimeout)
		defer cancel()
		created, err := m.envWriter.CreateDefaultEnvironment(ctx, childID)
		if err != nil {
			return m.status("error", fmt.Sprintf("Create child default: %v", err)), nil
		}
		m.envEditor.tabs = append(m.envEditor.tabs, envTab{
			ID: created.ID, Name: created.Name, CollectionID: childID,
			OwnerPath: m.envEditor.childPath,
		})
		defaultIndex = len(m.envEditor.tabs) - 1
	}

	m = m.stashEnvDraft()
	defaultTab := m.envEditor.tabs[defaultIndex]
	if _, ok := m.envEditor.drafts[defaultTab.ID]; !ok {
		currentIndex := m.envEditor.tabIdx
		m.envEditor.tabIdx = defaultIndex
		m = m.loadEnvEditorVars()
		m.envEditor.tabIdx = currentIndex
		m = m.loadEnvEditorVars()
	}
	defaultDraft := m.envEditor.drafts[defaultTab.ID]
	for _, variable := range defaultDraft.vars {
		if variable.Key == inherited.Key {
			return m.status("warn", fmt.Sprintf("Key %q already exists in env %q", inherited.Key, envTabLabel(defaultTab))), nil
		}
	}

	m = m.switchEnvTab(defaultIndex)
	m.envEditor.vars = append(m.envEditor.vars, envVar{Key: inherited.Key, Value: inherited.Value, Saved: false})
	m.envEditor.varCursor = len(m.envEditor.vars) - 1
	m.envEditor.dirty = true
	m = m.ensureEnvCursorVisible()
	m.envEditor.editing = true
	m.envEditor.editKey = textinput.New()
	m.envEditor.editVal = textinput.New()
	m.envEditor.editKey.SetValue(inherited.Key)
	m.envEditor.editVal.SetValue(inherited.Value)
	m.envEditor.editKey.Focus()
	m = m.status("success", fmt.Sprintf("Copied key %q to env %q", inherited.Key, envTabLabel(defaultTab)))
	return m, textinput.Blink
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
	if tab.ReadOnly {
		return m.readOnlyEnvWarning("save changes"), nil
	}
	if tab.CollectionID != m.activeCollectionID() {
		return m.status("warn", "Environment ownership changed; reopen the modal before saving"), nil
	}
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
		return envSavedMsg{envID: tab.ID}
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
	envReader EnvironmentHierarchyReader,
	_ map[string]string,
	collectionID string,
) (colEnv, globalEnv map[string]string, err error) {
	if envReader == nil {
		return nil, nil, nil
	}
	return exec.ResolveEnvVars(ctx, envReader, collectionID)
}

// dispatchWithEnvCmd dispatches an HTTP request with variable substitution.
func dispatchWithEnvCmd(
	ctx context.Context,
	executor RequestExecutor,
	envReader EnvironmentHierarchyReader,
	activeEnv map[string]string,
	req *domain.Request,
) tea.Cmd {
	return func() tea.Msg {
		if envReader != nil {
			colEnv, globalEnv, err := resolveEnvVars(ctx, envReader, activeEnv, req.CollectionID)
			if err != nil {
				return httpErrMsg{requestID: req.ID, err: fmt.Errorf("resolve environments: %w", err)}
			}
			interpolated, err := exec.InterpolateRequest(req, colEnv, globalEnv)
			if err != nil {
				return httpErrMsg{requestID: req.ID, err: err}
			}
			req = interpolated
		}
		result, err := executor.Execute(ctx, req)
		if err != nil {
			return httpErrMsg{requestID: req.ID, err: err}
		}
		return httpResponseMsg{requestID: req.ID, result: result}
	}
}
