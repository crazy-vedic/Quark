package cli

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

type collectionCommandStore struct {
	collections []*domain.Collection
}

func (s *collectionCommandStore) ListCollections(context.Context) ([]*domain.Collection, error) {
	return s.collections, nil
}

func (s *collectionCommandStore) SaveCollection(
	_ context.Context,
	collection *domain.Collection,
) error {
	for i, current := range s.collections {
		if current.ID == collection.ID {
			s.collections[i] = collection
			return nil
		}
	}
	s.collections = append(s.collections, collection)
	return nil
}

func (s *collectionCommandStore) RenameCollection(_ context.Context, id, name string) error {
	for _, current := range s.collections {
		if current.ID == id {
			current.Name = name
			return nil
		}
	}
	return errors.New("collection not found")
}

func (s *collectionCommandStore) DeleteCollection(_ context.Context, id string) error {
	for i, current := range s.collections {
		if current.ID == id {
			s.collections = append(s.collections[:i], s.collections[i+1:]...)
			return nil
		}
	}
	return nil
}

func TestCollectionRenameAndDeleteCommands(t *testing.T) {
	st := &collectionCommandStore{
		collections: []*domain.Collection{{ID: "collection-1", Name: "Before"}},
	}
	root := NewCollectionCmd(st)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"rename", "collection", "After"})
	require.NoError(t, root.Execute())
	require.Equal(t, "After", st.collections[0].Name)
	require.Contains(t, out.String(), `Renamed collection "collection" to "After"`)

	out.Reset()
	root = NewCollectionCmd(st)
	root.SetOut(&out)
	root.SetArgs([]string{"delete", "collection-1", "--yes"})
	require.NoError(t, root.Execute())
	require.Empty(t, st.collections)
	require.Contains(t, out.String(), `Deleted collection "After"`)
}

// Deleting removes every nested collection and request. It must not happen
// without an explicit --yes, and a short ID prefix must not pick a collection.
func TestCollectionDeleteRequiresYesAndFullPrefix(t *testing.T) {
	st := &collectionCommandStore{collections: []*domain.Collection{
		{ID: "a1b2c3d4-0000-4000-8000-000000000001", Name: "Payments"},
	}}

	root := NewCollectionCmd(st)
	root.SetArgs([]string{"delete", "Payments"})
	require.Error(t, root.Execute(), "delete without --yes must be refused")
	require.Len(t, st.collections, 1)

	root = NewCollectionCmd(st)
	root.SetArgs([]string{"delete", "a1", "--yes"})
	require.Error(t, root.Execute(), "a 2-character ID prefix must not match")
	require.Len(t, st.collections, 1)
}

func TestCollectionRenameAndDeleteRejectMissingCollection(t *testing.T) {
	for _, args := range [][]string{{"rename", "missing", "Name"}, {"delete", "missing"}} {
		root := NewCollectionCmd(&collectionCommandStore{})
		root.SetArgs(args)
		require.Error(t, root.Execute())
	}
}

// newCollectionMoveStore returns a real store with API/Users, a root Billing
// collection that has its own Users child (so moves can clash), and an empty
// root Archive.
func newCollectionMoveStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	st, err := store.New(filepath.Join(t.TempDir(), "quark.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	require.NoError(t, st.SaveCollection(ctx, &domain.Collection{ID: "col-api", Name: "API"}))
	require.NoError(t, st.SaveCollection(ctx, &domain.Collection{
		ID: "col-users", ParentID: "col-api", Name: "Users",
	}))
	require.NoError(t, st.SaveCollection(ctx, &domain.Collection{
		ID: "col-billing", Name: "Billing",
	}))
	require.NoError(t, st.SaveCollection(ctx, &domain.Collection{
		ID: "col-billing-users", ParentID: "col-billing", Name: "Users",
	}))
	require.NoError(t, st.SaveCollection(ctx, &domain.Collection{
		ID: "col-archive", Name: "Archive",
	}))
	return st
}

func runCollectionCmd(t *testing.T, st CollectionStore, args ...string) (string, error) {
	t.Helper()
	cmd := NewCollectionCmd(st)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	cmd.SetContext(context.Background())
	err := cmd.Execute()
	return out.String(), err
}

func TestCollectionMoveUnderAnotherCollection(t *testing.T) {
	st := newCollectionMoveStore(t)

	out, err := runCollectionCmd(t, st, "move", "API/Users", "Archive")
	require.NoError(t, err)
	assert.Contains(t, out, `Moved collection "API/Users" under "Archive"`)

	path, err := st.CollectionPath(context.Background(), "col-users")
	require.NoError(t, err)
	assert.Equal(t, "Archive/Users", path, "the collection is now a child of Archive")
}

func TestCollectionMoveToRoot(t *testing.T) {
	st := newCollectionMoveStore(t)

	out, err := runCollectionCmd(t, st, "move", "Billing/Users", "/")
	require.NoError(t, err)
	assert.Contains(t, out, `Moved collection "Billing/Users" to the root`)

	path, err := st.CollectionPath(context.Background(), "col-billing-users")
	require.NoError(t, err)
	assert.Equal(t, "Users", path)
}

func TestCollectionMoveRejectsBadTargets(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "into itself",
			args:    []string{"move", "API", "API"},
			wantErr: "into itself or one of its descendants",
		},
		{
			name:    "into a descendant",
			args:    []string{"move", "API", "API/Users"},
			wantErr: "into itself or one of its descendants",
		},
		{
			name:    "name clash with a sibling",
			args:    []string{"move", "Billing/Users", "API"},
			wantErr: `a collection named "Users" already exists in "API"`,
		},
		{
			name:    "unknown collection to move",
			args:    []string{"move", "Missing", "API"},
			wantErr: "collection move",
		},
		{
			name:    "unknown new parent",
			args:    []string{"move", "API/Users", "Missing"},
			wantErr: "new parent",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newCollectionMoveStore(t)
			_, err := runCollectionCmd(t, st, tt.args...)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestCollectionMoveNeedsAMovableStore(t *testing.T) {
	st := &collectionCommandStore{}
	_, err := runCollectionCmd(t, st, "move", "A", "/")
	require.ErrorContains(t, err, "store cannot move collections")
}
