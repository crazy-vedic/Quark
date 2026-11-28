//go:build e2e

package tui_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/store"
	"github.com/crazy-vedic/quark/internal/tui"
)

func keyPress(t *testing.T, m tui.Model, key tea.KeyType) tui.Model {
	return callUpdate(t, m, tea.KeyMsg{Type: key})
}

func runePress(t *testing.T, m tui.Model, s string) tui.Model {
	for _, r := range s {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

// sendRequest sends req from the request pane with Alt+Enter and
// returns the model with the response, or with the error shown instead.
func sendRequest(t *testing.T, m tui.Model, req *domain.Request) tui.Model {
	m = m.WithFocus(tui.RequestPane).
		WithActiveRequest(req).
		WithMethod(req.Method).
		WithURLValue(req.URL)
	model, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	return executeCmdUpdate(t, model, cmd)
}

// seedCollection saves a collection and returns its store.
func seedCollection(t *testing.T, cols ...*domain.Collection) *store.Store {
	t.Helper()
	st := setupStore(t, cols...)
	return st
}

// startSettings opens the collection settings modal from the sidebar.
func startSettings(t *testing.T, st *store.Store, collections ...*domain.Collection) tui.Model {
	t.Helper()
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	m := newE2EModel(t, st, exec.New(transport))
	m = resize(t, m, 130, 42)
	m = callUpdate(t, m, tui.CollectionsLoadedMsg(collections))
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	assertViewContains(t, m, "Collection settings:")
	return m
}

// The collection settings modal sets the collection's auth and default
// headers. A request set to inherit then sends them.
func TestE2E_CollectionSettings_SavedDefaultsAreSentByInheritingRequests(t *testing.T) {
	collection := &domain.Collection{ID: "settings-api", Name: "Settings API"}
	st := seedCollection(t, collection)
	ctx := context.Background()

	var gotAuth, gotTeam string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotTeam = r.Header.Get("X-Team")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	req := &domain.Request{
		ID:           uuid.NewString(),
		CollectionID: collection.ID,
		Name:         "inherits",
		Method:       http.MethodGet,
		URL:          srv.URL + "/inherits",
		AuthType:     domain.AuthTypeInherit,
	}
	require.NoError(t, st.SaveRequest(ctx, req))

	m := startSettings(t, st, collection)
	assertViewContains(t, m, "Inherit")
	m = keyPress(t, m, tea.KeyRight) // Inherit -> None
	m = keyPress(t, m, tea.KeyRight) // None -> Bearer
	assertViewContains(t, m, "Bearer")
	m = keyPress(t, m, tea.KeyDown)  // token row
	m = keyPress(t, m, tea.KeyEnter) // edit the token
	m = runePress(t, m, "team-token")
	m = keyPress(t, m, tea.KeyEnter)
	assertViewNotContains(t, m, "team-token")

	m = runePress(t, m, "a") // add a header: key, then value
	m = runePress(t, m, "X-Team")
	m = keyPress(t, m, tea.KeyEnter)
	m = runePress(t, m, "core")
	m = keyPress(t, m, tea.KeyEnter)
	assertViewContains(t, m, "X-Team")

	m = runePress(t, m, "s")
	assertViewContains(t, m, `Saved defaults for "Settings API"`)

	saved, err := st.GetCollection(ctx, collection.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.AuthTypeBearer, saved.AuthType)
	assert.JSONEq(t, `{"token":"team-token"}`, saved.AuthConfig)
	assert.JSONEq(t, `{"X-Team":["core"]}`, saved.Headers)

	m = sendRequest(t, m, req)
	require.NotNil(t, m.Response())
	assert.Equal(t, "Bearer team-token", gotAuth)
	assert.Equal(t, "core", gotTeam)
}

// Defaults flow down a collection tree. A child that inherits takes the
// parent's auth and headers and adds its own header, and a collection set to
// none stops the request from carrying auth.
func TestE2E_CollectionDefaultsInheritDownTheTree(t *testing.T) {
	root := &domain.Collection{
		ID: "root-col", Name: "Root", AuthType: domain.AuthTypeBearer,
		AuthConfig: `{"token":"root-token"}`, Headers: `{"Accept":["application/json"]}`,
	}
	child := &domain.Collection{
		ID: "child-col", ParentID: root.ID, Name: "Child", AuthType: domain.AuthTypeInherit,
		Headers: `{"X-Child":"yes"}`,
	}
	bare := &domain.Collection{ID: "bare-col", ParentID: root.ID, Name: "Bare", AuthType: "none"}
	st := seedCollection(t, root, child, bare)
	ctx := context.Background()

	var gotAuth, gotAccept, gotChild string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		gotChild = r.Header.Get("X-Child")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	inChild := &domain.Request{
		ID: uuid.NewString(), CollectionID: child.ID, Name: "in-child", Method: http.MethodGet,
		URL: srv.URL + "/child", AuthType: domain.AuthTypeInherit,
	}
	inBare := &domain.Request{
		ID: uuid.NewString(), CollectionID: bare.ID, Name: "in-bare", Method: http.MethodGet,
		URL: srv.URL + "/bare", AuthType: domain.AuthTypeInherit,
	}
	require.NoError(t, st.SaveRequest(ctx, inChild))
	require.NoError(t, st.SaveRequest(ctx, inBare))

	m := startSettings(t, st, root, child, bare)
	m = keyPress(t, m, tea.KeyEsc) // close the modal before sending
	m = sendRequest(t, m, inChild)
	require.NotNil(t, m.Response())
	assert.Equal(t, "Bearer root-token", gotAuth, "inherit takes the nearest ancestor's auth")
	assert.Equal(t, "application/json", gotAccept, "headers flow down from the parent")
	assert.Equal(t, "yes", gotChild, "the child's own header is added")

	gotAuth = "unset"
	m = sendRequest(t, m, inBare)
	require.NotNil(t, m.Response())
	assert.Empty(t, gotAuth, "a collection set to none stops inherited auth")
}

// Esc and a cancelled add leave the saved defaults as they were.
func TestE2E_CollectionSettings_CancelKeepsSavedDefaults(t *testing.T) {
	collection := &domain.Collection{
		ID: "cancel-col", Name: "Cancel", AuthType: domain.AuthTypeBearer,
		AuthConfig: `{"token":"keep"}`, Headers: `{"Accept":["application/json"]}`,
	}
	st := seedCollection(t, collection)
	ctx := context.Background()

	m := startSettings(t, st, collection)
	m = keyPress(t, m, tea.KeyDown) // token row
	m = keyPress(t, m, tea.KeyDown) // Accept header
	m = keyPress(t, m, tea.KeyEnter)
	m = runePress(t, m, "X-Replaced")
	m = keyPress(t, m, tea.KeyEsc)
	assertViewContains(t, m, "Accept")
	assertViewNotContains(t, m, "X-Replaced")

	m = runePress(t, m, "a") // a new header, open for its key
	m = keyPress(t, m, tea.KeyEsc)
	assertViewNotContains(t, m, "(new)")

	m = keyPress(t, m, tea.KeyEsc) // close without saving
	assertViewNotContains(t, m, "Collection settings:")

	saved, err := st.GetCollection(ctx, collection.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.AuthTypeBearer, saved.AuthType)
	assert.JSONEq(t, `{"token":"keep"}`, saved.AuthConfig)
	assert.JSONEq(t, `{"Accept":["application/json"]}`, saved.Headers)
}

// Deleting a header and switching the type to none saves the draft.
func TestE2E_CollectionSettings_DeleteHeaderAndSaveNone(t *testing.T) {
	collection := &domain.Collection{
		ID: "edit-col", Name: "Edit", AuthType: domain.AuthTypeBearer,
		AuthConfig: `{"token":"old"}`, Headers: `{"Accept":["application/json"],"X-Drop":["gone"]}`,
	}
	st := seedCollection(t, collection)
	ctx := context.Background()

	m := startSettings(t, st, collection)
	assertViewContains(t, m, "X-Drop")
	m = keyPress(t, m, tea.KeyDown)
	m = keyPress(t, m, tea.KeyDown) // Accept header
	m = keyPress(t, m, tea.KeyDown) // X-Drop header
	m = runePress(t, m, "d")
	assertViewNotContains(t, m, "X-Drop")

	m = keyPress(t, m, tea.KeyUp)
	m = keyPress(t, m, tea.KeyUp)
	m = keyPress(t, m, tea.KeyUp) // back to the type row
	m = keyPress(t, m, tea.KeyLeft)
	assertViewContains(t, m, "None")

	m = runePress(t, m, "s")
	assertViewContains(t, m, `Saved defaults for "Edit"`)
	saved, err := st.GetCollection(ctx, collection.ID)
	require.NoError(t, err)
	assert.Equal(t, "none", saved.AuthType, "None is stored explicitly, not as inherit")
	assert.JSONEq(t, `{"Accept":["application/json"]}`, saved.Headers)
}

// A collection with no defaults shows Inherit and saves it back as inherit.
// Unknown collection IDs are reported by the store.
func TestE2E_CollectionSettings_InheritIsStoredAndUnknownIDsFail(t *testing.T) {
	collection := &domain.Collection{ID: "plain-col", Name: "Plain"}
	st := seedCollection(t, collection)
	ctx := context.Background()

	m := startSettings(t, st, collection)
	assertViewContains(t, m, "Inherit")
	m = runePress(t, m, "s")
	assertViewContains(t, m, `Saved defaults for "Plain"`)
	saved, err := st.GetCollection(ctx, collection.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.AuthTypeInherit, saved.AuthType)

	err = st.SetCollectionDefaults(ctx, "no-such-collection", &domain.Collection{AuthType: "none"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, store.ErrNotFound))
}

// With no collection selected the modal does not open.
func TestE2E_CollectionSettings_NeedsASelectedCollection(t *testing.T) {
	st := seedCollection(t)
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	m := newE2EModel(t, st, exec.New(transport))
	m = resize(t, m, 130, 42)
	m = callUpdate(t, m, tui.CollectionsLoadedMsg(nil))
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	assertViewContains(t, m, "Select a collection first")
	assertViewNotContains(t, m, "Collection settings:")
}

// Stored default headers that cannot be parsed stop a request that inherits
// them, and the request is never sent.
func TestE2E_CollectionDefaultsWithBadHeadersStopTheRequest(t *testing.T) {
	collection := &domain.Collection{
		ID: "bad-col", Name: "Bad", AuthType: domain.AuthTypeNone, Headers: `{"X-Bad":{"nested":true}}`,
	}
	st := seedCollection(t, collection)
	ctx := context.Background()

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	req := &domain.Request{
		ID: uuid.NewString(), CollectionID: collection.ID, Name: "bad", Method: http.MethodGet,
		URL: srv.URL + "/bad",
	}
	require.NoError(t, st.SaveRequest(ctx, req))

	m := startSettings(t, st, collection)
	m = keyPress(t, m, tea.KeyEsc)
	m = sendRequest(t, m, req)
	assertViewContains(t, m, "collection defaults")
	assert.Nil(t, m.Response())
	assert.Equal(t, 0, hits, "the request must not be sent with unparseable defaults")
}

// An inherit auth type that was never resolved is refused by the executor.
func TestE2E_ExecutorRefusesUnresolvedInheritAuth(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
	}))
	t.Cleanup(srv.Close)
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)

	_, err := exec.New(transport).Execute(context.Background(), &domain.Request{
		ID: uuid.NewString(), Name: "raw", Method: http.MethodGet, URL: srv.URL,
		AuthType: domain.AuthTypeInherit,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth is inherited but collection defaults were not applied")
	assert.Equal(t, 0, hits)
}
