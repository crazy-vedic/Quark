package cli

import (
	"bytes"
	"context"
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
	root.SetArgs([]string{"delete", "collection-1"})
	require.NoError(t, root.Execute())
	require.Empty(t, st.collections)
	require.Contains(t, out.String(), `Deleted collection "After"`)
}

func TestCollectionRenameAndDeleteRejectMissingCollection(t *testing.T) {
	for _, args := range [][]string{{"rename", "missing", "Name"}, {"delete", "missing"}} {
		root := NewCollectionCmd(&collectionCommandStore{})
		root.SetArgs(args)
		require.Error(t, root.Execute())
	}
}
