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
	"github.com/crazy-vedic/quark/internal/postman"
	"github.com/crazy-vedic/quark/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "quark.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })
	return st
}

func newTestRun(st Store, opts Options) *run {
	if opts.OnDuplicate == "" {
		opts.OnDuplicate = ActionDuplicate
	}
	return &run{Service: New(st, nil), opts: opts, importer: postman.NewImporter()}
}

func writeCollectionFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

const healthCollection = `{"info":{"name":"API","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},` +
	`"item":[{"name":"Health","request":{"method":"GET","url":"https://example.test/health"}}]}`

// importFileForTest returns the CollectionResult of importFile together with
// its error, for tests that check both.
func (r *run) importFileForTest(ctx context.Context, path string) (CollectionResult, error) {
	result := r.importFile(ctx, path)
	return result, result.Err
}

func TestImportFile_NestedFoldersDoesNotBlockStoreConnection(t *testing.T) {
	st := newTestStore(t)
	file := writeCollectionFile(t, t.TempDir(), "nested.postman_collection.json", `{
		"info": {"name": "API", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
		"variable": [{"key":"base_url","value":"https://example.com"}],
		"item": [{"name": "Users", "item": [{"name": "Admin", "item": [{"name": "List", "request": {"method": "GET", "url": {"raw": "https://example.com/users"}}}]}]}]
	}`)

	result, err := newTestRun(st, Options{}).importFileForTest(context.Background(), file)
	require.NoError(t, err)
	require.Equal(t, 1, result.Imported)
	assert.Equal(t, OutcomeCreated, result.Outcome)

	cols, err := st.ListCollections(context.Background())
	require.NoError(t, err)
	paths := make(map[string]*domain.Collection)
	for _, col := range cols {
		path, err := st.CollectionPath(context.Background(), col.ID)
		require.NoError(t, err)
		paths[path] = col
	}
	require.Contains(t, paths, "API/Users/Admin")
	assert.Equal(t, paths["API"].ID, result.ID)
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

func TestImportFile_MergeCollectionVariablesPreservesLocalValues(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	collection := &domain.Collection{ID: "existing", Name: "API"}
	require.NoError(t, st.SaveCollection(ctx, collection))
	defaultEnvironment, err := st.GetEnvironmentByName(ctx, collection.ID, "default")
	require.NoError(t, err)
	defaultEnvironment.SetVars(map[string]string{"conflict": "local-secret", "same": "same"})
	require.NoError(t, st.SaveEnvironment(ctx, defaultEnvironment))

	file := writeCollectionFile(t, t.TempDir(), "collection.json", `{
		"info":{"name":"API","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
		"variable":[{"key":"conflict","value":"import-secret"},{"key":"same","value":"same"},{"key":"added","value":"new"}],
		"item":[]
	}`)
	result, err := newTestRun(st, Options{OnDuplicate: ActionMerge}).importFileForTest(ctx, file)
	require.NoError(t, err)
	assert.Equal(t, OutcomeMerged, result.Outcome)
	assert.Equal(t, collection.ID, result.ID)
	assert.Len(t, result.Warnings, 1)
	assert.NotContains(t, strings.Join(result.Warnings, " "), "local-secret")
	assert.NotContains(t, strings.Join(result.Warnings, " "), "import-secret")
	stored, err := st.GetEnvironmentByName(ctx, collection.ID, "default")
	require.NoError(t, err)
	assert.Equal(
		t,
		map[string]string{"conflict": "local-secret", "same": "same", "added": "new"},
		stored.Vars(),
	)
}

func TestImport_DuplicateActions(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		action      Action
		outcome     Outcome
		name        string
		imported    int
		collections int
	}{
		{ActionDuplicate, OutcomeDuplicated, "API 1", 1, 2},
		{ActionReplace, OutcomeReplaced, "API", 1, 1},
		{ActionMerge, OutcomeMerged, "API", 0, 1},
		{ActionSkip, OutcomeSkipped, "API", 0, 1},
	} {
		t.Run(string(tc.action), func(t *testing.T) {
			st := newTestStore(t)
			existing := &domain.Collection{Name: "API"}
			require.NoError(t, st.SaveCollection(ctx, existing))
			require.NoError(t, st.SaveRequest(ctx, &domain.Request{
				CollectionID: existing.ID,
				Name:         "Health",
				Method:       "GET",
				URL:          "https://example.test",
			}))
			file := writeCollectionFile(t, t.TempDir(), "api.json", healthCollection)

			result, err := New(st, nil).Import(ctx, file, Options{OnDuplicate: tc.action})
			require.NoError(t, err)
			require.Len(t, result.Collections, 1)
			got := result.Collections[0]
			assert.Equal(t, tc.outcome, got.Outcome)
			assert.Equal(t, tc.name, got.Name)
			assert.Equal(t, tc.imported, got.Imported)
			assert.Equal(t, 1, got.Total)
			cols, err := st.ListCollections(ctx)
			require.NoError(t, err)
			assert.Len(t, cols, tc.collections)
		})
	}
}

