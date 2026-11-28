package tui_test

import (
	"context"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/search"
	"github.com/crazy-vedic/quark/internal/store"
	"github.com/crazy-vedic/quark/internal/tui"
	"github.com/crazy-vedic/quark/internal/tui/textedit"
)

func TestRequestEditors_HeaderKeyOpensHeaderEditor(t *testing.T) {
	dir := t.TempDir()
	st, err := store.New(filepath.Join(dir, "test.db"), store.WithCacheSize(100))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	col := &domain.Collection{ID: uuid.New().String(), Name: "API"}
	req := &domain.Request{
		ID:           uuid.New().String(),
		CollectionID: col.ID,
		Name:         "Create",
		Method:       "POST",
		URL:          "https://example.com",
		Headers:      "{}",
	}
	ctx := context.Background()
	require.NoError(t, st.SaveCollection(ctx, col))
	require.NoError(t, st.SaveRequest(ctx, req))

	m := tui.New(tui.Deps{
		Lister:   st,
		Reader:   st,
		Writer:   st,
		Executor: &exec.Executor{},
		Searcher: &search.Searcher{},
		Importer: curl.NewImporter(),
		Config:   config.Default(t.TempDir()),
		Ctx:      ctx,
	})

	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))
	m = m.WithActiveRequest(req).WithFocus(tui.RequestPane)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	assert.Equal(t, tui.HeadersField, m.ActiveField())

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	assert.Len(t, m.HeaderPairs(), 1)
}

func selectAllKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyCtrlA} }

func TestHeaderKeyCopyCutPaste(t *testing.T) {
	restore := tui.StubClipboardForTest(func() (string, error) { return "PASTE", nil })
	t.Cleanup(restore)

	m := newModel(defaultConfig()).
		WithFocus(tui.RequestPane).
		WithHeaderPairEdit("Accept", "json")

	m = callUpdate(t, m, selectAllKey())
	m2, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	assert.NotNil(t, cmd, "copy writes the selected key")
	assert.Equal(t, "Accept", m2.HeaderKeyValue())

	m3, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlX})
	assert.NotNil(t, cmd, "cut writes the selected key")
	assert.Equal(t, "", m3.HeaderKeyValue())

	m4 := callPaste(t, m, tea.KeyMsg{Type: tea.KeyCtrlV})
	assert.Equal(t, "PASTE", m4.HeaderKeyValue(), "paste replaces the selected key")
}

func TestHeaderValueTypeOverSelection(t *testing.T) {
	m := newModel(defaultConfig()).
		WithFocus(tui.RequestPane).
		WithHeaderPairEdit("Zzz-Custom", "old")
	// Tab moves focus to the value input (no suggestion for this key).
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = callUpdate(t, m, selectAllKey())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("new")})
	assert.Equal(t, "new", m.HeaderValueValue())
	assert.Equal(t, "Zzz-Custom", m.HeaderKeyValue(), "the key is untouched")
}

func TestAuthTokenCopyCutPaste(t *testing.T) {
	restore := tui.StubClipboardForTest(func() (string, error) { return "NEW", nil })
	t.Cleanup(restore)

	m := newModel(defaultConfig()).
		WithFocus(tui.RequestPane).
		WithAuthFieldEdit(domain.AuthTypeBearer, 1, "secret")

	m = callUpdate(t, m, selectAllKey())
	assert.True(t, m.AuthTokenSelection().Active())
	m2, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlX})
	assert.NotNil(t, cmd)
	assert.Equal(t, "", m2.AuthTokenValue())

	m3 := callPaste(t, m2, tea.KeyMsg{Type: tea.KeyCtrlV})
	assert.Equal(t, "NEW", m3.AuthTokenValue())
}

func TestAuthBasicPasswordSelectionAndCopy(t *testing.T) {
	m := newModel(defaultConfig()).
		WithFocus(tui.RequestPane).
		WithAuthFieldEdit(domain.AuthTypeBasic, 2, "hunter2")

	m = callUpdate(t, m, selectAllKey())
	assert.Equal(t, textedit.Selection{Anchor: 0, Cursor: 7}, m.AuthPasswordSelection())
	m2, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	assert.NotNil(t, cmd)
	assert.Equal(t, "hunter2", m2.AuthPasswordValue(), "copy keeps the value")
}

func TestAuthAPIKeyValueTypeOverSelection(t *testing.T) {
	m := newModel(defaultConfig()).
		WithFocus(tui.RequestPane).
		WithAuthFieldEdit(domain.AuthTypeAPIKey, 3, "old")

	m = callUpdate(t, m, selectAllKey())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("new")})
	assert.Equal(t, "new", m.AuthAPIKeyValueValue())
}

func TestHeaderEditorRendersHeaderNotURL(t *testing.T) {
	m := newModel(defaultConfig()).
		WithFocus(tui.RequestPane).
		WithActiveRequest(&domain.Request{ID: "r1", Name: "r", Method: "GET", URL: "https://url.example.test"}).
		WithHeaderPairEdit("Zzz-Custom", "value-here")
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	out := m.View()
	assert.Contains(t, out, "Zzz-Custom", "header key shows in the header editor")
	assert.Contains(t, out, "value-here", "header value shows in the header editor")
}

