package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
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

func TestImportSingleFile_NestedFoldersDoesNotBlockStoreConnection(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "quark.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	file := filepath.Join(t.TempDir(), "nested.postman_collection.json")
	require.NoError(t, os.WriteFile(file, []byte(`{
		"info": {"name": "API", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
		"variable": [{"key":"base_url","value":"https://example.com"}],
		"item": [{"name": "Users", "item": [{"name": "Admin", "item": [{"name": "List", "request": {"method": "GET", "url": {"raw": "https://example.com/users"}}}]}]}]
	}`), 0600))

	cmd := &cobra.Command{}
	globalAction := duplicateAction("")
	stats, err := importSingleFile(
		context.Background(),
		cmd,
		st,
		file,
		"",
		"duplicate",
		&globalAction,
		NewDebugLogger(nil),
	)
	require.NoError(t, err)
	require.Equal(t, 1, stats.imported)

	cols, err := st.ListCollections(context.Background())
	require.NoError(t, err)
	paths := make(map[string]*domain.Collection)
	for _, col := range cols {
		path, err := st.CollectionPath(context.Background(), col.ID)
		require.NoError(t, err)
		paths[path] = col
	}
	require.Contains(t, paths, "API/Users/Admin")
	rootDefault, err := st.GetEnvironmentByName(context.Background(), paths["API"].ID, "default")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com", rootDefault.Vars()["base_url"])
	childDefault, err := st.GetEnvironmentByName(
		context.Background(),
		paths["API/Users/Admin"].ID,
		"default",
	)
	require.NoError(t, err)
	assert.NotContains(
		t,
		childDefault.Vars(),
		"base_url",
		"root variables must not be duplicated into descendants",
	)
	requests, err := st.ListRequests(context.Background(), paths["API/Users/Admin"].ID)
	require.NoError(t, err)
	require.Len(t, requests, 1)
	require.Equal(t, "List", requests[0].Name)
}

