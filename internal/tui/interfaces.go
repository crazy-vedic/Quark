package tui

import (
	"context"
	"io"

	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/search"
)

// RequestExecutor is a narrow interface for dispatching HTTP requests.
// *exec.Executor satisfies this interface structurally.
// Define at the consumer (tui package) per interface-segregation principle.
type RequestExecutor interface {
	Execute(ctx context.Context, req *domain.Request) (*exec.ExecuteResult, error)
}

// RequestSearcher is a narrow interface for searching requests.
// *search.Searcher satisfies this interface structurally.
type RequestSearcher interface {
	Search(ctx context.Context, collectionID, query string) (*search.SearchResult, error)
}

// CurlImporter is a narrow interface for parsing curl commands.
// *curl.Importer satisfies this interface structurally.
type CurlImporter interface {
	Parse(r io.Reader) (*curl.ImportResult, error)
}