func typeRunes(t *testing.T, m tui.Model, s string) tui.Model {
	t.Helper()
	for _, r := range s {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func extractTestModel() tui.Model {
	return newModel(defaultConfig()).
		WithFocus(tui.RequestPane).
		WithActiveRequest(&domain.Request{
			ID: "r1", Name: "login", Method: "POST", URL: "https://api.example.test/login",
		})
}

func TestExtractEditorAddsRuleAndSaves(t *testing.T) {
	m := extractTestModel()
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")})
	require.Equal(t, tui.ExtractField, m.ActiveField())

	m = typeRunes(t, m, "a") // add a row
	require.True(t, m.ExtractEditing())
	m = typeRunes(t, m, "token")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = typeRunes(t, m, "$.session.token")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.False(t, m.ExtractEditing())
	require.Len(t, m.ExtractRules(), 1)
	assert.Equal(t, "token", m.ExtractRules()[0].Variable)
	assert.Equal(t, domain.ExtractFromBody, m.ExtractRules()[0].Source)

	m2, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.NotNil(t, cmd, "saving the list persists the request")
	assert.Equal(t, tui.NoneField, m2.ActiveField())
	require.NotNil(t, m2.ActiveRequest())
	assert.Equal(t, domain.EncodeExtractions([]domain.Extraction{
		{Variable: "token", Source: domain.ExtractFromBody, Path: "$.session.token"},
	}), m2.ActiveRequest().Extractions)
}

func TestExtractEditorRejectsInvalidRowAndKeepsItOpen(t *testing.T) {
	m := extractTestModel()
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")})
	m = typeRunes(t, m, "a")
	m = typeRunes(t, m, "tok")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = typeRunes(t, m, "$[")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	assert.True(t, m.ExtractEditing(), "an invalid row stays open")
	assert.NotEmpty(t, m.ExtractError())
}

func TestExtractEditorEscRemovesNewRow(t *testing.T) {
	m := extractTestModel()
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")})
	m = typeRunes(t, m, "a")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})

	assert.False(t, m.ExtractEditing())
	assert.Empty(t, m.ExtractRules(), "cancelling a new row removes it")
	assert.Equal(t, tui.ExtractField, m.ActiveField(), "the list stays open")
}

func TestExtractEditorEscInListDiscardsChanges(t *testing.T) {
	m := extractTestModel()
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")})
	m = typeRunes(t, m, "a")
	m = typeRunes(t, m, "tok")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = typeRunes(t, m, "$.tok")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.Len(t, m.ExtractRules(), 1)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})

	assert.Equal(t, tui.NoneField, m.ActiveField())
	assert.Empty(t, m.ExtractRules())
	assert.Empty(t, m.ActiveRequest().Extractions, "nothing was saved")
}

func TestAssertEditorAddsRuleAndSaves(t *testing.T) {
	m := extractTestModel()
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("A")})
	require.Equal(t, tui.AssertField, m.ActiveField())

	m = typeRunes(t, m, "a")
	require.True(t, m.AssertEditing())
	m = typeRunes(t, m, "status = 200")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.False(t, m.AssertEditing())
	require.Len(t, m.AssertRules(), 1)
	assert.Equal(t, domain.AssertStatus, m.AssertRules()[0].Kind)

	m2, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.NotNil(t, cmd, "saving the list persists the request")
	assert.Equal(t, tui.NoneField, m2.ActiveField())
	assert.Equal(t, domain.EncodeAssertions([]domain.Assertion{
		{Kind: domain.AssertStatus, Op: "=", Value: "200"},
	}), m2.ActiveRequest().Assertions)
}

func TestAssertEditorRejectsInvalidRowAndKeepsItOpen(t *testing.T) {
	m := extractTestModel()
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("A")})
	m = typeRunes(t, m, "a")
	m = typeRunes(t, m, "status = 99")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	assert.True(t, m.AssertEditing(), "an invalid row stays open")
	assert.NotEmpty(t, m.AssertError())
}

func TestAssertEditorEscRemovesNewRow(t *testing.T) {
	m := extractTestModel()
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("A")})
	m = typeRunes(t, m, "a")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})

	assert.False(t, m.AssertEditing())
	assert.Empty(t, m.AssertRules())
	assert.Equal(t, tui.AssertField, m.ActiveField())
}

func TestAssertEditorEscInListDiscardsChanges(t *testing.T) {
	m := extractTestModel()
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("A")})
	m = typeRunes(t, m, "a")
	m = typeRunes(t, m, "time < 1s")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.Len(t, m.AssertRules(), 1)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})

	assert.Equal(t, tui.NoneField, m.ActiveField())
	assert.Empty(t, m.ActiveRequest().Assertions, "nothing was saved")
}
