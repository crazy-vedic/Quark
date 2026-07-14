package tuitest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/keybindings"
	"github.com/crazy-vedic/quark/internal/search"
	"github.com/crazy-vedic/quark/internal/store"
	"github.com/crazy-vedic/quark/internal/tui"
)

// allowFrameOverflow tracks tests that intentionally exercise overflow.
var allowFrameOverflow sync.Map // map[string]struct{}

// AllowFrameOverflow marks the current test as allowed to render a frame that
// exceeds the terminal (or show the Visual Overflow status). Call once at the
// start of intentional overflow tests.
func AllowFrameOverflow(t *testing.T) {
	t.Helper()
	name := t.Name()
	allowFrameOverflow.Store(name, struct{}{})
	t.Cleanup(func() { allowFrameOverflow.Delete(name) })
}

func frameOverflowAllowed(t *testing.T) bool {
	_, ok := allowFrameOverflow.Load(t.Name())
	return ok
}

// AssertNoFrameOverflow fails if the model's View exceeds its terminal size
// (or shows the Visual Overflow banner), unless AllowFrameOverflow was called.
func AssertNoFrameOverflow(t *testing.T, m tui.Model) {
	t.Helper()
	if frameOverflowAllowed(t) {
		return
	}
	if !m.HasFrameOverflow() {
		return
	}
	view := m.View()
	excerpt := view
	if len(excerpt) > 400 {
		excerpt = excerpt[:400] + "…"
	}
	require.Fail(t, fmt.Sprintf(
		"TUI frame overflow: terminal=%dx%d rendered=%dx%d\nview excerpt:\n%s",
		m.Width(),
		m.Height(),
		lipgloss.Width(view),
		lipgloss.Height(view),
		excerpt,
	))
}

func SetupStore(t *testing.T, collections ...*domain.Collection) *store.Store {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "e2e.db"), store.WithCacheSize(100))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()
	for _, col := range collections {
		if col.ID == "" {
			col.ID = uuid.New().String()
		}
		require.NoError(t, st.SaveCollection(ctx, col))
	}
	return st
}

func SeedRequests(t *testing.T, st *store.Store, colID string, reqs ...*domain.Request) {
	t.Helper()
	ctx := context.Background()
	for _, req := range reqs {
		if req.ID == "" {
			req.ID = uuid.New().String()
		}
		req.CollectionID = colID
		require.NoError(t, st.SaveRequest(ctx, req))
	}
}

type MockExecutor struct {
	Latency time.Duration
}

func (m *MockExecutor) Execute(
	ctx context.Context,
	req *domain.Request,
) (*exec.ExecuteResult, error) {
	if m.Latency > 0 {
		select {
		case <-time.After(m.Latency):
		case <-ctx.Done():
			return nil, exec.ErrRequestCancelled
		}
	}

	method := req.Method
	if method == "" {
		method = "GET"
	}

	var body string
	switch {
	case method == "GET" && strings.Contains(req.URL, "/json"):
		body = `{"status":"ok","id":42}`
	case method == "POST":
		body = `{"created":true}`
	default:
		body = "OK"
	}

	return &exec.ExecuteResult{
		StatusCode: 200,
		Status:     "200 OK",
		Headers:    http.Header{"Content-Type": []string{"application/json"}},
		Body:       []byte(body),
		Duration:   10 * time.Millisecond,
		Size:       int64(len(body)),
	}, nil
}

func RealExecutor(t *testing.T) (*httptest.Server, *exec.Executor) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"path":   r.URL.Path,
			"method": r.Method,
		})
	}))
	t.Cleanup(srv.Close)

	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	return srv, exec.New(transport)
}

func NewModel(t *testing.T, st *store.Store, executor tui.RequestExecutor) tui.Model {
	t.Helper()
	cfg := config.Default("")
	return tui.New(tui.Deps{
		Lister:          st,
		Reader:          st,
		Writer:          st,
		ColWriter:       st,
		ExecutionReader: st,
		EnvReader:       st,
		EnvWriter:       st,
		Executor:        executor,
		Searcher:        search.New(st),
		Importer:        curl.NewImporter(),
		Config:          cfg,
		Ctx:             context.Background(),
		Resolver:        keybindings.NewResolver(cfg.Keybindings),
	})
}

