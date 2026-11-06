//go:build e2e

// Package tui_test provides end-to-end tests for collection management
// (add, rename, delete) in the TUI sidebar.
// Run with: go test -tags e2e ./tests/e2e/tui/...
package tui_test

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/tui"
)

// --- E2E: Add collection ---

func TestE2E_CollectionMgmt_AddPrompt(t *testing.T) {
	st := setupStore(t)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg(nil))

	// Press 'A' in sidebar to add a collection
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	assert.Equal(t, tui.CollectionPromptMode, m.Mode(), "must enter collection prompt mode")
	assert.Equal(t, tui.PromptAdd, m.PromptMode(), "must be promptAdd")
	assertViewContains(t, m, "New Collection")
}

func TestE2E_CollectionMgmt_AddPromptCancel(t *testing.T) {
	st := setupStore(t)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg(nil))

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	require.Equal(t, tui.CollectionPromptMode, m.Mode())

	// Esc cancels the prompt
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.NormalMode, m.Mode(), "must return to normal mode")
	assert.Equal(t, tui.PromptNone, m.PromptMode(), "prompt mode must be reset")
}

func TestE2E_CollectionMgmt_AddAndSave(t *testing.T) {
	st := setupStore(t)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg(nil))

	// Open add prompt
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	require.Equal(t, tui.CollectionPromptMode, m.Mode())

	// Type collection name
	for _, r := range "NewAPI" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	// Confirm with Enter — this dispatches the async save command.
	var cmd tea.Cmd
	m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	// The async command returns a collectionSavedMsg. Run it and feed back.
	msg := runCmd(t, cmd)
	require.NotNil(t, msg, "Enter must dispatch a command")
	m = callUpdate(t, m, msg)

	assert.Equal(t, tui.NormalMode, m.Mode(), "must return to normal mode after save")

	// Verify the collection was saved to the real store.
	cols, err := st.ListCollections(context.Background())
	require.NoError(t, err)
	require.Len(t, cols, 1, "must have one collection")
	assert.Equal(t, "NewAPI", cols[0].Name, "collection name must match")
}

func TestE2E_CollectionMgmt_AddRequestPrompt_UsesLowercaseA(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	assert.Equal(t, tui.CollectionPromptMode, m.Mode(), "must enter request prompt mode")
	assert.Equal(t, tui.PromptAddRequest, m.PromptMode(), "must be promptAddRequest")
	assertViewContains(t, m, "New Request")
}

func TestE2E_CollectionMgmt_AddRequest_EnterClosesPromptAndPersists(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	require.Equal(t, tui.CollectionPromptMode, m.Mode())
	require.Equal(t, tui.PromptAddRequest, m.PromptMode())

	for _, r := range "Fresh Request" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	var cmd tea.Cmd
	m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	msg := runCmd(t, cmd)
	require.NotNil(t, msg, "Enter must dispatch a save command")
	m = callUpdate(t, m, msg)

	assert.Equal(t, tui.NormalMode, m.Mode(), "successful request creation must close the prompt")
	assert.Equal(
		t,
		tui.PromptNone,
		m.PromptMode(),
		"prompt mode must be reset after request creation",
	)
	assert.NotContains(t, m.View(), "New Request", "request modal must disappear after confirm")

	reqs, err := st.ListRequests(context.Background(), col.ID)
	require.NoError(t, err)
	require.Len(t, reqs, 1, "request must be saved to the store")
	assert.Equal(t, "Fresh Request", reqs[0].Name)
	assert.Equal(t, col.ID, reqs[0].CollectionID)
}

// --- E2E: Rename collection ---

func TestE2E_CollectionMgmt_RenamePrompt(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))

	// Press 'r' to rename the selected collection
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	assert.Equal(t, tui.CollectionPromptMode, m.Mode(), "must enter collection prompt mode")
	assert.Equal(t, tui.PromptRename, m.PromptMode(), "must be promptRename")
	assertViewContains(t, m, "Rename Collection")
}

func TestE2E_CollectionMgmt_RenameAndSave(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))

	// Open rename prompt
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	require.Equal(t, tui.CollectionPromptMode, m.Mode())

	// Type new name
	for _, r := range "RenamedAPI" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	// Confirm with Enter — dispatches async save command.
	var cmd tea.Cmd
	m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	msg := runCmd(t, cmd)
	require.NotNil(t, msg)
	m = callUpdate(t, m, msg)

	assert.Equal(t, tui.NormalMode, m.Mode(), "must return to normal mode after save")

	// Verify the collection was renamed in the real store.
	cols, err := st.ListCollections(context.Background())
	require.NoError(t, err)
	require.Len(t, cols, 1, "must have one collection")
	assert.Equal(t, "RenamedAPI", cols[0].Name, "collection name must be updated")
}

