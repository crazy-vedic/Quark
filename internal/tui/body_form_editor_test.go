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
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
	"github.com/crazy-vedic/quark/internal/tui"
)

// bodyFormTestModel opens a model with req saved in a real store and active.
func bodyFormTestModel(t *testing.T, req *domain.Request) (tui.Model, *store.Store) {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "test.db"), store.WithCacheSize(100))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	col := &domain.Collection{ID: uuid.New().String(), Name: "API"}
	require.NoError(t, st.SaveCollection(ctx, col))
	req.ID = uuid.New().String()
	req.CollectionID = col.ID
	req.Name = "Token"
	req.Method = "POST"
	require.NoError(t, st.SaveRequest(ctx, req))

	m := tui.New(
		tui.Deps{Lister: st, Reader: st, Writer: st, Config: config.Default(t.TempDir()), Ctx: ctx},
	)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m = m.WithActiveRequest(req).WithURLValue(req.URL).WithFocus(tui.RequestPane)
	return m, st
}

func bodyFormKey(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

func bodyFormType(t *testing.T, m tui.Model, s string) tui.Model {
	t.Helper()
	for _, r := range s {
		m = callUpdate(t, m, bodyFormKey(r))
	}
	return m
}

func savedRequest(t *testing.T, st *store.Store, id string) *domain.Request {
	t.Helper()
	req, err := st.GetRequest(context.Background(), id)
	require.NoError(t, err)
	return req
}

func TestBodyType_CycleSavesEachType(t *testing.T) {
	req := &domain.Request{URL: "https://api.test/", Body: "x"}
	m, st := bodyFormTestModel(t, req)

	var got []string
	for range 5 {
		m = callUpdate(t, m, bodyFormKey('T'))
		got = append(got, m.ActiveRequest().BodyType)
		assert.Equal(t, "Body type: "+m.ActiveRequest().BodyType, m.StatusSuccess())
	}
	assert.Equal(t, []string{"json", "form-urlencoded", "graphql", "none", "raw"}, got)
	assert.Equal(t, "raw", savedRequest(t, st, req.ID).BodyType)
}

func TestBodyForm_EditsRowsAndSavesEncoded(t *testing.T) {
	req := &domain.Request{
		URL:      "https://api.test/token",
		Body:     "a=1&b=two+words&c=%7B%7Bx%7D%7D",
		BodyType: domain.BodyTypeFormURLEncoded,
	}
	m, st := bodyFormTestModel(t, req)

	m = callUpdate(t, m, bodyFormKey('b'))
	require.Equal(t, tui.BodyFormField, m.ActiveField())
	pairs := m.HeaderPairs()
	require.Len(t, pairs, 3)
	assert.Equal(t, "two words", pairs[1].Value, "values are decoded for editing")
	assert.Equal(t, "{{x}}", pairs[2].Value)
	view := m.View()
	assert.Contains(t, view, "a = 1")
	assert.Contains(t, view, "b = two words")

	// Add a field with a space in the value.
	m = callUpdate(t, m, bodyFormKey('a'))
	require.True(t, m.HeaderEditing())
	m = bodyFormType(t, m, "New")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = bodyFormType(t, m, "x y&z")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, "New", m.HeaderPairs()[3].Key)

	// Confirm: the body is re-encoded and saved.
	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	cmd()
	assert.Equal(t, tui.NoneField, m.ActiveField())
	assert.Equal(t, "Form body updated", m.StatusSuccess())
	saved := savedRequest(t, st, req.ID)
	assert.Equal(t, "a=1&b=two+words&c={{x}}&New=x+y%26z", saved.Body)
	assert.Equal(t, domain.BodyTypeFormURLEncoded, saved.BodyType)
}

func TestBodyForm_EscDiscardsChanges(t *testing.T) {
	req := &domain.Request{
		URL:      "https://api.test/",
		Body:     "a=1&b=2",
		BodyType: domain.BodyTypeFormURLEncoded,
	}
	m, st := bodyFormTestModel(t, req)

	m = callUpdate(t, m, bodyFormKey('b'))
	m = callUpdate(t, m, bodyFormKey('d'))
	assert.Len(t, m.HeaderPairs(), 1)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.NoneField, m.ActiveField())
	assert.Equal(t, "a=1&b=2", savedRequest(t, st, req.ID).Body)
}

func TestBodyForm_TabDoesNotSuggestHeaderNames(t *testing.T) {
	req := &domain.Request{URL: "https://api.test/", BodyType: domain.BodyTypeFormURLEncoded}
	m, _ := bodyFormTestModel(t, req)

	m = callUpdate(t, m, bodyFormKey('b'))
	m = callUpdate(t, m, bodyFormKey('a'))
	m = bodyFormType(t, m, "Con")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, "Con", m.HeaderKeyValue(), "no Content-Type suggestion for a form field")
}

func TestBodyForm_EmptyFormSavesEmptyBody(t *testing.T) {
	req := &domain.Request{
		URL:      "https://api.test/",
		Body:     "a=1",
		BodyType: domain.BodyTypeFormURLEncoded,
	}
	m, st := bodyFormTestModel(t, req)

	m = callUpdate(t, m, bodyFormKey('b'))
	m = callUpdate(t, m, bodyFormKey('d'))
	assert.Contains(t, m.View(), "No form fields")
	_, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	cmd()
	assert.Empty(t, savedRequest(t, st, req.ID).Body)
}

func TestBodyType_NoneShowsNoBodyAndEditsText(t *testing.T) {
	req := &domain.Request{URL: "https://api.test/", Body: "kept", BodyType: domain.BodyTypeNone}
	m, _ := bodyFormTestModel(t, req)

	assert.Contains(t, m.View(), "No body is sent")
	m = callUpdate(t, m, bodyFormKey('b'))
	assert.Equal(t, tui.BodyField, m.ActiveField(), "none edits the plain body text")
}