func TestImportParsedEnvironmentsForCollection_PreservesNamedEnvironmentsAndLocalData(
	t *testing.T,
) {
	st, err := store.New(filepath.Join(t.TempDir(), "quark.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })
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

func TestDeduplicateImportedRequestNames(t *testing.T) {
	requests := []*domain.Request{
		{Name: "New Request"},
		{Name: "New Request"},
		{Name: "New Request"},
		{Name: "Health"},
		{Name: "Health"},
	}

	deduplicateImportedRequestNames(requests, nil)

	assert.Equal(t, []string{
		"New Request",
		"New Request (1)",
		"New Request (2)",
		"Health",
		"Health (1)",
	}, requestNames(requests))
}

func TestDeduplicateImportedRequestNamesAvoidsExistingAndExplicitNames(t *testing.T) {
	requests := []*domain.Request{
		{Name: "New Request"},
		{Name: "New Request"},
		{Name: "New Request (1)"},
	}

	deduplicateImportedRequestNames(requests, map[string]bool{
		"New Request (1)": true,
	})

	assert.Equal(t, []string{
		"New Request",
		"New Request (2)",
		"New Request (1) (1)",
	}, requestNames(requests))
}

func requestNames(requests []*domain.Request) []string {
	names := make([]string, 0, len(requests))
	for _, req := range requests {
		names = append(names, req.Name)
	}
	return names
}

// --- mergeEnvironmentsIntoGlobal tests ---

type mergeTestStore struct {
	envs      []*domain.Environment
	saveCalls int
	saveErr   error
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

	// Existing Global values always win.
	err := mergeEnvironmentsIntoGlobal(
		context.Background(),
		st,
		[]*domain.Environment{env1, env2},
		logger,
	)
	require.NoError(t, err)

	got, err := st.GetGlobalEnvironment(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "global", got.Vars()["key"])
}

func TestMergeEnvironmentsIntoGlobal_FirstImportedValueWinsAndSavesOnce(t *testing.T) {
	global := &domain.Environment{ID: "global", Name: "global", Data: `{}`}
	first := &domain.Environment{Name: "a.json", Data: `{"key":"first-secret","same":"value"}`}
	second := &domain.Environment{Name: "b.json", Data: `{"key":"later-secret","same":"value"}`}
	st := &mergeTestStore{envs: []*domain.Environment{global}}
	warnings, err := mergeParsedEnvironmentsIntoGlobal(
		context.Background(),
		st,
		[]parsedEnvironmentFile{
			{filename: first.Name, vars: first.Vars()},
			{filename: second.Name, vars: second.Vars()},
		},
		NewDebugLogger(nil),
	)
	require.NoError(t, err)
	assert.Equal(t, "first-secret", st.envs[0].Vars()["key"])
	assert.Equal(t, 1, st.saveCalls)
	joined := strings.Join(warnings, " ")
	assert.Contains(t, joined, `variable "key"`)
	assert.NotContains(t, joined, "first-secret")
	assert.NotContains(t, joined, "later-secret")
}

func TestImportSingleFile_MergeCollectionVariablesPreservesLocalValues(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "quark.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })
	ctx := context.Background()
	collection := &domain.Collection{ID: "existing", Name: "API"}
	require.NoError(t, st.SaveCollection(ctx, collection))
	defaultEnvironment, err := st.GetEnvironmentByName(ctx, collection.ID, "default")
	require.NoError(t, err)
	defaultEnvironment.SetVars(map[string]string{"conflict": "local-secret", "same": "same"})
	require.NoError(t, st.SaveEnvironment(ctx, defaultEnvironment))

	file := filepath.Join(t.TempDir(), "collection.json")
	require.NoError(t, os.WriteFile(file, []byte(`{
		"info":{"name":"API","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
		"variable":[{"key":"conflict","value":"import-secret"},{"key":"same","value":"same"},{"key":"added","value":"new"}],
		"item":[]
	}`), 0600))
	action := actionMerge
	stats, err := importSingleFile(
		ctx,
		&cobra.Command{},
		st,
		file,
		"",
		"merge",
		&action,
		NewDebugLogger(nil),
	)
	require.NoError(t, err)
	assert.Equal(t, 1, stats.warnings)
	assert.NotContains(t, strings.Join(stats.warningMsgs, " "), "local-secret")
	assert.NotContains(t, strings.Join(stats.warningMsgs, " "), "import-secret")
	stored, err := st.GetEnvironmentByName(ctx, collection.ID, "default")
	require.NoError(t, err)
	assert.Equal(
		t,
		map[string]string{"conflict": "local-secret", "same": "same", "added": "new"},
		stored.Vars(),
	)
}

func TestImportBulk_StandaloneEnvironmentsRemainSelectableAndAggregateErrors(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "quark.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })
	ctx := context.Background()
	global, err := st.GetGlobalEnvironment(ctx)
	require.NoError(t, err)
	global.SetVars(map[string]string{"existing": "global-secret"})
	require.NoError(t, st.SaveEnvironment(ctx, global))

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "collection"), 0700))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "environment"), 0700))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "collection", "collection.json"),
		[]byte(`{"info":{"name":"Imported","schema":"v2.1"},"item":[]}`),
		0600,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "environment", "a.json"),
		[]byte(`{
			"name":"A",
			"values":[
				{"key":"shared","value":"first-secret","enabled":true},
				{"key":"existing","value":"import-secret","enabled":true}
			]
		}`),
		0600,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "environment", "b.json"),
		[]byte(`{"name":"B","values":[{"key":"shared","value":"later-secret","enabled":true}]}`),
		0600,
	))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(dir, "environment", "z.json"), []byte(`not-json`), 0600),
	)

	stats, envResult, err := importBulk(
		ctx,
		&cobra.Command{},
		st,
		dir,
		"",
		"duplicate",
		new(duplicateAction),
		NewDebugLogger(nil),
	)
	require.NoError(t, err)
	require.Len(t, stats, 1)
	assert.NoError(t, stats[0].err)
	assert.Equal(t, 2, envResult.imported)
	assert.Len(t, envResult.errors, 1)
	merged, err := st.GetGlobalEnvironment(ctx)
	require.NoError(t, err)
	assert.Equal(t, "global-secret", merged.Vars()["existing"])
	assert.NotContains(t, merged.Vars(), "shared")
	collection, err := st.ResolveCollectionPath(ctx, "Imported")
	require.NoError(t, err)
	first, err := st.GetEnvironmentByName(ctx, collection.ID, "A")
	require.NoError(t, err)
	assert.Equal(t, "first-secret", first.Vars()["shared"])
	assert.Equal(t, "import-secret", first.Vars()["existing"])
	second, err := st.GetEnvironmentByName(ctx, collection.ID, "B")
	require.NoError(t, err)
	assert.Equal(t, "later-secret", second.Vars()["shared"])
	output := strings.Join(append(envResult.warnings, envResult.errors...), " ")
	for _, secret := range []string{"global-secret", "import-secret", "first-secret", "later-secret"} {
		assert.NotContains(t, output, secret)
	}
	var globalCount int
	require.NoError(
		t,
		st.DB().
			QueryRow(`SELECT COUNT(*) FROM environments WHERE collection_id IS NULL`).
			Scan(&globalCount),
	)
	assert.Equal(t, 1, globalCount)
}

func TestMergeEnvironmentsIntoGlobal_Error_GetGlobalFails(t *testing.T) {
	st := &mergeTestStore{envs: nil}
	logger := NewDebugLogger(nil)

	err := mergeEnvironmentsIntoGlobal(context.Background(), st, nil, logger)
	assert.Error(t, err)
}
