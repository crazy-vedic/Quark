// Package plugin defines the hook interfaces for Quark's plugin system.
// These interfaces are frozen at v1.0 — adding a method is a breaking change.
// The Lua plugin runtime (V2) satisfies these same interfaces.
package plugin

import (
	"context"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
)

// PreRequestHook is called before an HTTP request is dispatched.
// Returning a modified *domain.Request replaces the original.
// Returning an error aborts the request and surfaces it in the TUI status bar.
type PreRequestHook interface {
	BeforeRequest(ctx context.Context, req *domain.Request) (*domain.Request, error)
}

// PostResponseHook is called after a response is received.
// It cannot modify the response — it is for side effects (logging, notifications).
// Errors are logged via slog.Warn; the next hook still fires.
type PostResponseHook interface {
	AfterResponse(ctx context.Context, req *domain.Request, res *exec.ExecuteResult) error
}

// Plugin is the composition interface for anything that implements one or both hooks.
// Registering a Plugin that implements neither hook is a no-op enforced at registration time.
type Plugin interface {
	Name() string
}
