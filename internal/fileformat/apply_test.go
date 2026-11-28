package fileformat

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

type flatRequest struct {
	Name, Method, URL, Body, AuthType string
	Enabled                           bool
	Headers                           map[string][]string
	Auth                              domain.AuthConfig
}

type flatCollection struct {
	Name, Description string
	Requests          []flatRequest
	Environments      []Environment
	Children          []flatCollection
}

// flatten drops IDs, timestamps and JSON formatting so two trees can be
// compared by meaning.
func flatten(t *testing.T, c *Collection) flatCollection {
	t.Helper()
	out := flatCollection{Name: c.Name, Description: c.Description, Environments: c.Environments}
	for _, req := range c.Requests {
		headers, err := domain.ParseHeaders(req.Headers)
		require.NoError(t, err)
		auth, err := domain.ParseAuthConfig(req.AuthConfig)
		require.NoError(t, err)
		out.Requests = append(out.Requests, flatRequest{
			Name: req.Name, Method: req.Method, URL: req.URL, Body: req.Body,
			AuthType: req.AuthType, Enabled: req.Enabled, Headers: headers, Auth: auth,
		})
	}
	for _, child := range c.Children {
		out.Children = append(out.Children, flatten(t, child))
	}
	return out
}

func rootByName(t *testing.T, st *store.Store, name string) *domain.Collection {
	t.Helper()
	cols, err := st.ListCollections(context.Background())
	require.NoError(t, err)
	for _, c := range cols {
		if c.ParentID == "" && c.Name == name {
			return c
		}
	}
	t.Fatalf("collection %q not found", name)
	return nil
}

func TestExportImportRoundTrip(t *testing.T) {
	ctx := context.Background()
	src := newStore(t)
	api := seed(t, src)
	original, err := Load(ctx, src, api.ID)
	require.NoError(t, err)
	path, err := Write(t.TempDir(), original)
	require.NoError(t, err)

	read, err := Read(path)
	require.NoError(t, err)
	dst := newStore(t)
	res, err := Apply(ctx, dst, read, ActionDuplicate)
	require.NoError(t, err)
	assert.Equal(t, Result{CollectionName: "API", Imported: 4, Environments: 2}, res)

	imported, err := Load(ctx, dst, rootByName(t, dst, "API").ID)
	require.NoError(t, err)
	assert.Equal(t, flatten(t, original), flatten(t, imported))

	// Exporting the imported copy yields the same files.
	again, err := Write(t.TempDir(), imported)
	require.NoError(t, err)
	assert.Equal(t, readTree(t, path), readTree(t, again))
}

