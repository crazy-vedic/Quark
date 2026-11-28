//go:build e2e

package tui_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/store"
	"github.com/crazy-vedic/quark/internal/tui"
)

// extractFixture is a TUI model with one request selected and its request
// pane focused. Sending it runs the real executor with the extraction hook,
// so extracted values are written to the collection's default environment.
type extractFixture struct {
	m   tui.Model
	st  *store.Store
	col *domain.Collection
	req *domain.Request
}

func newExtractFixture(
	t *testing.T,
	handler http.HandlerFunc,
	rules ...domain.Extraction,
) extractFixture {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)

	col := &domain.Collection{ID: "col-extract", Name: "Extract"}
	st := setupStore(t, col)
	executor := exec.New(transport,
		exec.WithPostResponseHooks([]exec.PostResponseHook{exec.NewExtractionHook(st)}))

	req := &domain.Request{
		ID:           "req-extract",
		CollectionID: col.ID,
		Name:         "Login",
		Method:       "GET",
		URL:          srv.URL + "/data",
		Extractions:  domain.EncodeExtractions(rules),
	}
	seedRequests(t, st, col.ID, req)
	m := newE2EModel(t, st, executor)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	var cmd tea.Cmd
	m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if msg := runCmd(t, cmd); msg != nil {
		m = callUpdate(t, m, msg)
	}
	require.Equal(t, tui.RequestPane, m.Focus())
	return extractFixture{m: m, st: st, col: col, req: req}
}

// press sends one key and applies the message its command produces.
func (f *extractFixture) press(t *testing.T, key tea.KeyMsg) {
	t.Helper()
	var cmd tea.Cmd
	f.m, cmd = callUpdateWithCmd(t, f.m, key)
	f.m = applyQuick(t, f.m, cmd)
}

// applyQuick applies the message of cmd if it is ready shortly. Commands that
// only time something (status expiry, cursor blink) are not needed by these
// checks, so they are left pending instead of being waited for.
func applyQuick(t *testing.T, m tui.Model, cmd tea.Cmd) tui.Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	ready := make(chan tea.Msg, 1)
	go func() { ready <- cmd() }()
	select {
	case msg := <-ready:
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, sub := range batch {
				m = applyQuick(t, m, sub)
			}
			return m
		}
		if msg != nil {
			return callUpdate(t, m, msg)
		}
	case <-time.After(100 * time.Millisecond):
	}
	return m
}

