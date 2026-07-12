package exec

import (
	"context"

	"github.com/crazy-vedic/quark/internal/domain"
)

// PreRequestHook runs before an HTTP request is dispatched.
// Hooks can mutate or replace the request; returning an error aborts Execute.
type PreRequestHook interface {
	BeforeRequest(ctx context.Context, req *domain.Request) (*domain.Request, error)
}

// PostResponseHook runs after a response is received (and after body read).
// Errors are logged and do not fail Execute.
type PostResponseHook interface {
	AfterResponse(ctx context.Context, req *domain.Request, res *ExecuteResult) error
}
