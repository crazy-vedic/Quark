//go:build e2e

package tui_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/domain"
	quarkexec "github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/keybindings"
	"github.com/crazy-vedic/quark/internal/store"
	"github.com/crazy-vedic/quark/internal/tui"
)

type tuiEnvironmentFixture struct {
	root          *domain.Collection
	parent        *domain.Collection
	child         *domain.Collection
	global        *domain.Environment
	rootDefault   *domain.Environment
	rootActive    *domain.Environment
	parentDefault *domain.Environment
	parentActive  *domain.Environment
	childDefault  *domain.Environment
	childActive   *domain.Environment
}

func seedTUIEnvironmentFixture(t *testing.T, st *store.Store) tuiEnvironmentFixture {
	t.Helper()
	ctx := context.Background()
	fixture := tuiEnvironmentFixture{
		root:   &domain.Collection{ID: "tui-root", Name: "Root"},
		parent: &domain.Collection{ID: "tui-parent", Name: "Parent", ParentID: "tui-root"},
		child:  &domain.Collection{ID: "tui-child", Name: "Child", ParentID: "tui-parent"},
	}
	for _, collection := range []*domain.Collection{fixture.root, fixture.parent, fixture.child} {
		require.NoError(t, st.SaveCollection(ctx, collection))
	}
	var err error
	fixture.global, err = st.GetGlobalEnvironment(ctx)
	require.NoError(t, err)
	fixture.rootDefault, err = st.GetEnvironmentByName(ctx, fixture.root.ID, "default")
	require.NoError(t, err)
	fixture.parentDefault, err = st.GetEnvironmentByName(ctx, fixture.parent.ID, "default")
	require.NoError(t, err)
	fixture.childDefault, err = st.GetEnvironmentByName(ctx, fixture.child.ID, "default")
	require.NoError(t, err)
	fixture.rootActive = saveTUIEnvironment(t, st, "tui-root-dev", fixture.root.ID, "dev", map[string]string{"root_active": "root-dev-secret"})
	fixture.parentActive = saveTUIEnvironment(t, st, "tui-parent-dev", fixture.parent.ID, "dev", map[string]string{"parent_active": "parent-dev-secret"})
	fixture.childActive = saveTUIEnvironment(t, st, "tui-child-dev", fixture.child.ID, "dev", map[string]string{"child_active": "child-dev-secret"})
	require.NoError(t, st.SetActiveEnvironment(ctx, fixture.child.ID, fixture.childActive.ID))
	return fixture
}

func saveTUIEnvironment(
	t *testing.T,
	st *store.Store,
	id, collectionID, name string,
	vars map[string]string,
) *domain.Environment {
	t.Helper()
	environment := &domain.Environment{ID: id, CollectionID: collectionID, Name: name}
	environment.SetVars(vars)
	require.NoError(t, st.SaveEnvironment(context.Background(), environment))
	return environment
}

func setTUIEnvironmentVars(t *testing.T, st *store.Store, environment *domain.Environment, vars map[string]string) {
	t.Helper()
	environment.SetVars(vars)
	require.NoError(t, st.SaveEnvironment(context.Background(), environment))
}

func openNestedEnvironmentEditor(
	t *testing.T,
	st *store.Store,
	fixture tuiEnvironmentFixture,
	activeID string,
) tui.Model {
	t.Helper()
	collections := []*domain.Collection{fixture.root, fixture.parent, fixture.child}
	m := newE2EModel(t, st, &mockExecutor{}).
		WithCollections(collections).
		WithColCursor(2)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 180, Height: 42})
	if activeID != "" {
		m.ActiveEnv()[fixture.child.ID] = activeID
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	require.Equal(t, tui.EnvMode, m.Mode())
	return m
}