func TestApplyDuplicateActions(t *testing.T) {
	ctx := context.Background()
	incoming := func() *Collection {
		return &Collection{
			Name: "API",
			Requests: []*domain.Request{
				{Name: "Old", Method: "PUT", URL: "http://new/old"},
				{Name: "New", Method: "GET", URL: "http://new/new"},
				{Name: "New", Method: "GET", URL: "http://new/new2"},
			},
			Environments: []Environment{
				{Name: "default", Vars: map[string]string{"a": "incoming", "b": "2"}},
				{Name: "Prod", Vars: map[string]string{"p": "1"}},
			},
			Children: []*Collection{{
				Name:     "Users",
				Requests: []*domain.Request{{Name: "Get", Method: "GET", URL: "http://u"}},
			}},
		}
	}
	setup := func(t *testing.T) *store.Store {
		st := newStore(t)
		col := &domain.Collection{Name: "API"}
		require.NoError(t, st.SaveCollection(ctx, col))
		require.NoError(t, st.SaveRequest(ctx, &domain.Request{
			CollectionID: col.ID, Name: "Old", Method: "GET", URL: "http://old",
		}))
		users := &domain.Collection{Name: "Users", ParentID: col.ID}
		require.NoError(t, st.SaveCollection(ctx, users))
		envs, err := st.ListCollectionEnvironments(ctx, col.ID)
		require.NoError(t, err)
		envs[0].SetVars(map[string]string{"a": "local"})
		require.NoError(t, st.SaveEnvironment(ctx, envs[0]))
		return st
	}

	t.Run("skip", func(t *testing.T) {
		st := setup(t)
		res, err := Apply(ctx, st, incoming(), ActionSkip)
		require.NoError(t, err)
		assert.Equal(t, ActionSkip, res.Action)
		assert.Equal(t, 4, res.Skipped)
		reqs, err := st.ListRequests(ctx, rootByName(t, st, "API").ID)
		require.NoError(t, err)
		assert.Len(t, reqs, 1)
	})

	t.Run("duplicate", func(t *testing.T) {
		st := setup(t)
		res, err := Apply(ctx, st, incoming(), ActionDuplicate)
		require.NoError(t, err)
		assert.Equal(t, "API 1", res.CollectionName)
		copyTree, err := Load(ctx, st, rootByName(t, st, "API 1").ID)
		require.NoError(t, err)
		names := []string{}
		for _, r := range copyTree.Requests {
			names = append(names, r.Name)
		}
		assert.Equal(t, []string{"Old", "New", "New (1)"}, names)
		require.Len(t, copyTree.Children, 1)
	})

	t.Run("replace", func(t *testing.T) {
		st := setup(t)
		res, err := Apply(ctx, st, incoming(), ActionReplace)
		require.NoError(t, err)
		assert.Equal(t, ActionReplace, res.Action)
		tree, err := Load(ctx, st, rootByName(t, st, "API").ID)
		require.NoError(t, err)
		require.Len(t, tree.Requests, 3)
		assert.Equal(t, "PUT", tree.Requests[0].Method)
		assert.Equal(t, []Environment{
			{Name: "Prod", Vars: map[string]string{"p": "1"}},
			{Name: "default", Vars: map[string]string{"a": "incoming", "b": "2"}},
		}, tree.Environments)
		cols, err := st.ListCollections(ctx)
		require.NoError(t, err)
		assert.Len(t, cols, 2, "old nested collection removed with its parent")
	})

	t.Run("merge", func(t *testing.T) {
		st := setup(t)
		res, err := Apply(ctx, st, incoming(), ActionMerge)
		require.NoError(t, err)
		assert.Equal(t, ActionMerge, res.Action)
		assert.Equal(t, 3, res.Imported)
		assert.Equal(t, 1, res.Skipped)
		assert.Equal(t, []string{
			`API: environment "default" variable "a" already set; existing value kept`,
		}, res.Warnings)
		tree, err := Load(ctx, st, rootByName(t, st, "API").ID)
		require.NoError(t, err)
		require.Len(t, tree.Requests, 3)
		assert.Equal(t, "GET", tree.Requests[0].Method, "existing request kept")
		assert.Equal(t, "New (1)", tree.Requests[2].Name)
		assert.Equal(t, []Environment{
			{Name: "Prod", Vars: map[string]string{"p": "1"}},
			{Name: "default", Vars: map[string]string{"a": "local", "b": "2"}},
		}, tree.Environments)
		require.Len(t, tree.Children, 1, "existing nested collection reused")
		assert.Len(t, tree.Children[0].Requests, 1)

		// Merging the same files again changes nothing.
		res, err = Apply(ctx, st, incoming(), ActionMerge)
		require.NoError(t, err)
		assert.Equal(t, 0, res.Imported)
	})

	t.Run("no conflict", func(t *testing.T) {
		st := newStore(t)
		res, err := Apply(ctx, st, incoming(), ActionMerge)
		require.NoError(t, err)
		assert.Empty(t, res.Action)
		assert.Equal(t, 4, res.Imported)
	})
}

type failingApplyStore struct {
	ApplyStore
	listErr, txErr error
}

func (f failingApplyStore) ListCollections(context.Context) ([]*domain.Collection, error) {
	return nil, f.listErr
}

func (f failingApplyStore) BeginTransaction(context.Context) (store.TransactionalWriter, error) {
	return nil, f.txErr
}

func TestApplyStoreErrors(t *testing.T) {
	boom := errors.New("boom")
	_, err := Apply(
		context.Background(), failingApplyStore{listErr: boom}, &Collection{}, ActionSkip,
	)
	require.ErrorIs(t, err, boom)
	_, err = Apply(context.Background(), failingApplyStore{txErr: boom}, &Collection{}, ActionSkip)
	require.ErrorIs(t, err, boom)
}

func TestParseDuplicateAction(t *testing.T) {
	for _, s := range []string{"replace", "duplicate", "merge", "skip"} {
		a, err := ParseDuplicateAction(s)
		require.NoError(t, err)
		assert.Equal(t, DuplicateAction(s), a)
	}
	_, err := ParseDuplicateAction("overwrite")
	require.ErrorContains(t, err, "invalid --on-duplicate")
}