func (f *extractFixture) typeText(t *testing.T, text string) {
	t.Helper()
	for _, r := range text {
		f.press(t, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

// drainSend runs the send command and every command it returns until none is
// left, so the whole response and its follow-up messages are applied.
func drainSend(t *testing.T, m tui.Model, cmd tea.Cmd) tui.Model {
	t.Helper()
	for cmd != nil {
		msg := runCmd(t, cmd)
		if msg == nil {
			break
		}
		m, cmd = callUpdateWithCmd(t, m, msg)
	}
	return m
}

func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

// defaultVars returns the variables of the collection's default environment.
func (f *extractFixture) defaultVars(t *testing.T) map[string]string {
	t.Helper()
	envs, err := f.st.ListEnvironments(context.Background(), f.col.ID)
	require.NoError(t, err)
	for _, env := range envs {
		if env.Name == "default" {
			vars, err := env.DecodeVars()
			require.NoError(t, err)
			return vars
		}
	}
	require.FailNow(t, "no default environment")
	return nil
}

func TestE2E_ExtractionEditorAddEditDeleteAndSave(t *testing.T) {
	f := newExtractFixture(t, func(w http.ResponseWriter, _ *http.Request) {})

	f.press(t, runeKey('X'))
	assertViewContains(t, f.m, "No extraction rules")

	f.press(t, runeKey('a'))
	f.typeText(t, "token")
	f.press(t, tea.KeyMsg{Type: tea.KeyTab})
	f.typeText(t, "$.data.token")
	f.press(t, tea.KeyMsg{Type: tea.KeyEnter})
	assertViewContains(t, f.m, "token = $.data.token")

	f.press(t, runeKey('a'))
	f.typeText(t, "id")
	f.press(t, tea.KeyMsg{Type: tea.KeyTab})
	f.typeText(t, "header X-Req-Id")
	f.press(t, tea.KeyMsg{Type: tea.KeyEnter})
	assertViewContains(t, f.m, "id = header X-Req-Id")

	// Edit the first row, then cancel: the rule is unchanged.
	f.press(t, runeKey('k'))
	f.press(t, runeKey('e'))
	assertViewContains(t, f.m, "Variable:")
	f.press(t, tea.KeyMsg{Type: tea.KeyEsc})
	assertViewContains(t, f.m, "token = $.data.token")

	// Delete the second row.
	f.press(t, runeKey('j'))
	f.press(t, runeKey('d'))
	assertViewNotContains(t, f.m, "id = header X-Req-Id")

	// Enter in the list saves the rules to the request.
	f.press(t, tea.KeyMsg{Type: tea.KeyEnter})
	assertViewContains(t, f.m, "Extraction rules updated")
	saved, err := f.st.GetRequest(context.Background(), f.req.ID)
	require.NoError(t, err)
	rules, err := domain.ParseExtractions(saved.Extractions)
	require.NoError(t, err)
	assert.Equal(t, []domain.Extraction{
		{Variable: "token", Source: domain.ExtractFromBody, Path: "$.data.token"},
	}, rules)
}

func TestE2E_ExtractionEditorRejectsInvalidRows(t *testing.T) {
	f := newExtractFixture(t, func(w http.ResponseWriter, _ *http.Request) {})

	f.press(t, runeKey('X'))
	f.press(t, runeKey('a'))
	f.typeText(t, "bad name")
	f.press(t, tea.KeyMsg{Type: tea.KeyTab})
	f.typeText(t, "$.data")
	f.press(t, tea.KeyMsg{Type: tea.KeyEnter})
	assertViewContains(t, f.m, "must not contain spaces")
	// Esc cancels the row that was just added, so it is removed.
	f.press(t, tea.KeyMsg{Type: tea.KeyEsc})
	assertViewContains(t, f.m, "No extraction rules")

	f.press(t, runeKey('a'))
	f.typeText(t, "ok")
	f.press(t, tea.KeyMsg{Type: tea.KeyTab})
	f.typeText(t, "$.[")
	f.press(t, tea.KeyMsg{Type: tea.KeyEnter})
	assertViewContains(t, f.m, "invalid path")
	f.press(t, tea.KeyMsg{Type: tea.KeyEsc})

	f.press(t, runeKey('a'))
	f.typeText(t, "h")
	f.press(t, tea.KeyMsg{Type: tea.KeyTab})
	f.typeText(t, "header")
	f.press(t, tea.KeyMsg{Type: tea.KeyEnter})
	assertViewContains(t, f.m, "header name is missing")
	f.press(t, tea.KeyMsg{Type: tea.KeyEsc})

	// Leaving the editor with Esc keeps the request unchanged.
	f.press(t, tea.KeyMsg{Type: tea.KeyEsc})
	saved, err := f.st.GetRequest(context.Background(), f.req.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.EncodeExtractions(nil), saved.Extractions)
}

func TestE2E_ExtractionEditorRefusesStoredRulesThatDoNotParse(t *testing.T) {
	f := newExtractFixture(t, func(w http.ResponseWriter, _ *http.Request) {})
	f.req.Extractions = "not json"

	f.press(t, runeKey('X'))
	assertViewContains(t, f.m, "refusing to edit")
}

func TestE2E_ExtractionEditorNeedsASelectedRequest(t *testing.T) {
	col := &domain.Collection{ID: "col-empty", Name: "Empty"}
	st := setupStore(t, col)
	m := newE2EModel(t, st, nil)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	assertViewContains(t, m, "Select a request first")
}

func TestE2E_SendingRunsExtractionsAndReportsSuccess(t *testing.T) {
	f := newExtractFixture(t,
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"token":"tok-9"},"list":[1,2]}`))
		},
		domain.Extraction{Variable: "token", Source: domain.ExtractFromBody, Path: "$.data.token"},
		domain.Extraction{Variable: "last", Source: domain.ExtractFromBody, Path: "$.list[-1]"},
	)

	var cmd tea.Cmd
	f.m, cmd = callUpdateWithCmd(t, f.m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	require.True(t, f.m.Loading())
	f.m = drainSend(t, f.m, cmd)

	assertViewContains(t, f.m, "Extracted 2 variable(s)")
	vars := f.defaultVars(t)
	assert.Equal(t, "tok-9", vars["token"])
	assert.Equal(t, "2", vars["last"])
}

func TestE2E_SendingReportsExtractionFailure(t *testing.T) {
	f := newExtractFixture(t,
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{}}`))
		},
		domain.Extraction{Variable: "missing", Source: domain.ExtractFromBody, Path: "$.data.token"},
	)

	var cmd tea.Cmd
	f.m, cmd = callUpdateWithCmd(t, f.m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	f.m = drainSend(t, f.m, cmd)

	assertViewContains(t, f.m, "Extraction failed: missing:")
	_, written := f.defaultVars(t)["missing"]
	assert.False(t, written)
}

func TestE2E_SendingSkipsExtractionForErrorStatus(t *testing.T) {
	f := newExtractFixture(t,
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"data":{"token":"nope"}}`))
		},
		domain.Extraction{Variable: "token", Source: domain.ExtractFromBody, Path: "$.data.token"},
	)

	var cmd tea.Cmd
	f.m, cmd = callUpdateWithCmd(t, f.m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	f.m = drainSend(t, f.m, cmd)

	assertViewContains(t, f.m, "Extraction skipped: response was not 2xx")
	_, written := f.defaultVars(t)["token"]
	assert.False(t, written)
}

func TestE2E_SendingExtractsHeaderValues(t *testing.T) {
	f := newExtractFixture(t,
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Req-Id", "req-5")
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(strings.Repeat("x", 8)))
		},
		domain.Extraction{Variable: "rid", Source: domain.ExtractFromHeader, Path: "X-Req-Id"},
		domain.Extraction{Variable: "gone", Source: domain.ExtractFromHeader, Path: "X-Gone"},
	)

	var cmd tea.Cmd
	f.m, cmd = callUpdateWithCmd(t, f.m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	f.m = drainSend(t, f.m, cmd)

	assertViewContains(t, f.m, "Extraction failed: gone:")
	assert.Equal(t, "req-5", f.defaultVars(t)["rid"])
}
