//go:build e2e

package tui_test

import (
	"net/http/httptest"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/keybindings"
	"github.com/crazy-vedic/quark/internal/store"
	"github.com/crazy-vedic/quark/internal/tui"
	"github.com/crazy-vedic/quark/internal/tuitest"
)

type mockExecutor = tuitest.MockExecutor

func setupStore(t *testing.T, collections ...*domain.Collection) *store.Store {
	return tuitest.SetupStore(t, collections...)
}

func seedRequests(t *testing.T, st *store.Store, colID string, reqs ...*domain.Request) {
	tuitest.SeedRequests(t, st, colID, reqs...)
}

func realExecutor(t *testing.T) (*httptest.Server, *exec.Executor) {
	return tuitest.RealExecutor(t)
}

func newE2EModel(t *testing.T, st *store.Store, executor tui.RequestExecutor) tui.Model {
	return tuitest.NewModel(t, st, executor)
}

func newE2EModelWithBindings(t *testing.T, binds keybindings.Keybindings) tui.Model {
	return tuitest.NewModelWithBindings(t, binds)
}

func newE2EModelWithConfig(
	t *testing.T,
	st *store.Store,
	executor tui.RequestExecutor,
	cfg config.Config,
) tui.Model {
	return tuitest.NewModelWithConfig(t, st, executor, cfg)
}

func mergeBindings(dst, src keybindings.Keybindings) keybindings.Keybindings {
	return tuitest.MergeBindings(dst, src)
}

func callUpdate(t *testing.T, m tui.Model, msg tea.Msg) tui.Model {
	return tuitest.Update(t, m, msg)
}

func callUpdateWithCmd(t *testing.T, m tui.Model, msg tea.Msg) (tui.Model, tea.Cmd) {
	return tuitest.UpdateWithCmd(t, m, msg)
}

func runCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	return tuitest.RunCmd(t, cmd)
}

func executeCmdUpdate(t *testing.T, m tui.Model, cmd tea.Cmd) tui.Model {
	return tuitest.ExecuteCmdUpdate(t, m, cmd)
}

func assertViewContains(t *testing.T, m tui.Model, want string) {
	tuitest.AssertViewContains(t, m, want)
}

func assertViewNotContains(t *testing.T, m tui.Model, want string) {
	tuitest.AssertViewNotContains(t, m, want)
}
