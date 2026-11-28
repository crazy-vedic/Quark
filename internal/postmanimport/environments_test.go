package postmanimport

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

// mergeTestStore is an in-memory environment store for Global merge tests.
type mergeTestStore struct {
	Store
	envs      []*domain.Environment
	saveCalls int
	saveErr   error
}

func (s *mergeTestStore) GetGlobalEnvironment(context.Context) (*domain.Environment, error) {
	for _, e := range s.envs {
		if e.IsGlobal() {
			return e, nil
		}
	}
	return nil, store.ErrNotFound
}

func (s *mergeTestStore) SaveEnvironment(_ context.Context, env *domain.Environment) error {
	s.saveCalls++
	if s.saveErr != nil {
		return s.saveErr
	}
	for i, e := range s.envs {
		if e.ID == env.ID {
			s.envs[i] = env
			return nil
		}
	}
	s.envs = append(s.envs, env)
	return nil
}

func mergeIntoGlobal(t *testing.T, st Store, files ...parsedEnvironmentFile) ([]string, error) {
	t.Helper()
	return mergeParsedEnvironmentsIntoGlobal(context.Background(), st, files, nopLogger{})
}

func TestMergeIntoGlobal_EmptyMergeKeepsGlobal(t *testing.T) {
	global := &domain.Environment{ID: "global", Name: "global", Data: `{"existing": "value"}`}
	st := &mergeTestStore{envs: []*domain.Environment{global}}

	_, err := mergeIntoGlobal(t, st)
	require.NoError(t, err)
	assert.Equal(t, "value", st.envs[0].Vars()["existing"])
	assert.Zero(t, st.saveCalls, "nothing changed, nothing saved")
}

func TestMergeIntoGlobal_MergesVarsAndExistingWins(t *testing.T) {
	global := &domain.Environment{ID: "global", Name: "global", Data: `{"key": "global"}`}
	st := &mergeTestStore{envs: []*domain.Environment{global}}

	_, err := mergeIntoGlobal(t, st,
		parsedEnvironmentFile{filename: "dev", vars: map[string]string{"a": "1", "key": "dev"}},
		parsedEnvironmentFile{filename: "prod", vars: map[string]string{"b": "2", "key": "prod"}},
	)
	require.NoError(t, err)
	vars := st.envs[0].Vars()
	assert.Equal(t, "1", vars["a"])
	assert.Equal(t, "2", vars["b"])
	assert.Equal(t, "global", vars["key"])
}

func TestMergeIntoGlobal_FirstImportedValueWinsAndSavesOnce(t *testing.T) {
	global := &domain.Environment{ID: "global", Name: "global", Data: `{}`}
	st := &mergeTestStore{envs: []*domain.Environment{global}}
	warnings, err := mergeIntoGlobal(t, st,
		parsedEnvironmentFile{
			filename: "a.json",
			vars:     map[string]string{"key": "first-secret", "same": "value"},
		},
		parsedEnvironmentFile{
			filename: "b.json",
			vars:     map[string]string{"key": "later-secret", "same": "value"},
		},
	)
	require.NoError(t, err)
	assert.Equal(t, "first-secret", st.envs[0].Vars()["key"])
	assert.Equal(t, 1, st.saveCalls)
	joined := strings.Join(warnings, " ")
	assert.Contains(t, joined, `variable "key"`)
	assert.NotContains(t, joined, "first-secret")
	assert.NotContains(t, joined, "later-secret")
}

func TestMergeIntoGlobal_Errors(t *testing.T) {
	_, err := mergeIntoGlobal(t, &mergeTestStore{})
	assert.ErrorIs(t, err, store.ErrNotFound)

	global := &domain.Environment{ID: "global", Name: "global", Data: `{}`}
	failing := &mergeTestStore{
		envs:    []*domain.Environment{global},
		saveErr: errors.New("disk full"),
	}
	_, err = mergeIntoGlobal(t, failing,
		parsedEnvironmentFile{filename: "a.json", vars: map[string]string{"k": "v"}})
	assert.ErrorContains(t, err, "disk full")
}

func TestImportParsedEnvironmentsForCollection_PreservesNamedEnvironmentsAndLocalData(
	t *testing.T,
) {
	st := newTestStore(t)
	ctx := context.Background()
	collection := &domain.Collection{ID: "api", Name: "API"}
	require.NoError(t, st.SaveCollection(ctx, collection))
	local := &domain.Environment{ID: "local-dev", CollectionID: collection.ID, Name: "Development"}
	local.SetVars(map[string]string{"url": "http://local"})
	require.NoError(t, st.SaveEnvironment(ctx, local))

	imported, warnings, err := importParsedEnvironmentsForCollection(
		ctx,
		st,
		collection.ID,
		[]parsedEnvironmentFile{
			{
				filename: "development.json",
				name:     "Development",
				vars:     map[string]string{"url": "https://dev.example"},
			},
			{
				filename: "production.json",
				name:     "Production",
				vars:     map[string]string{"url": "https://api.example", "token": "secret"},
			},
		},
	)
	require.NoError(t, err)
	assert.Equal(t, 1, imported)
	assert.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "Development")
	assert.NotContains(t, warnings[0], "http://local")
	assert.NotContains(t, warnings[0], "https://dev.example")

	development, err := st.GetEnvironmentByName(ctx, collection.ID, "Development")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"url": "http://local"}, development.Vars())
	production, err := st.GetEnvironmentByName(ctx, collection.ID, "Production")
	require.NoError(t, err)
	assert.Equal(
		t,
		map[string]string{"url": "https://api.example", "token": "secret"},
		production.Vars(),
	)
	global, err := st.GetGlobalEnvironment(ctx)
	require.NoError(t, err)
	assert.Empty(t, global.Vars(), "Postman environment values must not leak into Global")
}

func TestParseEnvironmentsInDir(t *testing.T) {
	dir := t.TempDir()
	assert.Empty(t, parseEnvironmentsInDir(dir, nopLogger{}).files)
	envDir := filepath.Join(dir, "environment")
	require.NoError(t, os.MkdirAll(filepath.Join(envDir, "ignored"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(envDir, "bad.json"), []byte("invalid"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(envDir, "skip.txt"), []byte("ignored"), 0o600))
	devEnvironment := `{"name":"Dev","values":[{"key":"url","value":"https://dev.example","enabled":true}]}`
	require.NoError(
		t,
		os.WriteFile(filepath.Join(envDir, "dev.json"), []byte(devEnvironment), 0o600),
	)
	parsed := parseEnvironmentsInDir(dir, nopLogger{})
	assert.Len(t, parsed.files, 1)
	assert.Equal(t, "Dev", parsed.files[0].name)
	assert.Len(t, parsed.errors, 1)
}
