package cli

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
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
