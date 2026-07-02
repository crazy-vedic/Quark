package cli

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

type envTestStore struct {
	store.Store
	envs []*domain.Environment
	cols []*domain.Collection
}

func (s *envTestStore) GetEnvironment(ctx context.Context, id string) (*domain.Environment, error) {
	for _, e := range s.envs {
		if e.ID == id {
			return e, nil
		}
	}
	return nil, store.ErrNotFound
}

func (s *envTestStore) GetGlobalEnvironment(ctx context.Context) (*domain.Environment, error) {
	for _, e := range s.envs {
		if e.IsGlobal() {
			return e, nil
		}
	}
	return nil, store.ErrNotFound
}

func (s *envTestStore) ListEnvironments(
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

func (s *envTestStore) ListCollectionEnvironments(
	ctx context.Context,
	collectionID string,
) ([]*domain.Environment, error) {
	return s.ListEnvironments(ctx, collectionID)
}

func (s *envTestStore) GetEnvironmentByName(
	ctx context.Context,
	collectionID, name string,
) (*domain.Environment, error) {
	for _, e := range s.envs {
		if e.CollectionID == collectionID && e.Name == name {
			return e, nil
		}
	}
	return nil, store.ErrNotFound
}

func (s *envTestStore) SaveEnvironment(ctx context.Context, env *domain.Environment) error {
	for i, e := range s.envs {
		if e.ID == env.ID {
			s.envs[i] = env
			return nil
		}
	}
	s.envs = append(s.envs, env)
	return nil
}

func (s *envTestStore) DeleteEnvironment(ctx context.Context, id string) error {
	for i, e := range s.envs {
		if e.ID == id {
			s.envs = append(s.envs[:i], s.envs[i+1:]...)
			return nil
		}
	}
	return store.ErrNotFound
}

func (s *envTestStore) CreateDefaultEnvironment(
	ctx context.Context,
	collectionID string,
) (*domain.Environment, error) {
	return &domain.Environment{}, nil
}

func (s *envTestStore) ListAllEnvironments(ctx context.Context) ([]*domain.Environment, error) {
	var out []*domain.Environment
	for _, e := range s.envs {
		if e.IsGlobal() {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func (s *envTestStore) ListCollections(ctx context.Context) ([]*domain.Collection, error) {
	return s.cols, nil
}

func TestEnvList(t *testing.T) {
	s := &envTestStore{
		envs: []*domain.Environment{
			{ID: "g1", Name: "global", Data: `{"url":"http://localhost"}`},
			{ID: "c1", CollectionID: "col1", Name: "default", Data: `{}`},
			{ID: "c2", CollectionID: "col1", Name: "dev", Data: `{"url":"http://dev.local"}`},
		},
		cols: []*domain.Collection{
			{ID: "col1", Name: "API"},
		},
	}

	var buf bytes.Buffer
	err := envList(context.Background(), s, "col1", &buf)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "global")
	assert.Contains(t, buf.String(), "default")
	assert.Contains(t, buf.String(), "dev")
}

func TestEnvListAll(t *testing.T) {
	s := &envTestStore{
		envs: []*domain.Environment{
			{ID: "g1", Name: "global", Data: `{"url":"http://localhost"}`},
			{ID: "c1", CollectionID: "col1", Name: "default", Data: `{}`},
			{ID: "c2", CollectionID: "col1", Name: "dev", Data: `{"url":"http://dev.local"}`},
			{
				ID:           "c3",
				CollectionID: "col2",
				Name:         "default",
				Data:         `{"base":"https://api.example.com"}`,
			},
		},
		cols: []*domain.Collection{
			{ID: "col1", Name: "API"},
			{ID: "col2", Name: "Web"},
		},
	}

	var buf bytes.Buffer
	err := envList(context.Background(), s, "", &buf)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Global environments")
	assert.Contains(t, buf.String(), "API")
	assert.Contains(t, buf.String(), "Web")
	assert.Contains(t, buf.String(), "dev")
}

func TestEnvListAllNoCollections(t *testing.T) {
	s := &envTestStore{
		envs: []*domain.Environment{
			{ID: "g1", Name: "global", Data: `{"url":"http://localhost"}`},
		},
		cols: nil,
	}

	var buf bytes.Buffer
	err := envList(context.Background(), s, "", &buf)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Global environments")
	assert.Contains(t, buf.String(), "No collections found")
}

func TestEnvSetGlobal(t *testing.T) {
	s := &envTestStore{
		envs: []*domain.Environment{
			{ID: "g1", Name: "global", Data: `{}`},
		},
	}

	err := envSetGlobal(context.Background(), s, "url", "http://localhost")
	require.NoError(t, err)

	global, err := s.GetGlobalEnvironment(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "http://localhost", global.Vars()["url"])
}

func TestEnvSetAndDelete(t *testing.T) {
	s := &envTestStore{
		envs: []*domain.Environment{
			{ID: "c1", CollectionID: "col1", Name: "default", Data: `{}`},
		},
	}

	err := envSet(context.Background(), s, "col1", "default", "url", "http://localhost")
	require.NoError(t, err)

	env, err := s.GetEnvironmentByName(context.Background(), "col1", "default")
	require.NoError(t, err)
	assert.Equal(t, "http://localhost", env.Vars()["url"])

	err = envDelete(context.Background(), s, "col1", "default")
	assert.Error(t, err) // cannot delete default
}

func TestEnvCreate(t *testing.T) {
	s := &envTestStore{envs: nil}

	err := envCreate(context.Background(), s, "col1", "dev")
	require.NoError(t, err)

	env, err := s.GetEnvironmentByName(context.Background(), "col1", "dev")
	require.NoError(t, err)
	assert.Equal(t, "dev", env.Name)
	assert.Equal(t, "col1", env.CollectionID)
}
