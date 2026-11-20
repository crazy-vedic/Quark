package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

type envTestStore struct {
	store.Store
	envs      []*domain.Environment
	cols      []*domain.Collection
	activeEnv map[string]string
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

func (s *envTestStore) SetActiveEnvironment(ctx context.Context, collectionID, envID string) error {
	if s.activeEnv == nil {
		s.activeEnv = make(map[string]string)
	}
	s.activeEnv[collectionID] = envID
	return nil
}

func (s *envTestStore) GetActiveEnvironment(
	ctx context.Context,
	collectionID string,
) (string, error) {
	if s.activeEnv == nil {
		return "", nil
	}
	return s.activeEnv[collectionID], nil
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

func TestEnvActive(t *testing.T) {
	s := &envTestStore{
		envs: []*domain.Environment{
			{ID: "c2", CollectionID: "col1", Name: "dev", Data: `{}`},
		},
	}

	var buf bytes.Buffer
	err := envActive(context.Background(), s, "col1", "dev", &buf)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "dev")

	activeID, err := s.GetActiveEnvironment(context.Background(), "col1")
	require.NoError(t, err)
	assert.Equal(t, "c2", activeID)
}

func TestEnvActive_InvalidEnv(t *testing.T) {
	s := &envTestStore{
		envs: []*domain.Environment{
			{ID: "c1", CollectionID: "col1", Name: "default", Data: `{}`},
		},
	}

	err := envActive(context.Background(), s, "col1", "missing", &bytes.Buffer{})
	assert.Error(t, err)
}

func TestEnvActive_RejectsGlobal(t *testing.T) {
	s := &envTestStore{
		envs: []*domain.Environment{
			{ID: "g1", Name: "global", Data: `{}`},
		},
	}

	err := envActive(context.Background(), s, "col1", "global", &bytes.Buffer{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "global")
}

// A collection name that does not exist is a mistake, not an empty section.
func TestEnvListCmd_UnknownCollectionIsAnError(t *testing.T) {
	ctx := context.Background()
	st, err := store.New(filepath.Join(t.TempDir(), "quark.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	require.NoError(t, st.SaveCollection(ctx, &domain.Collection{ID: "col1", Name: "API"}))

	cmd := NewEnvCmd(st)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"list", "no-such-collection"})
	cmd.SetContext(ctx)

	err = cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no-such-collection")
	assert.NotContains(
		t,
		out.String(),
		"Global environments",
		"no listing for an unknown collection",
	)
}

// If an environment's stored data is corrupt, env set must refuse to write.
// Treating it as empty would replace every existing variable with the one
// new key.
func TestEnvSetCmd_RefusesToOverwriteCorruptEnvironment(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(filepath.Join(t.TempDir(), "quark.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, s.SaveCollection(ctx, &domain.Collection{ID: "col1", Name: "API"}))

	// Simulate damage on disk: a truncated JSON object that still holds data.
	const corrupt = `{"api_key":"keep-me","url":"https://`
	_, err = s.DB().ExecContext(ctx, `UPDATE environments SET data = ?`, corrupt)
	require.NoError(t, err)

	for _, args := range [][]string{
		{"set", "col1", "default", "new", "value"},
		{"set-global", "new", "value"},
	} {
		cmd := NewEnvCmd(s)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(args)
		cmd.SetContext(ctx)
		require.Error(t, cmd.Execute(), "%v must fail on corrupt data", args)
	}

	var remaining int
	require.NoError(t, s.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM environments WHERE data = ?`, corrupt).Scan(&remaining))
	var total int
	require.NoError(t, s.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM environments`).Scan(&total))
	assert.Equal(t, total, remaining, "corrupt data must be left for the user to repair")
}
