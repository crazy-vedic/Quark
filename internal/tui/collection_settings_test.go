package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/domain"
)

// fakeCollectionDefaults records the defaults saved from the modal.
type fakeCollectionDefaults struct {
	calls []string
	saved []*domain.Collection
	err   error
}

func (f *fakeCollectionDefaults) SetCollectionDefaults(
	_ context.Context,
	id string,
	c *domain.Collection,
) error {
	f.calls = append(f.calls, id)
	if f.err != nil {
		return f.err
	}
	f.saved = append(f.saved, c)
	return nil
}

func collectionSettingsModel(
	t *testing.T,
	col *domain.Collection,
	writer *fakeCollectionDefaults,
) Model {
	t.Helper()
	m := New(Deps{CollectionDefaults: writer, Config: config.Default(t.TempDir())})
	m.collections = []*domain.Collection{col}
	m.colCursor = 0
	return m.openCollectionSettings()
}

func pressCollKey(t *testing.T, m Model, msg tea.KeyMsg) Model {
	t.Helper()
	next, _ := m.handleCollectionSettingsKey(msg)
	model, ok := next.(Model)
	require.True(t, ok, "the modal returns a Model")
	return model
}

func typeRunes(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m = pressCollKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

var (
	keyEnter = tea.KeyMsg{Type: tea.KeyEnter}
	keyEsc   = tea.KeyMsg{Type: tea.KeyEsc}
	keyRight = tea.KeyMsg{Type: tea.KeyRight}
	keyDown  = tea.KeyMsg{Type: tea.KeyDown}
)

func TestCollectionSettings_OpensWithStoredDefaults(t *testing.T) {
	writer := &fakeCollectionDefaults{}
	m := collectionSettingsModel(t, &domain.Collection{
		ID: "col", Name: "Payments", AuthType: domain.AuthTypeBearer,
		AuthConfig: `{"token":"t"}`,
		Headers:    `{"X-Team":["core"],"Accept":"application/json"}`,
	}, writer)

	assert.Equal(t, collectionSettingsMode, m.mode)
	assert.Equal(t, domain.AuthTypeBearer, m.authEditor.authType)
	require.Len(t, m.collSettings.headers, 2)
	assert.Equal(t, "Accept", m.collSettings.headers[0].Key, "headers are listed by name")
	assert.Equal(t, "X-Team", m.collSettings.headers[1].Key)
}

func TestCollectionSettings_EmptyCollectionShowsInherit(t *testing.T) {
	m := collectionSettingsModel(
		t,
		&domain.Collection{ID: "col", Name: "c"},
		&fakeCollectionDefaults{},
	)
	assert.Equal(t, domain.AuthTypeInherit, m.authEditor.authType)
	assert.Empty(t, m.collSettings.headers)
}

func TestCollectionSettings_SaveWritesAuthAndHeaders(t *testing.T) {
	writer := &fakeCollectionDefaults{}
	m := collectionSettingsModel(t, &domain.Collection{ID: "col", Name: "c"}, writer)

	m = pressCollKey(t, m, keyRight) // Inherit -> None (Inherit is last in the cycle)
	m = pressCollKey(t, m, keyRight) // None -> Bearer
	require.Equal(t, domain.AuthTypeBearer, m.authEditor.authType)
	m = pressCollKey(t, m, keyDown)  // token row
	m = pressCollKey(t, m, keyEnter) // start editing the token
	require.True(t, m.collSettings.editing)
	m = typeRunes(t, m, "tok")
	m = pressCollKey(t, m, keyEnter)
	require.False(t, m.collSettings.editing)

	// Add a header: key, then value.
	m = pressCollKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	require.True(t, m.collSettings.editing)
	m = typeRunes(t, m, "X-Team")
	m = pressCollKey(t, m, keyEnter)
	m = typeRunes(t, m, "core")
	m = pressCollKey(t, m, keyEnter)
	require.Len(t, m.collSettings.headers, 1)

	m = pressCollKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	require.Equal(t, []string{"col"}, writer.calls)
	saved := writer.saved[0]
	assert.Equal(t, domain.AuthTypeBearer, saved.AuthType)
	assert.JSONEq(t, `{"token":"tok"}`, saved.AuthConfig)
	assert.JSONEq(t, `{"X-Team":["core"]}`, saved.Headers)
	assert.Equal(t, normalMode, m.mode)
}

func TestCollectionSettings_NoneIsStoredExplicitly(t *testing.T) {
	writer := &fakeCollectionDefaults{}
	m := collectionSettingsModel(t, &domain.Collection{ID: "col", Name: "c"}, writer)
	m = pressCollKey(t, m, keyRight) // Inherit -> None
	require.Equal(t, domain.AuthTypeNone, m.authEditor.authType)
	m = pressCollKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})

	require.Len(t, writer.saved, 1)
	assert.Equal(t, "none", writer.saved[0].AuthType,
		"an empty collection auth type means inherit, so none must be stored explicitly")
}

func TestCollectionSettings_EscDiscardsDraft(t *testing.T) {
	writer := &fakeCollectionDefaults{}
	m := collectionSettingsModel(t, &domain.Collection{ID: "col", Name: "c"}, writer)
	m = pressCollKey(t, m, keyRight)
	m = pressCollKey(t, m, keyEsc)

	assert.Equal(t, normalMode, m.mode)
	assert.Empty(t, writer.calls)
}

func TestCollectionSettings_EscCancelsNewHeader(t *testing.T) {
	writer := &fakeCollectionDefaults{}
	m := collectionSettingsModel(t, &domain.Collection{ID: "col", Name: "c"}, writer)
	m = pressCollKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	require.Len(t, m.collSettings.headers, 1)
	m = pressCollKey(t, m, keyEsc)

	assert.Empty(t, m.collSettings.headers, "a blank header added and cancelled is removed")
	assert.False(t, m.collSettings.editing)
}

func TestCollectionSettings_DeleteHeader(t *testing.T) {
	writer := &fakeCollectionDefaults{}
	m := collectionSettingsModel(t, &domain.Collection{
		ID: "col", Name: "c", Headers: `{"X-One":"1","X-Two":"2"}`,
	}, writer)
	m = pressCollKey(t, m, keyDown) // first header row (auth has one row: the type)
	require.Equal(t, 1, m.collSettings.cursor)
	m = pressCollKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	require.Len(t, m.collSettings.headers, 1)
	assert.Equal(t, "X-Two", m.collSettings.headers[0].Key)
}

func TestCollectionSettings_SaveFailureShowsError(t *testing.T) {
	writer := &fakeCollectionDefaults{err: assert.AnError}
	m := collectionSettingsModel(t, &domain.Collection{ID: "col", Name: "c"}, writer)
	m = pressCollKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})

	assert.Equal(t, collectionSettingsMode, m.mode)
	assert.Contains(t, m.collSettings.err, assert.AnError.Error())
}

func TestCollectionSettings_ParseHeadersAcceptsSingleValues(t *testing.T) {
	h, err := parseCollectionHeaders(`{"A":"1","B":["2","3"]}`)
	require.NoError(t, err)
	assert.Equal(t, []string{"1"}, h.Values("A"))
	assert.Equal(t, []string{"2", "3"}, h.Values("B"))

	_, err = parseCollectionHeaders(`{`)
	require.Error(t, err)
}