func TestImport_ResolverIsAskedAndApplyToAllIsRemembered(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	require.NoError(t, st.SaveCollection(ctx, &domain.Collection{Name: "API"}))
	require.NoError(t, st.SaveCollection(ctx, &domain.Collection{Name: "Other"}))
	dir := t.TempDir()
	first := writeCollectionFile(t, dir, "a.json", healthCollection)
	second := writeCollectionFile(t, dir, "b.json",
		strings.Replace(healthCollection, `"name":"API"`, `"name":"Other"`, 1))

	calls := 0
	r := newTestRun(st, Options{Resolve: func(_ context.Context, c Conflict) (Action, bool, error) {
		calls++
		assert.Equal(t, "API", c.Name)
		assert.Len(t, c.IncomingRequests, 1)
		return ActionSkip, true, nil
	}})
	for _, file := range []string{first, second} {
		result, err := r.importFileForTest(ctx, file)
		require.NoError(t, err)
		assert.Equal(t, OutcomeSkipped, result.Outcome)
	}
	assert.Equal(t, 1, calls, "apply-to-all must not ask again")

	failing := newTestRun(
		st,
		Options{Resolve: func(context.Context, Conflict) (Action, bool, error) {
			return "", false, errors.New("cancelled")
		}},
	)
	_, err := failing.importFileForTest(ctx, first)
	require.ErrorContains(t, err, "cancelled")

	invalid := newTestRun(
		st,
		Options{Resolve: func(context.Context, Conflict) (Action, bool, error) {
			return "bogus", false, nil
		}},
	)
	result, err := invalid.importFileForTest(ctx, first)
	require.NoError(t, err)
	assert.Equal(t, OutcomeDuplicated, result.Outcome, "an unknown action falls back to duplicate")
}

func TestImport_SingleFileErrors(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	service := New(st, nil)

	_, err := service.Import(ctx, filepath.Join(t.TempDir(), "missing.json"), Options{})
	require.ErrorIs(t, err, os.ErrNotExist)

	file := writeCollectionFile(t, t.TempDir(), "api.json", healthCollection)
	_, err = service.Import(ctx, file, Options{OnDuplicate: "bogus"})
	require.ErrorContains(t, err, "invalid duplicate action")

	bad := writeCollectionFile(t, t.TempDir(), "bad.json", "not json")
	result, err := service.Import(ctx, bad, Options{})
	require.ErrorContains(t, err, "parse:")
	require.Len(t, result.Collections, 1)
	assert.Equal(t, err, result.Collections[0].Err)
	assert.Equal(t, 1, result.Summary().Failed)
}

