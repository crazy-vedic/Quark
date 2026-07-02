package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

// --- safeManifestID tests ---

func TestSafeManifestID(t *testing.T) {
	tests := []struct {
		name  string
		id    string
		valid bool
	}{
		{"empty", "", false},
		{"dot_dot", "..", false},
		{"dot_dot_slash", "../", false},
		{"leading_slash", "/collection", false},
		{"backslash", "back\\slash", false},
		{"forward_slash", "path/to/file", false},
		{"valid_id", "collection", true},
		{"valid_with_hyphen", "my-collection", true},
		{"valid_with_underscore", "my_collection", true},
		{"nested_traversal", "nested/../path", false},
		{"double_dots_in_middle", "foo/..bar", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.valid, safeManifestID(tt.id))
		})
	}
}

// --- mergeEnvironmentsIntoGlobal tests ---

type mergeTestStore struct {
	envs []*domain.Environment
}

func (s *mergeTestStore) GetEnvironment(
	ctx context.Context,
	id string,
) (*domain.Environment, error) {
	for _, e := range s.envs {
		if e.ID == id {
			return e, nil
		}
	}
	return nil, store.ErrNotFound
}

func (s *mergeTestStore) GetGlobalEnvironment(ctx context.Context) (*domain.Environment, error) {
	for _, e := range s.envs {
		if e.IsGlobal() {
			return e, nil
		}
	}
	return nil, store.ErrNotFound
}

func (s *mergeTestStore) ListEnvironments(
	ctx context.Context,
	collectionID string,
) ([]*domain.Environment, error) {
	var out []*domain.Environment
	for _, e := range s.envs {
		if collectionID == "" && e.IsGlobal() {
			out = append(out, e)
		} else if e.CollectionID == collectionID {
			out = append(out, e)
		}
	}
	return out, nil
}

func (s *mergeTestStore) ListCollectionEnvironments(
	ctx context.Context,
	collectionID string,
) ([]*domain.Environment, error) {
	return s.ListEnvironments(ctx, collectionID)
}

func (s *mergeTestStore) ListAllEnvironments(ctx context.Context) ([]*domain.Environment, error) {
	var out []*domain.Environment
	for _, e := range s.envs {
		if !e.IsGlobal() {
			out = append(out, e)
		}
	}
	return out, nil
}

func (s *mergeTestStore) SaveEnvironment(ctx context.Context, env *domain.Environment) error {
	for i, e := range s.envs {
		if e.ID == env.ID {
			s.envs[i] = env
			return nil
		}
	}
	s.envs = append(s.envs, env)
	return nil
}

func (s *mergeTestStore) DeleteEnvironment(ctx context.Context, id string) error {
	for i, e := range s.envs {
		if e.ID == id {
			s.envs = append(s.envs[:i], s.envs[i+1:]...)
			return nil
		}
	}
	return store.ErrNotFound
}

func (s *mergeTestStore) CreateDefaultEnvironment(
	ctx context.Context,
	collectionID string,
) (*domain.Environment, error) {
	return &domain.Environment{}, nil
}

func (s *mergeTestStore) ListCollections(ctx context.Context) ([]*domain.Collection, error) {
	return nil, nil
}

func (s *mergeTestStore) GetRequest(ctx context.Context, id string) (*domain.Request, error) {
	return nil, store.ErrNotFound
}

func (s *mergeTestStore) ListRequests(
	ctx context.Context,
	collectionID string,
) ([]*domain.Request, error) {
	return nil, nil
}

func (s *mergeTestStore) BeginTransaction(ctx context.Context) (store.TransactionalWriter, error) {
	return nil, nil
}

func TestMergeEnvironmentsIntoGlobal_EmptyMerge(t *testing.T) {
	global := &domain.Environment{ID: "global", Name: "global", Data: `{"existing": "value"}`}
	st := &mergeTestStore{envs: []*domain.Environment{global}}
	logger := NewDebugLogger(nil)

	err := mergeEnvironmentsIntoGlobal(context.Background(), st, nil, logger)
	require.NoError(t, err)

	got, err := st.GetGlobalEnvironment(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "value", got.Vars()["existing"])
}

func TestMergeEnvironmentsIntoGlobal_MergesVars(t *testing.T) {
	global := &domain.Environment{ID: "global", Name: "global", Data: `{"existing": "value"}`}
	env1 := &domain.Environment{
		ID:           "e1",
		CollectionID: "c1",
		Name:         "dev",
		Data:         `{"url": "http://dev.local"}`,
	}
	st := &mergeTestStore{envs: []*domain.Environment{global, env1}}
	logger := NewDebugLogger(nil)

	err := mergeEnvironmentsIntoGlobal(
		context.Background(),
		st,
		[]*domain.Environment{env1},
		logger,
	)
	require.NoError(t, err)

	got, err := st.GetGlobalEnvironment(context.Background())
	require.NoError(t, err)
	vars := got.Vars()
	assert.Equal(t, "value", vars["existing"])
	assert.Equal(t, "http://dev.local", vars["url"])
}

func TestMergeEnvironmentsIntoGlobal_MultipleEnvs(t *testing.T) {
	global := &domain.Environment{ID: "global", Name: "global", Data: `{}`}
	env1 := &domain.Environment{ID: "e1", CollectionID: "c1", Name: "dev", Data: `{"a": "1"}`}
	env2 := &domain.Environment{ID: "e2", CollectionID: "c2", Name: "prod", Data: `{"b": "2"}`}
	st := &mergeTestStore{envs: []*domain.Environment{global, env1, env2}}
	logger := NewDebugLogger(nil)

	err := mergeEnvironmentsIntoGlobal(
		context.Background(),
		st,
		[]*domain.Environment{env1, env2},
		logger,
	)
	require.NoError(t, err)

	got, err := st.GetGlobalEnvironment(context.Background())
	require.NoError(t, err)
	vars := got.Vars()
	assert.Equal(t, "1", vars["a"])
	assert.Equal(t, "2", vars["b"])
}

func TestMergeEnvironmentsIntoGlobal_DuplicateKeyResolution(t *testing.T) {
	global := &domain.Environment{ID: "global", Name: "global", Data: `{"key": "global"}`}
	env1 := &domain.Environment{ID: "e1", CollectionID: "c1", Name: "dev", Data: `{"key": "dev"}`}
	env2 := &domain.Environment{ID: "e2", CollectionID: "c2", Name: "prod", Data: `{"key": "prod"}`}
	st := &mergeTestStore{envs: []*domain.Environment{global, env1, env2}}
	logger := NewDebugLogger(nil)

	// Last env wins on duplicate key
	err := mergeEnvironmentsIntoGlobal(
		context.Background(),
		st,
		[]*domain.Environment{env1, env2},
		logger,
	)
	require.NoError(t, err)

	got, err := st.GetGlobalEnvironment(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "prod", got.Vars()["key"], "last env in slice should win on duplicate key")
}

func TestMergeEnvironmentsIntoGlobal_Error_GetGlobalFails(t *testing.T) {
	st := &mergeTestStore{envs: nil}
	logger := NewDebugLogger(nil)

	err := mergeEnvironmentsIntoGlobal(context.Background(), st, nil, logger)
	assert.Error(t, err)
}
