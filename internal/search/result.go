// Package search implements fuzzy search over Quark's request collection.
package search

import (
	"time"

	"github.com/crazy-vedic/quark/internal/domain"
)

// SearchResult holds the ranked output of a search query.
type SearchResult struct {
	Hits     []*SearchHit
	Duration time.Duration
	// Failures lists collections that could not be searched. Their hits are
	// missing from Hits, so the result is partial when this is non-empty.
	Failures []CollectionFailure
}

// CollectionFailure records why one collection was skipped during SearchAll.
type CollectionFailure struct {
	CollectionID string
	Err          error
}

// SearchHit is a single ranked match.
type SearchHit struct {
	Request    *domain.Request
	Collection *domain.Collection // nil for MVP (name+URL search only)
	Score      float64
}
