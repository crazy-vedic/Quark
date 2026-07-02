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
}

// SearchHit is a single ranked match.
type SearchHit struct {
	Request    *domain.Request
	Collection *domain.Collection // nil for MVP (name+URL search only)
	Score      float64
}
