package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

func TestNestedCollections_SiblingNamesAndPaths(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(filepath.Join(t.TempDir(), "nested.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	left := &domain.Collection{ID: "left", Name: "left"}
	right := &domain.Collection{ID: "right", Name: "right"}
	require.NoError(t, s.SaveCollection(ctx, left))
	require.NoError(t, s.SaveCollection(ctx, right))
	child := &domain.Collection{ID: "child", Name: "leaf", ParentID: left.ID}
	require.NoError(t, s.SaveCollection(ctx, child))
	assert.Equal(t, "left/leaf", mustCollectionPath(t, s, child.ID))

	req := &domain.Request{
		ID:           "req",
		CollectionID: child.ID,
		Name:         "get",
		Method:       "GET",
		URL:          "https://example.test",
	}
	require.NoError(t, s.SaveRequest(ctx, req))
	_, err = s.ResolveRequestPath(ctx, "leaf/get")
	require.NoError(t, err)

	_, err = s.ResolveRequestPath(ctx, "get")
	require.NoError(t, err)
}

func TestNestedCollections_RepairsSlashCollision(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(filepath.Join(t.TempDir(), "names.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, s.SaveCollection(ctx, &domain.Collection{ID: "existing", Name: "api-v1"}))
	repaired := &domain.Collection{ID: "repaired", Name: "api/v1"}
	require.NoError(t, s.SaveCollection(ctx, repaired))
	assert.Equal(t, "api-v1-2", repaired.Name)
}

func TestNestedCollections_MoveAndCountErrors(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(filepath.Join(t.TempDir(), "errors.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	err = s.MoveCollection(ctx, "missing", "")
	assert.ErrorIs(t, err, store.ErrNotFound)
	_, _, err = s.CountDescendants(ctx, "missing")
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func mustCollectionPath(t *testing.T, s *store.Store, id string) string {
	t.Helper()
	path, err := s.CollectionPath(context.Background(), id)
	require.NoError(t, err)
	return path
}

func TestNestedCollections_AmbiguousReference(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(filepath.Join(t.TempDir(), "ambiguous.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	for _, id := range []string{"a", "b"} {
		parent := &domain.Collection{ID: "parent-" + id, Name: id}
		child := &domain.Collection{ID: id, Name: "leaf", ParentID: parent.ID}
		require.NoError(t, s.SaveCollection(ctx, parent))
		require.NoError(t, s.SaveCollection(ctx, child))
		require.NoError(
			t,
			s.SaveRequest(
				ctx,
				&domain.Request{
					ID:           "req-" + id,
					CollectionID: id,
					Name:         "same",
					Method:       "GET",
					URL:          "https://example.test",
				},
			),
		)
	}
	_, err = s.ResolveRequestPath(ctx, "same")
	var ambiguous *store.AmbiguousPathError
	assert.True(t, errors.As(err, &ambiguous))
	assert.Len(t, ambiguous.Matches, 2)
}

func TestRenameCollection_ChangesOnlyTheName(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(filepath.Join(t.TempDir(), "rename.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	parent := &domain.Collection{ID: "parent", Name: "parent"}
	require.NoError(t, s.SaveCollection(ctx, parent))
	child := &domain.Collection{
		ID:          "child",
		Name:        "child",
		Description: "notes",
		ParentID:    "parent",
	}
	require.NoError(t, s.SaveCollection(ctx, child))
	sibling := &domain.Collection{ID: "sibling", Name: "sibling", ParentID: "parent"}
	require.NoError(t, s.SaveCollection(ctx, sibling))

	require.NoError(t, s.RenameCollection(ctx, "child", "renamed"))
	got, err := s.GetCollection(ctx, "child")
	require.NoError(t, err)
	assert.Equal(t, "renamed", got.Name)
	assert.Equal(t, "parent", got.ParentID)
	assert.Equal(t, "notes", got.Description)

	err = s.RenameCollection(ctx, "child", "sibling")
	require.ErrorIs(t, err, store.ErrDuplicate)
	err = s.RenameCollection(ctx, "missing", "x")
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestCollectionAuthAndHeaders_RoundTrip(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(filepath.Join(t.TempDir(), "collection-defaults.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	// A collection saved without defaults inherits, with no headers.
	plain := &domain.Collection{ID: "plain", Name: "plain"}
	require.NoError(t, s.SaveCollection(ctx, plain))
	got, err := s.GetCollection(ctx, plain.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.AuthTypeInherit, got.AuthType)
	assert.Equal(t, "{}", got.AuthConfig)
	assert.Equal(t, "{}", got.Headers)

	withDefaults := &domain.Collection{
		ID:         "defaults",
		Name:       "defaults",
		AuthType:   domain.AuthTypeBearer,
		AuthConfig: `{"token":"t"}`,
		Headers:    `{"X-Team":["core"]}`,
	}
	require.NoError(t, s.SaveCollection(ctx, withDefaults))
	child := &domain.Collection{ID: "child", Name: "child", ParentID: withDefaults.ID}
	require.NoError(t, s.SaveCollection(ctx, child))

	got, err = s.GetCollection(ctx, withDefaults.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.AuthTypeBearer, got.AuthType)
	assert.JSONEq(t, `{"token":"t"}`, got.AuthConfig)
	assert.JSONEq(t, `{"X-Team":["core"]}`, got.Headers)

	children, err := s.ListChildCollections(ctx, withDefaults.ID)
	require.NoError(t, err)
	require.Len(t, children, 1)
	assert.Equal(t, domain.AuthTypeInherit, children[0].AuthType)

	all, err := s.ListCollections(ctx)
	require.NoError(t, err)
	found := 0
	for _, c := range all {
		if c.ID == withDefaults.ID {
			found++
			assert.Equal(t, domain.AuthTypeBearer, c.AuthType)
		}
	}
	assert.Equal(t, 1, found)
}

func TestSetCollectionDefaults_ReplacesOnlyDefaults(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(filepath.Join(t.TempDir(), "set-defaults.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	col := &domain.Collection{ID: "col", Name: "keep-name", Description: "keep"}
	require.NoError(t, s.SaveCollection(ctx, col))

	err = s.SetCollectionDefaults(ctx, col.ID, &domain.Collection{
		AuthType:   domain.AuthTypeBearer,
		AuthConfig: `{"token":"t"}`,
		Headers:    `{"X-Team":"core"}`,
	})
	require.NoError(t, err)

	got, err := s.GetCollection(ctx, col.ID)
	require.NoError(t, err)
	assert.Equal(t, "keep-name", got.Name)
	assert.Equal(t, "keep", got.Description)
	assert.Equal(t, domain.AuthTypeBearer, got.AuthType)
	assert.JSONEq(t, `{"token":"t"}`, got.AuthConfig)
	assert.JSONEq(t, `{"X-Team":"core"}`, got.Headers)

	require.NoError(t, s.SetCollectionDefaults(ctx, col.ID, &domain.Collection{}))
	got, err = s.GetCollection(ctx, col.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.AuthTypeInherit, got.AuthType, "an empty type inherits")
	assert.Equal(t, "{}", got.Headers)

	err = s.SetCollectionDefaults(ctx, "missing", &domain.Collection{})
	require.ErrorIs(t, err, store.ErrNotFound)
}