func NewModelWithConfig(
	t *testing.T,
	st *store.Store,
	executor tui.RequestExecutor,
	cfg config.Config,
) tui.Model {
	t.Helper()
	return tui.New(tui.Deps{
		Lister:          st,
		Reader:          st,
		Writer:          st,
		ColWriter:       st,
		ExecutionReader: st,
		EnvReader:       st,
		EnvWriter:       st,
		Executor:        executor,
		Searcher:        search.New(st),
		Importer:        curl.NewImporter(),
		Config:          cfg,
		Ctx:             context.Background(),
		Resolver:        keybindings.NewResolver(cfg.Keybindings),
	})
}

func MergeBindings(dst, src keybindings.Keybindings) keybindings.Keybindings {
	out := dst
	dstValue := reflect.ValueOf(&out).Elem()
	srcValue := reflect.ValueOf(src)
	for i := 0; i < srcValue.NumField(); i++ {
		if srcValue.Field(i).String() == "" {
			continue
		}
		dstValue.Field(i).SetString(srcValue.Field(i).String())
	}
	return out
}

func NewModelWithBindings(t *testing.T, binds keybindings.Keybindings) tui.Model {
	t.Helper()
	cfg := config.Default("")
	cfg.Keybindings = MergeBindings(cfg.Keybindings, binds)
	return tui.New(tui.Deps{
		Searcher: &search.Searcher{},
		Importer: curl.NewImporter(),
		Config:   cfg,
		Ctx:      context.Background(),
		Resolver: keybindings.NewResolver(cfg.Keybindings),
	})
}

func Update(t *testing.T, m tui.Model, msg tea.Msg) tui.Model {
	t.Helper()
	updated, _ := m.Update(msg)
	model, ok := updated.(tui.Model)
	require.True(t, ok)
	AssertNoFrameOverflow(t, model)
	return model
}

func Resize(t *testing.T, m tui.Model, width, height int) tui.Model {
	t.Helper()
	return Update(t, m, tea.WindowSizeMsg{Width: width, Height: height})
}

func UpdateWithCmd(t *testing.T, m tui.Model, msg tea.Msg) (tui.Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	model, ok := updated.(tui.Model)
	require.True(t, ok)
	AssertNoFrameOverflow(t, model)
	return model, cmd
}

func KeyRunes(runes ...rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: runes}
}

func Key(typeID tea.KeyType) tea.KeyMsg {
	return tea.KeyMsg{Type: typeID}
}

func Click(x, y int) tea.MouseMsg {
	return tea.MouseMsg{
		X:      x,
		Y:      y,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	}
}

func RightClick(x, y int) tea.MouseMsg {
	return tea.MouseMsg{
		X:      x,
		Y:      y,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonRight,
	}
}

func WheelUp(x, y int) tea.MouseMsg {
	return tea.MouseMsg{
		X:      x,
		Y:      y,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelUp,
	}
}

func WheelDown(x, y int) tea.MouseMsg {
	return tea.MouseMsg{
		X:      x,
		Y:      y,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelDown,
	}
}

func RunCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	return cmd()
}

func ExecuteCmdUpdate(t *testing.T, m tui.Model, cmd tea.Cmd) tui.Model {
	t.Helper()
	msg := RunCmd(t, cmd)
	if msg == nil {
		return m
	}
	return Update(t, m, msg)
}

func AssertViewContains(t *testing.T, m tui.Model, want string) {
	t.Helper()
	AssertNoFrameOverflow(t, m)
	assert.Contains(t, m.View(), want, "rendered view must contain %q", want)
}

func AssertViewNotContains(t *testing.T, m tui.Model, want string) {
	t.Helper()
	AssertNoFrameOverflow(t, m)
	assert.NotContains(t, m.View(), want, "rendered view must NOT contain %q", want)
}

func AssertFocus(t *testing.T, m tui.Model, want interface{}) {
	t.Helper()
	assert.Equal(t, want, m.Focus(), "unexpected focused pane")
}

func AssertMode(t *testing.T, m tui.Model, want interface{}) {
	t.Helper()
	assert.Equal(t, want, m.Mode(), "unexpected TUI mode")
}

func AssertActiveField(t *testing.T, m tui.Model, want interface{}) {
	t.Helper()
	assert.Equal(t, want, m.ActiveField(), "unexpected active request field")
}

func AssertResponseTab(t *testing.T, m tui.Model, want interface{}) {
	t.Helper()
	assert.Equal(t, want, m.ResponseTab(), "unexpected response tab")
}

func AssertSearchResultsLen(t *testing.T, m tui.Model, want int) {
	t.Helper()
	assert.Len(t, m.SearchResults(), want, "unexpected number of search results")
}