func TestE2E_EnvEditor_NestedTabsAreReadOnlyAndDoNotChangeChildActive(t *testing.T) {
	st := setupStore(t)
	fixture := seedTUIEnvironmentFixture(t, st)
	setTUIEnvironmentVars(t, st, fixture.global, map[string]string{"global_key": "global-secret"})
	setTUIEnvironmentVars(t, st, fixture.rootDefault, map[string]string{"root_key": "root-secret"})
	setTUIEnvironmentVars(t, st, fixture.parentDefault, map[string]string{"parent_key": "parent-secret"})
	setTUIEnvironmentVars(t, st, fixture.childDefault, map[string]string{"child_key": "child-secret"})

	m := openNestedEnvironmentEditor(t, st, fixture, fixture.childActive.ID)
	assert.Equal(t, 6, m.EnvEditorTabIdx(), "active child env must be selected")
	assertViewContains(t, m, "Root/Parent/Child/dev")

	// Browse to the parent dev tab and attempt every mutating action.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyLeft}) // child default
	assertViewContains(t, m, "Root/Parent/Child/default")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyLeft}) // parent dev
	require.Equal(t, 4, m.EnvEditorTabIdx())
	assertViewContains(t, m, "Root/Parent/dev (read-only)")
	parentBefore, err := st.GetEnvironment(context.Background(), fixture.parentActive.ID)
	require.NoError(t, err)
	beforeVars := parentBefore.Vars()
	for _, action := range []rune{'a', 'd', 's'} {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{action}})
		assert.Contains(t, m.StatusErr(), "read-only")
		assert.False(t, m.EnvEditorEditing())
		assert.Equal(t, 4, m.EnvEditorTabIdx())
	}

	parentAfter, err := st.GetEnvironment(context.Background(), fixture.parentActive.ID)
	require.NoError(t, err)
	assert.Equal(t, beforeVars, parentAfter.Vars())
	assert.Equal(t, fixture.childActive.ID, m.ActiveEnv()[fixture.child.ID])
	persistedActive, err := st.GetActiveEnvironment(context.Background(), fixture.child.ID)
	require.NoError(t, err)
	assert.Equal(t, fixture.childActive.ID, persistedActive)

	// Every inherited owner/path label remains visible when its tab is selected,
	// even when the one-line tab viewport cannot show the whole hierarchy.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	assertViewContains(t, m, "Root/Parent/default (read-only)")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	assertViewContains(t, m, "Root/default (read-only)")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	assertViewContains(t, m, "Global (read-only)")

	// Creating a new environment remains a child-scoped action even while an
	// inherited tab is selected.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	assert.Equal(t, tui.CollectionPromptMode, m.Mode())
	assert.Equal(t, tui.PromptAddEnv, m.PromptMode())
}

func TestE2E_EnvEditor_CopyOnEditFromEveryInheritedScope(t *testing.T) {
	sources := []struct {
		name      string
		tabIndex  int
		key       string
		value     string
		configure func(*testing.T, *store.Store, tuiEnvironmentFixture)
	}{
		{
			name: "Global", tabIndex: 0, key: "from_global", value: "global-copy-secret",
			configure: func(t *testing.T, st *store.Store, f tuiEnvironmentFixture) {
				setTUIEnvironmentVars(t, st, f.global, map[string]string{"from_global": "global-copy-secret"})
			},
		},
		{
			name: "root", tabIndex: 1, key: "from_root", value: "root-copy-secret",
			configure: func(t *testing.T, st *store.Store, f tuiEnvironmentFixture) {
				setTUIEnvironmentVars(t, st, f.rootDefault, map[string]string{"from_root": "root-copy-secret"})
			},
		},
		{
			name: "parent", tabIndex: 3, key: "from_parent", value: "parent-copy-secret",
			configure: func(t *testing.T, st *store.Store, f tuiEnvironmentFixture) {
				setTUIEnvironmentVars(t, st, f.parentDefault, map[string]string{"from_parent": "parent-copy-secret"})
			},
		},
	}

	for _, source := range sources {
		t.Run(source.name, func(t *testing.T) {
			st := setupStore(t)
			fixture := seedTUIEnvironmentFixture(t, st)
			source.configure(t, st, fixture)
			m := openNestedEnvironmentEditor(t, st, fixture, "")
			for m.EnvEditorTabIdx() < source.tabIndex {
				m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight})
			}

			m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
			assert.Equal(t, 5, m.EnvEditorTabIdx(), "copy must switch to child default")
			assert.True(t, m.EnvEditorEditing())
			vars := m.EnvEditorVars()
			require.Len(t, vars, 1)
			assert.Equal(t, source.key, vars[0].Key)
			assert.Equal(t, source.value, vars[0].Value)
			assert.False(t, vars[0].Saved)
			assert.Contains(t, m.StatusSuccess(), `Copied key "`+source.key+`"`)
			assert.NotContains(t, m.StatusSuccess(), source.value)

			// Cancel only the input sub-mode; the unsaved copied draft remains and
			// can be explicitly saved.
			m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
			var cmd tea.Cmd
			m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
			require.NotNil(t, cmd)
			m = callUpdate(t, m, runCmd(t, cmd))
			assert.Empty(t, m.EnvEditorSaveErr())

			childDefault, err := st.GetEnvironment(context.Background(), fixture.childDefault.ID)
			require.NoError(t, err)
			assert.Equal(t, source.value, childDefault.Vars()[source.key])
		})
	}
}

