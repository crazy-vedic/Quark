// Package runner executes every request of a collection in order, applying
// each request's extraction rules and assertions, and reports the outcome of
// each request. It backs 'quark test'.
package runner

import (
	"context"
	"fmt"
	"time"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
)

// Status is the outcome of one request.
type Status string

// Request outcomes.
const (
	Pass Status = "pass"
	Fail Status = "fail"
)

// Store is the data access the runner needs.
type Store interface {
	ResolveCollectionPath(ctx context.Context, reference string) (*domain.Collection, error)
	CollectionPath(ctx context.Context, id string) (string, error)
	ListRequests(ctx context.Context, collectionID string) ([]*domain.Request, error)
	ListChildCollections(ctx context.Context, parentID string) ([]*domain.Collection, error)
	exec.EnvResolver
}

// Executor sends one request. *exec.Executor satisfies it; its hooks apply
// extraction and assertions.
type Executor interface {
	Execute(ctx context.Context, req *domain.Request) (*exec.ExecuteResult, error)
}

// Result reports one request of the run.
type Result struct {
	Name       string // collection path and request name, e.g. "API/Users/get user"
	Collection string // collection path the request belongs to
	Status     Status
	// Messages explains a failure: one line per failed assertion, or the
	// transport or interpolation error. Empty when the request passed.
	Messages   []string
	StatusCode int
	Duration   time.Duration
}

// Options controls a run.
type Options struct {
	// Bail stops the run after the first failing request.
	Bail bool
}

// Run executes the requests of the collection at reference, in order: the
// collection's own requests by sort order, then each child collection in
// turn, depth first. The request's Enabled flag is not consulted: no code
// path sets it yet, so every stored request reads as disabled. Results are returned in execution order; a failed run returns the results
// so far, and the caller decides the exit status from them.
func Run(
	ctx context.Context,
	st Store,
	ex Executor,
	reference string,
	opts Options,
) ([]Result, error) {
	root, err := st.ResolveCollectionPath(ctx, reference)
	if err != nil {
		return nil, fmt.Errorf("test: collection %q: %w", reference, err)
	}
	rootPath, err := st.CollectionPath(ctx, root.ID)
	if err != nil {
		return nil, fmt.Errorf("test: collection path: %w", err)
	}
	r := &run{st: st, ex: ex, opts: opts}
	if err := r.collection(ctx, root.ID, rootPath); err != nil {
		return r.results, err
	}
	return r.results, nil
}

// Failed reports how many results did not pass.
func Failed(results []Result) int {
	n := 0
	for _, r := range results {
		if r.Status == Fail {
			n++
		}
	}
	return n
}

type run struct {
	st      Store
	ex      Executor
	opts    Options
	results []Result
	stop    bool
}

// collection runs the requests of one collection, then its children.
func (r *run) collection(ctx context.Context, id, path string) error {
	reqs, err := r.st.ListRequests(ctx, id)
	if err != nil {
		return fmt.Errorf("test: list requests in %q: %w", path, err)
	}
	for _, req := range reqs {
		if r.stop {
			return nil
		}
		res := r.request(ctx, req, path)
		r.results = append(r.results, res)
		if res.Status == Fail && r.opts.Bail {
			r.stop = true
			return nil
		}
	}
	children, err := r.st.ListChildCollections(ctx, id)
	if err != nil {
		return fmt.Errorf("test: list child collections of %q: %w", path, err)
	}
	for _, child := range children {
		if r.stop {
			return nil
		}
		if err := r.collection(ctx, child.ID, path+"/"+child.Name); err != nil {
			return err
		}
	}
	return nil
}

// request sends one request and judges the response.
func (r *run) request(ctx context.Context, req *domain.Request, collection string) Result {
	res := Result{
		Name:       collection + "/" + req.Name,
		Collection: collection,
	}
	// Re-read the environments for every request: an earlier request may
	// have extracted values into them.
	colEnv, globalEnv, err := exec.ResolveEnvVars(ctx, r.st, req.CollectionID)
	if err != nil {
		return fail(res, fmt.Sprintf("resolve environments: %v", err))
	}
	prepared, err := exec.InterpolateRequestWithOverrides(req, nil, nil, colEnv, globalEnv)
	if err != nil {
		return fail(res, fmt.Sprintf("interpolate: %v", err))
	}
	out, err := r.ex.Execute(ctx, prepared)
	if err != nil {
		return fail(res, fmt.Sprintf("request failed: %v", err))
	}
	defer func() { _ = out.Cleanup() }()

	res.StatusCode = out.StatusCode
	res.Duration = out.Duration
	var messages []string
	for _, a := range out.Assertions {
		if !a.Passed {
			messages = append(messages, a.Rule+": "+a.Reason)
		}
	}
	if len(messages) > 0 {
		res.Status = Fail
		res.Messages = messages
		return res
	}
	res.Status = Pass
	return res
}

func fail(res Result, msg string) Result {
	res.Status = Fail
	res.Messages = []string{msg}
	return res
}

// Summary is a one-line count of the results, for text output.
func Summary(results []Result) string {
	passed := 0
	for _, r := range results {
		if r.Status == Pass {
			passed++
		}
	}
	return fmt.Sprintf("%d passed, %d failed", passed, len(results)-passed)
}
