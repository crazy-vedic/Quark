package plugin

import (
	"log/slog"
)

// Registry holds registered plugins and their hook slices.
// Hook order is preserved — hooks fire in registration order.
type Registry struct {
	preHooks  []PreRequestHook
	postHooks []PostResponseHook
}

// NewRegistry constructs an empty Registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a plugin to the registry.
// If the plugin implements neither PreRequestHook nor PostResponseHook,
// it is a no-op and a warning is logged.
func (r *Registry) Register(p Plugin) {
	pre, isPre := p.(PreRequestHook)
	post, isPost := p.(PostResponseHook)

	if !isPre && !isPost {
		slog.Warn("plugin implements neither PreRequestHook nor PostResponseHook; skipping",
			"plugin", p.Name())
		return
	}

	if isPre {
		r.preHooks = append(r.preHooks, pre)
	}
	if isPost {
		r.postHooks = append(r.postHooks, post)
	}
}

// PreRequestHooks returns the ordered slice of pre-request hooks.
func (r *Registry) PreRequestHooks() []PreRequestHook {
	return r.preHooks
}

// PostResponseHooks returns the ordered slice of post-response hooks.
func (r *Registry) PostResponseHooks() []PostResponseHook {
	return r.postHooks
}