func TestE2E_EnvEditor_CopyOnEditCollisionDoesNotCopySwitchOrLeakValue(t *testing.T) {
	st := setupStore(t)
	fixture := seedTUIEnvironmentFixture(t, st)
	setTUIEnvironmentVars(t, st, fixture.parentDefault, map[string]string{"collision": "parent-collision-secret"})
	setTUIEnvironmentVars(t, st, fixture.childDefault, map[string]string{"collision": "child-collision-secret"})
	m := openNestedEnvironmentEditor(t, st, fixture, "")
	for m.EnvEditorTabIdx() < 3 {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight})
	}

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	assert.Equal(t, 3, m.EnvEditorTabIdx())
	assert.False(t, m.EnvEditorEditing())
	assert.Equal(t, `Key "collision" already exists in env "Root/Parent/Child/default"`, m.StatusErr())
	assert.NotContains(t, m.StatusErr(), "parent-collision-secret")
	assert.NotContains(t, m.StatusErr(), "child-collision-secret")
	childDefault, err := st.GetEnvironment(context.Background(), fixture.childDefault.ID)
	require.NoError(t, err)
	assert.Equal(t, "child-collision-secret", childDefault.Vars()["collision"])
}

func TestE2E_EnvEditor_CopyCreatesMissingChildDefault(t *testing.T) {
	st := setupStore(t)
	fixture := seedTUIEnvironmentFixture(t, st)
	setTUIEnvironmentVars(t, st, fixture.parentDefault, map[string]string{"copy_me": "copy-secret"})
	require.NoError(t, st.DeleteEnvironment(context.Background(), fixture.childDefault.ID))
	m := openNestedEnvironmentEditor(t, st, fixture, "")
	for m.EnvEditorTabIdx() < 3 {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight})
	}

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	assert.True(t, m.EnvEditorEditing())
	vars := m.EnvEditorVars()
	require.Len(t, vars, 1)
	assert.Equal(t, "copy_me", vars[0].Key)
	created, err := st.GetEnvironmentByName(context.Background(), fixture.child.ID, "default")
	require.NoError(t, err)
	assert.Empty(t, created.Vars(), "copy remains a draft until the user saves")
}

func TestE2E_EnvEditor_PerTabDraftsSurviveNavigationWithoutPersisting(t *testing.T) {
	st := setupStore(t)
	fixture := seedTUIEnvironmentFixture(t, st)
	m := openNestedEnvironmentEditor(t, st, fixture, "")
	for m.EnvEditorTabIdx() < 5 {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight})
	}

	addDraft := func(key, value string) {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
		for _, r := range key {
			m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
		for _, r := range value {
			m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	}

	addDraft("default_draft", "default-secret")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight})
	addDraft("dev_draft", "dev-secret")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	require.Len(t, m.EnvEditorVars(), 1)
	assert.Equal(t, "default_draft", m.EnvEditorVars()[0].Key)
	assert.False(t, m.EnvEditorVars()[0].Saved)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight})
	require.Len(t, m.EnvEditorVars(), 2) // persisted child_active + draft
	assert.Equal(t, "dev_draft", m.EnvEditorVars()[1].Key)
	assert.False(t, m.EnvEditorVars()[1].Saved)

	persistedDefault, err := st.GetEnvironment(context.Background(), fixture.childDefault.ID)
	require.NoError(t, err)
	assert.Empty(t, persistedDefault.Vars())
	persistedDev, err := st.GetEnvironment(context.Background(), fixture.childActive.ID)
	require.NoError(t, err)
	assert.NotContains(t, persistedDev.Vars(), "dev_draft")
}

