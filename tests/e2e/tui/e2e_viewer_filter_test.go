//go:build e2e

package tui_test

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/tui"
)

const viewerFilterBody = `{"items":[{"name":"alpha","id":1},{"name":"beta","id":2}]}`

// openJSONViewerE2E shows a JSON response with the given body in the fullscreen viewer.
func openJSONViewerE2E(t *testing.T, body string) tui.Model {
	t.Helper()
	st := setupStore(t)
	m := newE2EModel(t, st, &mockExecutor{})
	m = resize(t, m, viewerE2EWidth, viewerE2EHeight)
	m = callUpdate(t, m, tui.HttpResponseMsg(&exec.ExecuteResult{
		StatusCode: 200,
		Status:     "200 OK",
		Headers:    map[string][]string{"Content-Type": {"application/json"}},
		Body:       []byte(body),
	}))
	m = openViewerE2E(t, m)
	require.Equal(t, tui.ViewerMode, m.Mode())
	return m
}

// typeViewerFilter opens the jq input with | and submits text in one edit, so
// the filter runs once. The evaluation command is executed synchronously.
func typeViewerFilter(t *testing.T, m tui.Model, text string) tui.Model {
	t.Helper()
	m = callUpdate(t, m, tuitestKey('|'))
	require.Contains(t, m.View(), "jq:", "| must focus the jq input")
	m, cmd := callUpdateWithCmd(t, m, tuitestRunes(text))
	return settleViewerFilter(t, m, cmd)
}

// settleViewerFilter runs the commands returned by an edit and feeds their
// messages back through Update, the way the program loop would.
func settleViewerFilter(t *testing.T, m tui.Model, cmd tea.Cmd) tui.Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := runCmd(t, cmd)
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		if msg != nil {
			m = callUpdate(t, m, msg)
		}
		return m
	}
	for _, sub := range batch {
		if sub == nil {
			continue
		}
		if next := runCmd(t, sub); next != nil {
			m = callUpdate(t, m, next)
		}
	}
	return m
}

// A jq expression replaces the viewer body with its results. Enter keeps the
// result and hands keys back to the viewer; closing the viewer clears it.
func TestE2E_ViewerFilter_ExpressionReplacesBodyAndIsKept(t *testing.T) {
	m := openJSONViewerE2E(t, viewerFilterBody)

	m = typeViewerFilter(t, m, ".items[].name")
	assert.Contains(t, m.View(), "alpha")
	assert.Contains(t, m.View(), "beta")
	assert.NotContains(t, m.View(), "\"id\"", "fields outside the filter must not be shown")
	assertViewNotContains(t, m, "error:")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assertViewNotContains(t, m, "jq:")
	assert.Equal(t, tui.ViewerMode, m.Mode(), "Enter keeps the viewer open")
	assertViewContains(t, m, "filter: .items[].name")
	assert.Contains(t, m.View(), "alpha")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.NormalMode, m.Mode())
	m = openViewerE2E(t, m)
	assertViewNotContains(t, m, "filter: .items")
}

// Several results are printed one after another, each as its own value.
func TestE2E_ViewerFilter_MultipleResultsPrintOnePerValue(t *testing.T) {
	m := openJSONViewerE2E(t, viewerFilterBody)

	m = typeViewerFilter(t, m, ".items[]")
	assert.Contains(t, m.View(), "alpha")
	assert.Contains(t, m.View(), "beta")
	assert.Contains(t, m.View(), "\"id\"", "each object keeps its fields")
}

// An expression that does not parse keeps the last good result and names the error.
func TestE2E_ViewerFilter_InvalidExpressionKeepsLastResult(t *testing.T) {
	m := openJSONViewerE2E(t, viewerFilterBody)
	m = typeViewerFilter(t, m, ".items[].name")
	require.Contains(t, m.View(), "alpha")

	m, cmd := callUpdateWithCmd(t, m, tuitestRunes(" | ["))
	m = settleViewerFilter(t, m, cmd)
	assertViewContains(t, m, "error:")
	assert.Contains(t, m.View(), "alpha", "the last good result stays on screen while typing")
}

// A query that matches nothing says so instead of showing an empty body.
func TestE2E_ViewerFilter_NoResults(t *testing.T) {
	m := openJSONViewerE2E(t, viewerFilterBody)
	m = typeViewerFilter(t, m, "empty")
	assertViewContains(t, m, "(no results)")
	assertViewNotContains(t, m, "error:")
}

// Bodies that are not a single JSON value cannot be filtered and say why.
func TestE2E_ViewerFilter_NonJSONBodiesAreRejected(t *testing.T) {
	cases := map[string]string{
		"not json":      "plain text body",
		"two documents": `{"a":1} {"b":2}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			m := openJSONViewerE2E(t, body)
			m = typeViewerFilter(t, m, ".a")
			assertViewContains(t, m, "error:")
		})
	}
}

// Esc in the filter input clears the filter and restores the original body.
func TestE2E_ViewerFilter_EscClearsAndRestoresBody(t *testing.T) {
	m := openJSONViewerE2E(t, viewerFilterBody)
	m = typeViewerFilter(t, m, ".items[].name")
	require.NotContains(t, m.View(), "\"id\"")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.ViewerMode, m.Mode(), "Esc clears the filter but keeps the viewer open")
	assertViewNotContains(t, m, "jq:")
	assert.Contains(t, m.View(), "\"id\"", "the unfiltered body is back")
	assertViewNotContains(t, m, "filter: .items")
}

// Deleting the expression back to empty restores the unfiltered body.
func TestE2E_ViewerFilter_EmptyExpressionRestoresBody(t *testing.T) {
	m := openJSONViewerE2E(t, viewerFilterBody)
	m = typeViewerFilter(t, m, ".items[].name")
	require.NotContains(t, m.View(), "\"id\"")

	for range len(".items[].name") {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
		m = settleViewerFilter(t, m, nil)
	}
	assert.Contains(t, m.View(), "\"id\"")
	assertViewNotContains(t, m, "error:")
}

// A result larger than the output cap is refused with the size in the footer.
func TestE2E_ViewerFilter_OversizedResultIsRefused(t *testing.T) {
	m := openJSONViewerE2E(t, viewerFilterBody)
	m = typeViewerFilter(t, m, "range(3000000)")
	assertViewContains(t, m, "error: result exceeds 8 MiB")
}

// A filter that keeps computing without output is stopped at the time limit.
func TestE2E_ViewerFilter_RunawayFilterTimesOut(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for the filter time limit")
	}
	m := openJSONViewerE2E(t, viewerFilterBody)
	started := time.Now()
	m = typeViewerFilter(t, m, "reduce range(1000000000) as $x (0; . + $x)")
	assertViewContains(t, m, "error: filter timed out after 5s")
	assert.Less(t, time.Since(started), 30*time.Second)
}
