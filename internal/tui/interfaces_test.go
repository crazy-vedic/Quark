package tui_test

// Narrow-interface + context-propagation tests (todos 004 + 005).
//
// These tests verify:
// 1. Fake executor/searcher/importer satisfy the narrow tui interfaces
//    and can be injected via Deps without constructing concrete types.
// 2. Search dispatching cancels any stale in-flight search goroutine.
// 3. The model stores and uses the context provided in Deps.

import (
	"context"
	"io"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/search"
	"github.com/crazy-vedic/quark/internal/tui"
)

// --- Fake implementations of the narrow interfaces ---

// fakeExecutor satisfies tui.RequestExecutor (once defined).
type fakeExecutor struct {
	result *exec.ExecuteResult
	err    error
}

func (f *fakeExecutor) Execute(_ context.Context, _ *domain.Request) (*exec.ExecuteResult, error) {
	return f.result, f.err
}

// fakeSearcher satisfies tui.RequestSearcher (once defined).
type fakeSearcher struct {
	result *search.SearchResult
	err    error
	delay  time.Duration
	calls  int
}

func (f *fakeSearcher) Search(_ context.Context, _, _ string) (*search.SearchResult, error) {
	f.calls++
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	return f.result, f.err
}

// fakeImporter satisfies tui.CurlImporter (once defined).
type fakeImporter struct {
	result *curl.ImportResult
	err    error
}

func (f *fakeImporter) Parse(_ io.Reader) (*curl.ImportResult, error) {
	return f.result, f.err
}

// --- Todo 005: Narrow interfaces ---

// TestDeps_AcceptsFakeExecutor verifies that a fake executor (not *exec.Executor)
// can be injected into Deps.Executor. This test FAILS before the interface is
// defined because Deps.Executor is currently typed as *exec.Executor.
func TestDeps_AcceptsFakeExecutor(t *testing.T) {
	fake := &fakeExecutor{result: &exec.ExecuteResult{StatusCode: 201}}
	m := tui.New(tui.Deps{
		Config:   config.Default(t.TempDir()),
		Executor: fake, // would not compile if Executor is *exec.Executor
	})
	assert.NotNil(t, m)
}

// TestDeps_AcceptsFakeSearcher verifies fake searcher injection.
func TestDeps_AcceptsFakeSearcher(t *testing.T) {
	fake := &fakeSearcher{result: &search.SearchResult{}}
	m := tui.New(tui.Deps{
		Config:   config.Default(t.TempDir()),
		Searcher: fake,
	})
	assert.NotNil(t, m)
}

// TestDeps_AcceptsFakeImporter verifies fake importer injection.
func TestDeps_AcceptsFakeImporter(t *testing.T) {
	fake := &fakeImporter{result: &curl.ImportResult{Method: "GET", URL: "https://example.com"}}
	m := tui.New(tui.Deps{
		Config:   config.Default(t.TempDir()),
		Importer: fake,
	})
	assert.NotNil(t, m)
}

// --- Todo 004: Context propagation ---

// TestModel_SearchCancel_CalledBeforeNewSearch verifies that dispatching a
// second search keystroke cancels the previous search's context. This requires
// m.searchCancel to be set and exposed via the TUI test-support API.
func TestModel_SearchCancel_CalledBeforeNewSearch(t *testing.T) {
	m := newModel(defaultConfig()).WithMode(tui.SearchMode)

	// Simulate a pre-existing search cancel func.
	cancelled := false
	m = m.WithSearchCancel(func() { cancelled = true })

	// Dispatch a search keystroke — should cancel the stale search first.
	update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})

	assert.True(
		t,
		cancelled,
		"dispatching a new search must cancel the previous search cancel func",
	)
}

// TestModel_SearchCancel_SetAfterDispatch verifies that after dispatching a
// search, m.searchCancel is non-nil (a cancel func was stored).
func TestModel_SearchCancel_SetAfterDispatch(t *testing.T) {
	m := newModel(defaultConfig()).WithMode(tui.SearchMode)

	updated := update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})

	// searchCancel should now be set since a search was dispatched.
	assert.NotNil(t, updated.SearchCancel(), "searchCancel must be set after dispatching a search")
}

// TestModel_Ctx_StoredFromDeps verifies that Deps.Ctx is stored on the model
// and accessible via export.
func TestModel_Ctx_StoredFromDeps(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := tui.New(tui.Deps{
		Config: config.Default(t.TempDir()),
		Ctx:    ctx,
	})

	assert.Equal(t, ctx, m.ModelCtx(), "m.ctx must equal the context provided in Deps")
}

// TestInit_UsesModelCtx verifies Init() is callable without panic when lister is nil.
// (Existing behavior — regression guard.)
func TestInit_NilLister_ReturnsNil(t *testing.T) {
	m := tui.New(tui.Deps{Config: config.Default(t.TempDir())})
	cmd := m.Init()
	require.Nil(t, cmd, "Init with nil lister must return nil Cmd")
}