func TestE2E_EnvEditor_StaleHierarchySaveCanOnlyWriteChild(t *testing.T) {
	st := setupStore(t)
	fixture := seedTUIEnvironmentFixture(t, st)
	setTUIEnvironmentVars(t, st, fixture.parentDefault, map[string]string{"protected": "parent-secret"})
	m := openNestedEnvironmentEditor(t, st, fixture, "")
	for m.EnvEditorTabIdx() < 5 {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	for _, r := range "child_only" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	for _, r := range "child-secret" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	// Reparent the child while the modal still holds the old path label.
	require.NoError(t, st.MoveCollection(context.Background(), fixture.child.ID, fixture.root.ID))
	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	require.NotNil(t, cmd)
	m = callUpdate(t, m, runCmd(t, cmd))
	assert.Empty(t, m.EnvEditorSaveErr())
	parent, err := st.GetEnvironment(context.Background(), fixture.parentDefault.ID)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"protected": "parent-secret"}, parent.Vars())
	child, err := st.GetEnvironment(context.Background(), fixture.childDefault.ID)
	require.NoError(t, err)
	assert.Equal(t, "child-secret", child.Vars()["child_only"])
}

func TestE2E_TUIDirectSend_UsesNestedInheritanceAndBlocksResolutionFailure(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		assert.Equal(t, "/tui/child-dev", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)

	st := setupStore(t)
	fixture := seedTUIEnvironmentFixture(t, st)
	setTUIEnvironmentVars(t, st, fixture.global, map[string]string{"shared": "global"})
	setTUIEnvironmentVars(t, st, fixture.rootDefault, map[string]string{"shared": "root-default"})
	setTUIEnvironmentVars(t, st, fixture.rootActive, map[string]string{"shared": "root-dev"})
	setTUIEnvironmentVars(t, st, fixture.parentDefault, map[string]string{"shared": "parent-default"})
	setTUIEnvironmentVars(t, st, fixture.parentActive, map[string]string{"shared": "parent-dev"})
	setTUIEnvironmentVars(t, st, fixture.childDefault, map[string]string{"shared": "child-default"})
	setTUIEnvironmentVars(t, st, fixture.childActive, map[string]string{"shared": "child-dev"})
	req := &domain.Request{ID: "tui-send", CollectionID: fixture.child.ID, Name: "Send", Method: "GET", URL: srv.URL + "/tui/{{shared}}"}
	require.NoError(t, st.SaveRequest(context.Background(), req))
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	executor := quarkexec.New(transport)
	m := newE2EModel(t, st, executor).
		WithCollections([]*domain.Collection{fixture.root, fixture.parent, fixture.child}).
		WithColCursor(2).
		WithFocus(tui.RequestPane)
	m, _ = m.SelectRequest(req)
	m.ActiveEnv()[fixture.child.ID] = fixture.childActive.ID

	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	require.NotNil(t, cmd)
	m = callUpdate(t, m, runCmd(t, cmd))
	assert.Equal(t, int32(1), hits.Load())
	require.NotNil(t, m.Response())
	assert.Equal(t, http.StatusOK, m.Response().StatusCode)

	// A missing default in the already-open store is a hard resolution error.
	require.NoError(t, st.DeleteEnvironment(context.Background(), fixture.parentDefault.ID))
	m = m.WithFocus(tui.RequestPane)
	m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	require.NotNil(t, cmd)
	m = callUpdate(t, m, runCmd(t, cmd))
	assert.Equal(t, int32(1), hits.Load(), "resolution failure must block TUI dispatch")
	require.Error(t, m.Err())
	assert.Contains(t, m.Err().Error(), "missing default environment")
}

