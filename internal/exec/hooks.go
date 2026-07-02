package exec

import (
	"context"

	"github.com/crazy-vedic/quark/internal/domain"
)

// PreRequestHook is the exec-local mirror of plugin.PreRequestHook.
// Any type that implements plugin.PreRequestHook automatically satisfies
// this interface (Go structural typing — identical method signature).
// Defined here to avoid an exec→plugin import cycle.
type PreRequestHook interface {
	BeforeRequest(ctx context.Context, req *domain.Request) (*domain.Request, error)
}

// PostResponseHook is the exec-local mirror of plugin.PostResponseHook.
// AfterResponse takes *ExecuteResult (defined in this package), so the
// signatures are identical to plugin.PostResponseHook which uses *exec.ExecuteResult.
type PostResponseHook interface {
	AfterResponse(ctx context.Context, req *domain.Request, res *ExecuteResult) error
}
