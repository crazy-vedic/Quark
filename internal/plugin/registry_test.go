package plugin_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/plugin"
)

// --- Inline test fakes ---

type fakePrePlugin struct {
	name string
	fn   func(ctx context.Context, req *domain.Request) (*domain.Request, error)
}

func (f *fakePrePlugin) Name() string { return f.name }

func (f *fakePrePlugin) BeforeRequest(
	ctx context.Context,
	req *domain.Request,
) (*domain.Request, error) {
	return f.fn(ctx, req)
}

type fakePostPlugin struct {
	name string
	fn   func(ctx context.Context, req *domain.Request, res *exec.ExecuteResult) error
}

func (f *fakePostPlugin) Name() string { return f.name }

func (f *fakePostPlugin) AfterResponse(
	ctx context.Context,
	req *domain.Request,
	res *exec.ExecuteResult,
) error {
	return f.fn(ctx, req, res)
}

// A plugin implementing both hooks.
type fakeBothPlugin struct {
	name string
}

func (f *fakeBothPlugin) Name() string { return f.name }

func (f *fakeBothPlugin) BeforeRequest(
	ctx context.Context,
	req *domain.Request,
) (*domain.Request, error) {
	return req, nil
}

func (f *fakeBothPlugin) AfterResponse(
	ctx context.Context,
	req *domain.Request,
	res *exec.ExecuteResult,
) error {
	return nil
}

// A plugin implementing neither hook.
type fakeNoHookPlugin struct{ name string }

func (f *fakeNoHookPlugin) Name() string { return f.name }

// --- Tests ---

func TestRegistry_RegisterPreRequestHook(t *testing.T) {
	r := plugin.NewRegistry()
	p := &fakePrePlugin{
		name: "auth",
		fn: func(_ context.Context, req *domain.Request) (*domain.Request, error) {
			return req, nil
		},
	}
	r.Register(p)

	hooks := r.PreRequestHooks()
	require.Len(t, hooks, 1, "must have 1 pre-request hook")
	assert.Empty(t, r.PostResponseHooks())
}

func TestRegistry_RegisterPostResponseHook(t *testing.T) {
	r := plugin.NewRegistry()
	p := &fakePostPlugin{
		name: "logger",
		fn: func(_ context.Context, _ *domain.Request, _ *exec.ExecuteResult) error {
			return nil
		},
	}
	r.Register(p)

	hooks := r.PostResponseHooks()
	require.Len(t, hooks, 1, "must have 1 post-response hook")
	assert.Empty(t, r.PreRequestHooks())
}

func TestRegistry_HookOrderPreserved(t *testing.T) {
	r := plugin.NewRegistry()
	order := []string{}

	for _, name := range []string{"first", "second", "third"} {

		r.Register(&fakePrePlugin{
			name: name,
			fn: func(_ context.Context, req *domain.Request) (*domain.Request, error) {
				order = append(order, name)
				return req, nil
			},
		})
	}

	ctx := context.Background()
	req := &domain.Request{ID: "r1"}
	var err error
	for _, h := range r.PreRequestHooks() {
		req, err = h.BeforeRequest(ctx, req)
		require.NoError(t, err)
	}

	assert.Equal(t, []string{"first", "second", "third"}, order)
}

func TestRegistry_PluginImplementingNeitherHook_NoOp(t *testing.T) {
	r := plugin.NewRegistry()
	// Registering a plugin with neither hook must not panic and must not add any hook.
	r.Register(&fakeNoHookPlugin{name: "empty"})

	assert.Empty(t, r.PreRequestHooks(), "must not add a pre-request hook")
	assert.Empty(t, r.PostResponseHooks(), "must not add a post-response hook")
}

func TestRegistry_PreRequestHookError_Propagates(t *testing.T) {
	r := plugin.NewRegistry()
	sentinel := errors.New("auth failed")
	r.Register(&fakePrePlugin{
		name: "failing-auth",
		fn: func(_ context.Context, req *domain.Request) (*domain.Request, error) {
			return nil, sentinel
		},
	})

	ctx := context.Background()
	req := &domain.Request{ID: "r1"}
	var chainErr error
	for _, h := range r.PreRequestHooks() {
		req, chainErr = h.BeforeRequest(ctx, req)
		if chainErr != nil {
			break
		}
	}

	require.Error(t, chainErr)
	assert.ErrorIs(t, chainErr, sentinel)
}

func TestRegistry_BothHookPlugin_AppearsInBothLists(t *testing.T) {
	r := plugin.NewRegistry()
	r.Register(&fakeBothPlugin{name: "full"})

	assert.Len(t, r.PreRequestHooks(), 1)
	assert.Len(t, r.PostResponseHooks(), 1)
}
