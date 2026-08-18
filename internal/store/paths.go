package store

import (
	"context"
	"database/sql"
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
	return fmt.Sprintf("store: ambiguous reference %q (matches: %s)", e.Reference, strings.Join(e.Matches, ", "))
}

// ResolveRequestPath resolves a full path or the shortest unique suffix.
func (s *Store) ResolveRequestPath(ctx context.Context, reference string) (*domain.Request, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, collection_id, name, method, url, headers, auth_type, auth_config, body, sort_order, enabled, created_at, updated_at FROM requests`)
	if err != nil {
		return nil, fmt.Errorf("store: resolve request: %w", err)
	}
	defer rows.Close()
	var matches []*domain.Request
	var paths []string
	for rows.Next() {
		r := &domain.Request{}
		var body sql.NullString
		if err := rows.Scan(&r.ID, &r.CollectionID, &r.Name, &r.Method, &r.URL, &r.Headers, &r.AuthType, &r.AuthConfig, &body, &r.SortOrder, &r.Enabled, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Body = body.String
		p, err := s.CollectionPath(ctx, r.CollectionID)
		if err != nil {
			return nil, err
		}
		full := p + "/" + r.Name
		if reference == full || strings.HasSuffix(full, "/"+reference) || (reference == r.Name && strings.Count(full, "/") == 1) {
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
