//go:build e2e

package tui_test

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/tui"
)

// jumpJourneyModel is a 120x40 TUI with one collection holding requestCount
// requests and the first request open. The response pane is empty.
func jumpJourneyModel(t *testing.T, requestCount int) (tui.Model, []*domain.Request) {
	t.Helper()
	col := &domain.Collection{ID: "jump-col", Name: "Jumps"}
	st := setupStore(t, col)
	reqs := make([]*domain.Request, 0, requestCount)
	for i := 0; i < requestCount; i++ {
		reqs = append(reqs, &domain.Request{
			ID:           fmt.Sprintf("jump-req-%02d", i),
			CollectionID: col.ID,
			Name:         fmt.Sprintf("Ping %02d", i),
			Method:       "GET",
			URL:          "https://jump.example",
		})
	}
	seedRequests(t, st, col.ID, reqs...)
	m := newE2EModel(t, st, &mockExecutor{})
	m = resize(t, m, 120, 40)
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, reqs))
	m = m.WithActiveRequest(reqs[0])
	return m, reqs
}

func pressRunes(t *testing.T, m tui.Model, s string) tui.Model {
	t.Helper()
	for _, r := range s {
		m = callUpdate(t, m, tuitestKey(r))
	}
	return m
}

// "g" opens jump mode, which labels each focus target and waits for a label.
// The labels are drawn over the panes, URL, body and sidebar rows in order.
func TestE2E_JumpMode_LabelsTargetsAndCancelsWithEsc(t *testing.T) {
	m, _ := jumpJourneyModel(t, 1)

	m = pressRunes(t, m, "g")
	assertViewContains(t, m, "jump - type a label, esc cancels")
	assertViewContains(t, m, "Ping 00")
	assert.Equal(t, tui.SidebarPane, m.Focus(), "opening jump mode must not move focus")

	// A key that starts no label is ignored and jump mode stays open.
	m = pressRunes(t, m, "z")
	assertViewContains(t, m, "esc cancels")
	assert.Equal(t, tui.SidebarPane, m.Focus())

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.NormalMode, m.Mode())
	assert.Equal(t, tui.SidebarPane, m.Focus(), "Esc cancels without acting")
}

func TestE2E_JumpMode_FocusesPanes(t *testing.T) {
	m, _ := jumpJourneyModel(t, 1)

	// Targets: a sidebar pane, s request pane, d URL, f body, g response.
	m = pressRunes(t, m, "g")
	m = pressRunes(t, m, "s")
	assert.Equal(t, tui.NormalMode, m.Mode())
	assert.Equal(t, tui.RequestPane, m.Focus())

	m = pressRunes(t, m, "g")
	m = pressRunes(t, m, "g")
	assert.Equal(t, tui.ResponsePane, m.Focus())

	m = pressRunes(t, m, "g")
	m = pressRunes(t, m, "a")
	assert.Equal(t, tui.SidebarPane, m.Focus())
}

// The URL and body targets go straight into their editors, as their keys do.
func TestE2E_JumpMode_OpensURLAndBodyEditors(t *testing.T) {
	m, _ := jumpJourneyModel(t, 1)

	m = pressRunes(t, m, "g")
	m = pressRunes(t, m, "d")
	assert.Equal(t, tui.RequestPane, m.Focus())
	assert.Equal(t, tui.URLField, m.ActiveField())

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m = pressRunes(t, m, "g")
	m = pressRunes(t, m, "f")
	assert.Equal(t, tui.RequestPane, m.Focus())
	assert.Equal(t, tui.BodyField, m.ActiveField())
}

// Sidebar rows are labelled too: the collection row and a request row.
func TestE2E_JumpMode_SelectsSidebarRows(t *testing.T) {
	m, reqs := jumpJourneyModel(t, 2)

	// Index order: 0 sidebar, 1 request, 2 URL, 3 body, 4 response, 5 collection
	// row (h), 6 first request row (j), 7 second request row (k).
	m = pressRunes(t, m, "g")
	assertViewContains(t, m, "Ping 01")
	m = pressRunes(t, m, "k")
	assert.Equal(t, tui.NormalMode, m.Mode())
	assert.Equal(t, tui.RequestPane, m.Focus(), "opening a sidebar request hands focus to the request pane")
	require.NotNil(t, m.ActiveRequest())
	assert.Equal(t, reqs[1].ID, m.ActiveRequest().ID, "the labelled request row must open that request")
}

// Response tabs get labels once a response exists.
func TestE2E_JumpMode_ResponseTabs(t *testing.T) {
	m, _ := jumpJourneyModel(t, 1)
	m = callUpdate(t, m, tui.HttpResponseMsg(&exec.ExecuteResult{
		StatusCode: 200,
		Status:     "200 OK",
		Headers:    map[string][]string{"Content-Type": {"text/plain"}},
		Body:       []byte("tab target"),
	}))

	// Index order after the response pane: body (h), headers (j), raw (k).
	m = pressRunes(t, m, "g")
	m = pressRunes(t, m, "k")
	assert.Equal(t, tui.ResponsePane, m.Focus())
	assert.Equal(t, tui.RawTab, m.ResponseTab())

	m = pressRunes(t, m, "g")
	m = pressRunes(t, m, "j")
	assert.Equal(t, tui.HeadersTab, m.ResponseTab())

	m = pressRunes(t, m, "g")
	m = pressRunes(t, m, "h")
	assert.Equal(t, tui.BodyTab, m.ResponseTab())
}

// With more targets than single letters, every label has two letters. A
// first letter waits for its partner, Backspace drops it, and Esc cancels.
func TestE2E_JumpMode_TwoLetterLabels(t *testing.T) {
	m, reqs := jumpJourneyModel(t, 30)

	// 35 targets: index 0 is "aa", index 6 is "aj", index 10 is "aw".
	m = pressRunes(t, m, "g")
	assertViewContains(t, m, "Ping 29")
	m = pressRunes(t, m, "a")
	// A first letter alone shows as the typed prefix and does not act yet.
	assertViewContains(t, m, "jump a - type a label")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	m = pressRunes(t, m, "a")
	m = pressRunes(t, m, "w")
	assert.Equal(t, tui.NormalMode, m.Mode())
	require.NotNil(t, m.ActiveRequest())
	assert.Equal(t, reqs[4].ID, m.ActiveRequest().ID, "label aw is the fifth request row")

	m = pressRunes(t, m, "g")
	m = pressRunes(t, m, "a")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.NormalMode, m.Mode())
	assert.Equal(t, reqs[4].ID, m.ActiveRequest().ID, "Esc after a partial label leaves the request unchanged")
}

// Resizing the terminal while labels are shown closes jump mode, since the
// labels were placed for the old size.
func TestE2E_JumpMode_ResizeClosesLabels(t *testing.T) {
	m, _ := jumpJourneyModel(t, 1)
	m = pressRunes(t, m, "g")
	assertViewContains(t, m, "esc cancels")

	m = resize(t, m, 100, 30)
	assert.Equal(t, tui.NormalMode, m.Mode())
}