// --- E2E: Delete collection ---

func TestE2E_CollectionMgmt_DeletePrompt(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))

	// Press 'D' (Shift+D) to delete the selected collection.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'D'}})
	assert.Equal(t, tui.CollectionPromptMode, m.Mode(), "must enter collection prompt mode")
	assert.Equal(t, tui.PromptDeleteConfirm, m.PromptMode(), "must be promptDeleteConfirm")
	assertViewContains(t, m, "Delete Collection")
	assertViewContains(t, m, "Type 'delete' to confirm")
	assertViewContains(t, m, "permanent and irreversible")
	assertViewContains(t, m, "[Esc] cancel")
}

func TestE2E_CollectionMgmt_DeleteCancel(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'D'}})
	require.Equal(t, tui.CollectionPromptMode, m.Mode())

	// Esc cancels the delete prompt
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.NormalMode, m.Mode(), "must return to normal mode")
	assert.Equal(t, tui.PromptNone, m.PromptMode(), "prompt mode must be reset")

	// Collection still exists in store.
	cols, err := st.ListCollections(context.Background())
	require.NoError(t, err)
	assert.Len(t, cols, 1, "collection must still exist")
}

func TestE2E_CollectionMgmt_DeleteConfirm(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))

	// Open delete prompt
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'D'}})
	require.Equal(t, tui.CollectionPromptMode, m.Mode())

	// Type 'delete' and submit — dispatches async delete command.
	var cmd tea.Cmd
	for _, r := range "delete" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	msg := runCmd(t, cmd)
	require.NotNil(t, msg)
	m = callUpdate(t, m, msg)

	assert.Equal(t, tui.NormalMode, m.Mode(), "must return to normal mode after delete")

	// Verify the collection was deleted from the real store.
	cols, err := st.ListCollections(context.Background())
	require.NoError(t, err)
	assert.Len(t, cols, 0, "collection must be deleted")
}

func TestE2E_CollectionMgmt_DeleteWrongConfirm(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))

	// Open delete prompt
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'D'}})
	require.Equal(t, tui.CollectionPromptMode, m.Mode())

	// Press any key other than 'D' — should leave the prompt open.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	assert.Equal(t, tui.CollectionPromptMode, m.Mode(), "must stay in prompt mode on wrong key")

	// Then cancel with Esc
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.NormalMode, m.Mode(), "must return to normal mode")

	// Collection still exists in store.
	cols, err := st.ListCollections(context.Background())
	require.NoError(t, err)
	assert.Len(t, cols, 1, "collection must still exist")
}

// --- E2E: No collection selected — prompts require selection ---

func TestE2E_CollectionMgmt_NoSelection(t *testing.T) {
	st := setupStore(t)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg(nil))

	// Rename, Delete, and Add-request with no collections should show error in status.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	assert.Equal(t, tui.NormalMode, m.Mode(), "must stay in normal mode")
	assert.Equal(t, "Select a collection first", m.StatusErr())
	assertViewContains(t, m, "Select a collection first")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	assert.Equal(t, tui.NormalMode, m.Mode(), "must stay in normal mode")
	assert.NotEmpty(t, m.StatusErr(), "must show error when no collection selected")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	assert.Equal(t, tui.NormalMode, m.Mode(), "must stay in normal mode")
	assert.NotEmpty(t, m.StatusErr(), "must show error when no collection selected")

	// 'A' (new collection) must clear the stale error from 'a'.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	assert.Equal(t, tui.CollectionPromptMode, m.Mode())
	assert.Empty(t, m.StatusErr(), "new-collection prompt must not show stale select-collection error")
	assert.NotContains(t, m.View(), "Select a collection first")
}

// --- E2E: Add empty name shows error ---

func TestE2E_CollectionMgmt_AddEmptyName(t *testing.T) {
	st := setupStore(t)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg(nil))

	// Open add prompt
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	require.Equal(t, tui.CollectionPromptMode, m.Mode())

	// Press Enter without typing anything
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, tui.CollectionPromptMode, m.Mode(), "must stay in prompt mode")
	assert.NotEmpty(t, m.StatusErr(), "must show error for empty name")
}

// --- E2E: Rename empty name shows error ---

func TestE2E_CollectionMgmt_RenameEmptyName(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))

	// Open rename prompt
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	require.Equal(t, tui.CollectionPromptMode, m.Mode())

	// Press Enter without typing anything
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, tui.CollectionPromptMode, m.Mode(), "must stay in prompt mode")
	assert.NotEmpty(t, m.StatusErr(), "must show error for empty name")
}