func TestImport_CollectionNameOverrideAndIncludeDisabled(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	file := writeCollectionFile(
		t,
		t.TempDir(),
		"api.json",
		`{"info":{"name":"","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},`+
			`"item":[{"name":"H","request":{"method":"GET","url":"https://x.test","header":[{"key":"X-Off","value":"v","disabled":true}]}}]}`,
	)

	result, err := New(st, nil).Import(ctx, file, Options{IncludeDisabled: true})
	require.NoError(t, err)
	assert.Equal(
		t,
		"Imported",
		result.Collections[0].Name,
		"an unnamed collection gets a default name",
	)
	requests, err := st.ListRequests(ctx, result.Collections[0].ID)
	require.NoError(t, err)
	require.Len(t, requests, 1)
	assert.Contains(t, requests[0].Headers, "X-Off")

	result, err = New(st, nil).Import(ctx, file, Options{CollectionName: "Override"})
	require.NoError(t, err)
	assert.Equal(t, "Override", result.Collections[0].Name)
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

func TestUniqueCollectionName(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	for _, name := range []string{"API", "API 1"} {
		require.NoError(t, st.SaveCollection(ctx, &domain.Collection{Name: name}))
	}
	got, err := uniqueCollectionName(ctx, st, "API")
	require.NoError(t, err)
	assert.Equal(t, "API 2", got)
	got, err = uniqueCollectionName(ctx, st, "Fresh")
	require.NoError(t, err)
	assert.Equal(t, "Fresh", got)
}

// flakyStore wraps a Store and injects read failures or a vanishing collection.
type flakyStore struct {
	Store
	listErr       error
	emptyFromCall int // when > 0, ListCollections returns nothing from this call on
	requestsErr   error
	listCalls     int
}

func (s *flakyStore) ListCollections(ctx context.Context) ([]*domain.Collection, error) {
	s.listCalls++
	if s.listErr != nil {
		return nil, s.listErr
	}
	if s.emptyFromCall > 0 && s.listCalls >= s.emptyFromCall {
		return nil, nil
	}
	return s.Store.ListCollections(ctx)
}

func (s *flakyStore) ListRequests(
	ctx context.Context,
	collectionID string,
) ([]*domain.Request, error) {
	if s.requestsErr != nil {
		return nil, s.requestsErr
	}
	return s.Store.ListRequests(ctx, collectionID)
}

func TestImport_ReadErrorsAreReturnedNotDiscarded(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("boom")
	seed := func(t *testing.T) *store.Store {
		st := newTestStore(t)
		require.NoError(t, st.SaveCollection(ctx, &domain.Collection{Name: "API"}))
		return st
	}
	file := writeCollectionFile(t, t.TempDir(), "api.json", healthCollection)

	t.Run("list collections", func(t *testing.T) {
		fs := &flakyStore{Store: seed(t), listErr: boom}
		_, err := New(fs, nil).Import(ctx, file, Options{OnDuplicate: ActionDuplicate})
		assert.ErrorIs(t, err, boom)
	})
	t.Run("list requests during merge", func(t *testing.T) {
		fs := &flakyStore{Store: seed(t), requestsErr: boom}
		result, err := New(fs, nil).Import(ctx, file, Options{OnDuplicate: ActionMerge})
		assert.ErrorIs(t, err, boom)
		require.Len(t, result.Collections, 1)
		assert.ErrorIs(t, result.Collections[0].Err, boom)
	})
	t.Run("merge target vanishes", func(t *testing.T) {
		fs := &flakyStore{Store: seed(t), emptyFromCall: 2}
		result, err := New(fs, nil).Import(ctx, file, Options{OnDuplicate: ActionMerge})
		assert.ErrorContains(t, err, "existing collection not found")
		require.Len(t, result.Collections, 1)
		assert.ErrorContains(t, result.Collections[0].Err, "existing collection not found")
	})
}

func TestImport_ConflictsOnlyConsiderRootCollections(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	// A nested folder named like the import must not count as a conflict.
	parent := &domain.Collection{Name: "Parent"}
	require.NoError(t, st.SaveCollection(ctx, parent))
	require.NoError(t, st.SaveCollection(ctx, &domain.Collection{Name: "API", ParentID: parent.ID}))
	file := writeCollectionFile(t, t.TempDir(), "api.json", healthCollection)

	result, err := New(st, nil).Import(ctx, file, Options{OnDuplicate: ActionReplace})
	require.NoError(t, err)
	assert.Equal(t, OutcomeCreated, result.Collections[0].Outcome)
	cols, err := st.ListCollections(ctx)
	require.NoError(t, err)
	assert.Len(t, cols, 3, "replace must not delete the nested folder")
}

func TestImport_MergeTargetsRootNotNestedFolder(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	parent := &domain.Collection{Name: "Parent"}
	require.NoError(t, st.SaveCollection(ctx, parent))
	nested := &domain.Collection{Name: "API", ParentID: parent.ID}
	require.NoError(t, st.SaveCollection(ctx, nested))
	root := &domain.Collection{Name: "API"}
	require.NoError(t, st.SaveCollection(ctx, root))
	file := writeCollectionFile(t, t.TempDir(), "api.json", healthCollection)

	result, err := New(st, nil).Import(ctx, file, Options{OnDuplicate: ActionMerge})
	require.NoError(t, err)
	got := result.Collections[0]
	assert.Equal(t, OutcomeMerged, got.Outcome)
	assert.Equal(t, root.ID, got.ID)
}

func TestUniqueCollectionName_IgnoresNestedFolders(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	parent := &domain.Collection{Name: "Parent"}
	require.NoError(t, st.SaveCollection(ctx, parent))
	require.NoError(t, st.SaveCollection(ctx, &domain.Collection{Name: "API", ParentID: parent.ID}))
	got, err := uniqueCollectionName(ctx, st, "API")
	require.NoError(t, err)
	assert.Equal(t, "API", got)
}