func TestE2E_TUIScheduledSend_UsesNestedInheritanceAndPersistsFailure(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		assert.Equal(t, "/scheduled/child-dev", r.URL.Path)
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)

	ctx := context.Background()
	st := setupStore(t)
	fixture := seedTUIEnvironmentFixture(t, st)
	setTUIEnvironmentVars(t, st, fixture.rootDefault, map[string]string{"shared": "root-default"})
	setTUIEnvironmentVars(t, st, fixture.parentDefault, map[string]string{"shared": "parent-default"})
	setTUIEnvironmentVars(t, st, fixture.childDefault, map[string]string{"shared": "child-default"})
	setTUIEnvironmentVars(t, st, fixture.childActive, map[string]string{"shared": "child-dev"})
	req := &domain.Request{ID: "tui-scheduled", CollectionID: fixture.child.ID, Name: "Scheduled", Method: "GET", URL: srv.URL + "/scheduled/{{shared}}"}
	require.NoError(t, st.SaveRequest(ctx, req))
	fixedNow := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	require.NoError(t, st.SaveScheduledRun(ctx, &domain.ScheduledRun{
		ID: "tui-scheduled-success", RequestID: req.ID,
		RunAt: fixedNow.Add(-time.Minute), Status: domain.ScheduledRunPending,
	}))

	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	executor := quarkexec.New(transport)
	cfg := config.Default("")
	m := tui.New(tui.Deps{
		Lister: st, Reader: st, ExecutionReader: st, EnvReader: st,
		ActiveEnvStore: st, Scheduler: st, Executor: executor,
		Config: cfg, Resolver: keybindings.NewResolver(cfg.Keybindings),
		Ctx: ctx, Now: func() time.Time { return fixedNow },
	}).WithScheduleTimerSeq(10)

	updated, cmd := m.Update(tui.ScheduledRunWakeMsg(10))
	require.NotNil(t, cmd)
	model, ok := updated.(tui.Model)
	require.True(t, ok)
	model = callUpdate(t, model, runCmd(t, cmd))
	assert.Equal(t, int32(1), hits.Load())
	run, err := st.GetScheduledRun(ctx, "tui-scheduled-success")
	require.NoError(t, err)
	assert.Equal(t, domain.ScheduledRunCompleted, run.Status)

	_, err = st.DB().ExecContext(ctx,
		`UPDATE environments SET data = '{"broken":false}' WHERE id = ?`, fixture.parentDefault.ID)
	require.NoError(t, err)
	require.NoError(t, st.SaveScheduledRun(ctx, &domain.ScheduledRun{
		ID: "tui-scheduled-failure", RequestID: req.ID,
		RunAt: fixedNow.Add(-time.Minute), Status: domain.ScheduledRunPending,
	}))
	model = model.WithScheduleTimerSeq(11)
	updated, cmd = model.Update(tui.ScheduledRunWakeMsg(11))
	require.NotNil(t, cmd)
	model, ok = updated.(tui.Model)
	require.True(t, ok)
	model = callUpdate(t, model, runCmd(t, cmd))
	assert.Equal(t, int32(1), hits.Load())
	assert.Contains(t, model.StatusErr(), "invalid environment data")
	run, err = st.GetScheduledRun(ctx, "tui-scheduled-failure")
	require.NoError(t, err)
	assert.Equal(t, domain.ScheduledRunFailed, run.Status)
	assert.Contains(t, run.LastError, "resolve environments")
}

func TestE2E_EnvEditor_HighScaleUnicodeHierarchyFitsEveryLayout(t *testing.T) {
	st := setupStore(t)
	root := &domain.Collection{ID: "scale-root", Name: "根-Extremely-Long-Root-Collection"}
	parent := &domain.Collection{ID: "scale-parent", Name: "父-Extremely-Long-Parent-Collection", ParentID: root.ID}
	child := &domain.Collection{ID: "scale-child", Name: "子-Extremely-Long-Child-Collection", ParentID: parent.ID}
	for _, collection := range []*domain.Collection{root, parent, child} {
		require.NoError(t, st.SaveCollection(context.Background(), collection))
	}
	for _, collection := range []*domain.Collection{root, parent, child} {
		for i := 0; i < 24; i++ {
			saveTUIEnvironment(t, st,
				fmt.Sprintf("%s-env-%02d", collection.ID, i), collection.ID,
				fmt.Sprintf("环境-%02d-with-a-long-name", i), map[string]string{"marker": fmt.Sprintf("value-%02d", i)})
		}
	}
	parentDefault, err := st.GetEnvironmentByName(context.Background(), parent.ID, "default")
	require.NoError(t, err)
	manyVars := make(map[string]string, 250)
	for i := 0; i < 250; i++ {
		manyVars[fmt.Sprintf("unicode_变量_%03d", i)] = strings.Repeat("値", 20)
	}
	setTUIEnvironmentVars(t, st, parentDefault, manyVars)

	m := newE2EModel(t, st, &mockExecutor{}).
		WithCollections([]*domain.Collection{root, parent, child}).
		WithColCursor(2)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 180, Height: 48})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	require.Equal(t, tui.EnvMode, m.Mode())
	for i := 0; i < 26; i++ { // parent default after Global + root's 25 tabs
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight})
	}
	require.Len(t, m.EnvEditorVars(), 250)

	for _, size := range []struct{ width, height int }{
		{180, 48}, {110, 30}, {72, 22}, {44, 14}, {20, 8}, {8, 4},
	} {
		m = resize(t, m, size.width, size.height)
		_ = m.View() // harness assertions verify no frame overflow.
	}
}
