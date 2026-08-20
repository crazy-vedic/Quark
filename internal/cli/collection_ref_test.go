package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

type collectionReferenceTestStore struct {
	collections []*domain.Collection
}

func (s *collectionReferenceTestStore) ListCollections(context.Context) ([]*domain.Collection, error) {
	return s.collections, nil
}

func TestResolveCollectionReferenceAcceptsIDPrefixAndName(t *testing.T) {
	st := &collectionReferenceTestStore{collections: []*domain.Collection{
		{ID: "0123456789abcdef", Name: "API"},
	}}

	byName, err := resolveCollectionReference(context.Background(), st, "API")
	require.NoError(t, err)
	require.Equal(t, "0123456789abcdef", byName.ID)

	byPrefix, err := resolveCollectionReference(context.Background(), st, "01234567")
	require.NoError(t, err)
	require.Equal(t, "API", byPrefix.Name)
}

func TestResolveCollectionReferenceRejectsAmbiguousName(t *testing.T) {
	st := &collectionReferenceTestStore{collections: []*domain.Collection{
		{ID: "one", Name: "API"},
		{ID: "two", Name: "API"},
	}}
	_, err := resolveCollectionReference(context.Background(), st, "API")
	require.Error(t, err)
	require.Contains(t, err.Error(), "ambiguous")
}
