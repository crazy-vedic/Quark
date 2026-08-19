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

	req := &domain.Request{ID: "req", CollectionID: child.ID, Name: "get", Method: "GET", URL: "https://example.test"}
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
		require.NoError(t, s.SaveRequest(ctx, &domain.Request{ID: "req-" + id, CollectionID: id, Name: "same", Method: "GET", URL: "https://example.test"}))
	}
	_, err = s.ResolveRequestPath(ctx, "same")
	var ambiguous *store.AmbiguousPathError
	assert.True(t, errors.As(err, &ambiguous))
	assert.Len(t, ambiguous.Matches, 2)
}
