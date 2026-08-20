package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

type collectionPathResolver interface {
	ResolveCollectionPath(context.Context, string) (*domain.Collection, error)
}

type collectionFullPathResolver interface {
	CollectionPath(context.Context, string) (string, error)
}

// resolveCollectionReference accepts a full ID, a unique ID prefix, a name,
// or (when supported by the store) a nested path such as API/Users.
func resolveCollectionReference(ctx context.Context, lister store.CollectionLister, reference string) (*domain.Collection, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return nil, fmt.Errorf("collection reference is required")
	}
	if resolver, ok := lister.(collectionPathResolver); ok {
		if col, err := resolver.ResolveCollectionPath(ctx, reference); err == nil {
			return col, nil
		}
	}
	cols, err := lister.ListCollections(ctx)
	if err != nil {
		return nil, err
	}
	var matches []*domain.Collection
	for _, col := range cols {
		if col.ID == reference || col.Name == reference || strings.HasPrefix(col.ID, reference) {
			matches = append(matches, col)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("collection %q is ambiguous; use its full ID or path", reference)
	}
	return nil, fmt.Errorf("collection %q not found", reference)
}
