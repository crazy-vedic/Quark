package store

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/crazy-vedic/quark/internal/domain"
)

// AmbiguousPathError reports every full path matching a shorthand reference.
type AmbiguousPathError struct {
	Reference string
	Matches   []string
}

func (e *AmbiguousPathError) Error() string {
	return fmt.Sprintf(
		"store: ambiguous reference %q (matches: %s)",
		e.Reference,
		strings.Join(e.Matches, ", "),
	)
}

// ResolveRequestPath resolves a full path or the shortest unique suffix.
func (s *Store) ResolveRequestPath(ctx context.Context, reference string) (*domain.Request, error) {
	pathCache := make(map[string]string)
	collections, err := s.ListCollections(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: resolve request collections: %w", err)
	}
	for _, c := range collections {
		path, err := s.CollectionPath(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		pathCache[c.ID] = path
	}
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT `+requestColumns+` FROM requests`,
	)
	if err != nil {
		return nil, fmt.Errorf("store: resolve request: %w", err)
	}
	defer rows.Close()
	var matches []*domain.Request
	var paths []string
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		p, ok := pathCache[r.CollectionID]
		if !ok {
			return nil, fmt.Errorf("store: request collection %q: %w", r.CollectionID, ErrNotFound)
		}
		full := p + "/" + r.Name
		if reference == full || strings.HasSuffix(full, "/"+reference) ||
			(reference == r.Name && strings.Count(full, "/") == 1) {
			matches = append(matches, r)
			paths = append(paths, full)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("store: request %q: %w", reference, ErrNotFound)
	}
	if len(matches) > 1 {
		sort.Strings(paths)
		return nil, &AmbiguousPathError{Reference: reference, Matches: paths}
	}
	return matches[0], nil
}
