package search

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

// Searcher implements fast substring search over request name, method, and URL fields.
// Fuzzy scoring uses substring containment
// with prefix weighting. This runs in O(n) and easily handles 1000 requests <50ms.
type Searcher struct {
	reader store.RequestReader
}

// New constructs a Searcher. Zero options are valid.
func New(reader store.RequestReader, opts ...Option) *Searcher {
	o := options{}
	for _, opt := range opts {
		opt.apply(&o)
	}
	_ = o
	return &Searcher{reader: reader}
}

// Search performs substring matching over all requests in the given collection.
// Results are ranked by score descending; ties broken by Request.ID ascending.
// Returns all requests (score=1.0) when query is empty.
func (s *Searcher) Search(ctx context.Context, collectionID, query string) (*SearchResult, error) {
	start := time.Now()

	reqs, err := s.reader.ListRequests(ctx, collectionID)
	if err != nil {
		return nil, fmt.Errorf("search: list requests: %w", err)
	}

	var hits []*SearchHit
	q := strings.ToLower(strings.TrimSpace(query))

	if q == "" {
		// Empty query: return all requests with equal score.
		for _, r := range reqs {
			hits = append(hits, &SearchHit{Request: r, Score: 1.0})
		}
	} else {
		for _, r := range reqs {
			if score := scoreRequest(r, q); score > 0 {
				hits = append(hits, &SearchHit{Request: r, Score: score})
			}
		}
	}

	// Stable sort: score DESC, ID ASC as deterministic tiebreaker.
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].Request.ID < hits[j].Request.ID
	})

	return &SearchResult{
		Hits:     hits,
		Duration: time.Since(start),
	}, nil
}

// SearchAll searches across multiple collections. Each collection is searched
// independently; results are merged and re-ranked.
func (s *Searcher) SearchAll(
	ctx context.Context,
	query string,
	collectionIDs []string,
) (*SearchResult, error) {
	start := time.Now()
	var allHits []*SearchHit

	for _, colID := range collectionIDs {
		result, err := s.Search(ctx, colID, query)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
				ctx.Err() != nil {
				return &SearchResult{
					Hits:     allHits,
					Duration: time.Since(start),
				}, nil
			}
			// Log and continue — SearchAll returns partial results on per-collection failures.
			slog.Warn(
				"search: skipping collection due to error",
				"collection_id",
				colID,
				"err",
				err,
			)
			continue
		}
		allHits = append(allHits, result.Hits...)
	}

	sort.SliceStable(allHits, func(i, j int) bool {
		if allHits[i].Score != allHits[j].Score {
			return allHits[i].Score > allHits[j].Score
		}
		return allHits[i].Request.ID < allHits[j].Request.ID
	})

	return &SearchResult{
		Hits:     allHits,
		Duration: time.Since(start),
	}, nil
}

// scoreRequest computes a score in (0, 1] for a request against a lowercase query.
// Returns 0 if no match. It favors exact/prefix matches, then token and fuzzy
// subsequence matches so short power-user queries like "lp" can find
// "List Payments" without hiding stronger literal matches.
func scoreRequest(r *domain.Request, query string) float64 {
	name := strings.ToLower(r.Name)
	method := strings.ToLower(r.Method)
	url := strings.ToLower(r.URL)
	haystack := strings.TrimSpace(name + " " + method + " " + url)

	if strings.Contains(name, query) {
		return 1.0
	}
	if strings.Contains(method, query) {
		return 0.9
	}
	if strings.Contains(url, query) {
		return 0.8
	}
	if score := tokenScore(query, name, 0.78); score > 0 {
		return score
	}
	if score := tokenScore(query, url, 0.68); score > 0 {
		return score
	}
	if initialsMatch(query, name) {
		return 0.66
	}
	if fuzzySubsequence(query, name) {
		return 0.58
	}
	if fuzzySubsequence(query, url) {
		return 0.48
	}
	if fuzzySubsequence(query, haystack) {
		return 0.40
	}
	return 0
}

func tokenScore(query, value string, base float64) float64 {
	terms := strings.Fields(query)
	if len(terms) == 0 {
		return 0
	}
	tokens := splitSearchTokens(value)
	if len(tokens) == 0 {
		return 0
	}

	matched := 0
	for _, term := range terms {
		for _, token := range tokens {
			if strings.HasPrefix(token, term) || fuzzySubsequence(term, token) {
				matched++
				break
			}
		}
	}
	if matched != len(terms) {
		return 0
	}
	return base + minFloat(0.1, float64(matched)*0.02)
}

func splitSearchTokens(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

func initialsMatch(query, value string) bool {
	tokens := splitSearchTokens(value)
	queryRunes := []rune(query)
	if len(tokens) == 0 || len(queryRunes) > len(tokens) {
		return false
	}
	var initials strings.Builder
	for _, token := range tokens {
		if token == "" {
			continue
		}
		initial := []rune(token)[0]
		initials.WriteRune(initial)
	}
	return strings.HasPrefix(initials.String(), string(queryRunes))
}

func fuzzySubsequence(needle, haystack string) bool {
	if needle == "" {
		return true
	}
	if haystack == "" {
		return false
	}
	needleRunes := []rune(needle)
	idx := 0
	for _, r := range haystack {
		if needleRunes[idx] != r {
			continue
		}
		idx++
		if idx == len(needleRunes) {
			return true
		}
	}
	return false
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
