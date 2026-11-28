//go:build e2e

package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

// withHomeStore opens the quark DB under home, runs fn, and closes it again so
// the binary can use the database while the test is not.
func withHomeStore(t *testing.T, home string, fn func(st *store.Store)) {
	t.Helper()
	st, err := store.New(filepath.Join(home, ".quark", "quark.db"), store.WithCacheSize(100))
	require.NoError(t, err)
	defer st.Close()
	fn(st)
}

// seedShopTree stores a "Shop" collection with an "Orders" child. The requests
// cover each shape the file format must carry: headers with repeated values,
// bearer, basic and API-key auth, a body, a disabled request, and stored
// headers that are not valid JSON. It returns the number of requests.
func seedShopTree(t *testing.T, home string) int {
	t.Helper()
	ctx := context.Background()
	count := 0
	withHomeStore(t, home, func(st *store.Store) {
		shop := &domain.Collection{Name: "Shop", Description: "storefront"}
		require.NoError(t, st.SaveCollection(ctx, shop))
		orders := &domain.Collection{Name: "Orders", ParentID: shop.ID}
		require.NoError(t, st.SaveCollection(ctx, orders))

		requests := []*domain.Request{
			{
				CollectionID: shop.ID, Name: "List items", Method: "GET",
				URL:      "https://shop.test/items",
				Headers:  `{"X-Trace":"abc","Accept":["application/json","text/plain"]}`,
				AuthType: "bearer", AuthConfig: `{"token":"t0k"}`, Enabled: true,
			},
			{
				CollectionID: orders.ID, Name: "Create order", Method: "POST",
				URL: "https://shop.test/orders", Body: "{\n  \"id\": 1\n}", Enabled: true,
			},
			{
				CollectionID: orders.ID, Name: "Legacy", Method: "GET",
				URL: "https://shop.test/legacy", Headers: `not json`, Enabled: false,
			},
			{
				CollectionID: orders.ID, Name: "Basic login", Method: "GET",
				URL: "https://shop.test/basic", AuthType: "basic",
				AuthConfig: `{"username":"ana","password":"pw"}`, Enabled: true,
			},
			{
				CollectionID: orders.ID, Name: "Key header", Method: "GET",
				URL: "https://shop.test/key", AuthType: "api_key",
				AuthConfig: `{"in":"header","name":"X-Key","value":"v1"}`, Enabled: true,
			},
		}
		for _, req := range requests {
			require.NoError(t, st.SaveRequest(ctx, req))
		}
		count = len(requests)

		prod := &domain.Environment{
			ID: uuid.NewString(), CollectionID: shop.ID, Name: "prod",
		}
		prod.SetVars(map[string]string{"base_url": "https://shop.test"})
		require.NoError(t, st.SaveEnvironment(ctx, prod))
	})
	return count
}

// importedCollections returns the collections stored under home, keyed by
// their path (for example "Shop/Orders").
func importedCollections(t *testing.T, home string) map[string]*domain.Collection {
	t.Helper()
	var paths map[string]*domain.Collection
	withHomeStore(t, home, func(st *store.Store) {
		paths = collectionPaths(t, st)
	})
	return paths
}

func TestCLI_ExportWritesCollectionTreeAndImportRestoresIt(t *testing.T) {
	src, _ := setupHomeWithCollection(t)
	total := seedShopTree(t, src)

	out := t.TempDir()
	stdout, stderr, code := runQuarkWithHome(t, src, "export", "Shop", out)
	require.Equal(t, 0, code, "stderr: %s", stderr)
	require.Equal(t, 5, total)
	exportDir := filepath.Join(out, "shop")
	assert.Contains(t, stdout, `Exported 5 requests from "Shop" to `+exportDir)

	root, err := os.ReadFile(filepath.Join(exportDir, "collection.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(root), "name: Shop")
	assert.Contains(t, string(root), "description: storefront")

	list, err := os.ReadFile(filepath.Join(exportDir, "list-items.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(list), "token: t0k")
	assert.Contains(t, string(list), "X-Trace")

	orders := filepath.Join(exportDir, "orders")
	create, err := os.ReadFile(filepath.Join(orders, "create-order.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(create), "body: |-")
	legacy, err := os.ReadFile(filepath.Join(orders, "legacy.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(legacy), "disabled: true")
	assert.Contains(t, string(legacy), "headers_json:")

	dst, _ := setupHomeWithCollection(t)
	stdout, stderr, code = runQuarkWithHome(t, dst, "import", "dir", out)
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, `Imported 5 requests and`)
	assert.Contains(t, stdout, `into "Shop"`)

	paths := importedCollections(t, dst)
	require.Contains(t, paths, "Shop")
	require.Contains(t, paths, "Shop/Orders")
	withHomeStore(t, dst, func(st *store.Store) {
		ctx := context.Background()
		requests, err := st.ListRequests(ctx, paths["Shop/Orders"].ID)
		require.NoError(t, err)
		byName := make(map[string]*domain.Request, len(requests))
		for _, req := range requests {
			byName[req.Name] = req
		}
		require.Len(t, requests, 4)
		assert.Equal(t, `not json`, byName["Legacy"].Headers,
			"invalid stored headers survive a round trip")
		assert.False(t, byName["Legacy"].Enabled)
		assert.Equal(t, "{\n  \"id\": 1\n}", byName["Create order"].Body)
		assert.Equal(t, "basic", byName["Basic login"].AuthType)
		assert.Equal(t, "api_key", byName["Key header"].AuthType)
	})
}

func TestCLI_ImportDirHandlesDuplicateNamesByAction(t *testing.T) {
	src, _ := setupHomeWithCollection(t)
	seedShopTree(t, src)
	out := t.TempDir()
	_, stderr, code := runQuarkWithHome(t, src, "export", "Shop", out)
	require.Equal(t, 0, code, "stderr: %s", stderr)

	dst, _ := setupHomeWithCollection(t)
	steps := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "first import keeps the name",
			args: []string{"import", "dir", out},
			want: `into "Shop"`,
		},
		{
			name: "default duplicates under a numbered name",
			args: []string{"import", "dir", out},
			want: `into "Shop 1" (duplicated)`,
		},
		{
			name: "skip leaves the existing collection",
			args: []string{"import", "dir", out, "--on-duplicate", "skip"},
			want: `Skipped "Shop" (5 requests)`,
		},
		{
			name: "merge adds only what is missing",
			args: []string{"import", "dir", out, "--on-duplicate", "merge"},
			want: `into "Shop" (merged)`,
		},
		{
			name: "replace swaps the collection",
			args: []string{"import", "dir", out, "--on-duplicate", "replace"},
			want: `into "Shop" (replaced)`,
		},
		{
			name: "a name override imports under the new name",
			args: []string{"import", "dir", out, "-n", "Renamed"},
			want: `into "Renamed"`,
		},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			stdout, stderr, code := runQuarkWithHome(t, dst, step.args...)
			require.Equal(t, 0, code, "stderr: %s", stderr)
			assert.Contains(t, stdout, step.want)
		})
	}
}

func TestCLI_ImportDirReadsEveryCollectionUnderARoot(t *testing.T) {
	src, _ := setupHomeWithCollection(t)
	seedShopTree(t, src)
	root := t.TempDir()
	_, stderr, code := runQuarkWithHome(t, src, "export", "Shop", root)
	require.Equal(t, 0, code, "stderr: %s", stderr)
	_, stderr, code = runQuarkWithHome(t, src, "export", "API", root)
	require.Equal(t, 0, code, "stderr: %s", stderr)

	dst, _ := setupHomeWithCollection(t)
	stdout, stderr, code := runQuarkWithHome(t, dst, "import", "dir", root)
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, `into "Shop"`)
	// The destination already has an "API" collection, so the export is duplicated.
	assert.Contains(t, stdout, `into "API 1" (duplicated)`)

	_, stderr, code = runQuarkWithHome(t, dst, "import", "dir", root, "-n", "Both")
	assert.NotEqual(t, 0, code)
	assert.Contains(t, stderr, "needs a single collection")
}

func TestCLI_ReExportRemovesOnlyFilesItWrote(t *testing.T) {
	home, _ := setupHomeWithCollection(t)
	seedShopTree(t, home)
	out := t.TempDir()
	_, stderr, code := runQuarkWithHome(t, home, "export", "Shop", out)
	require.Equal(t, 0, code, "stderr: %s", stderr)

	// A file the export did not write must survive the next export.
	notes := filepath.Join(out, "shop", "notes.txt")
	require.NoError(t, os.WriteFile(notes, []byte("keep me"), 0o600))

	withHomeStore(t, home, func(st *store.Store) {
		ctx := context.Background()
		orders := collectionPaths(t, st)["Shop/Orders"]
		require.NotNil(t, orders)
		requests, err := st.ListRequests(ctx, orders.ID)
		require.NoError(t, err)
		for _, req := range requests {
			if req.Name == "Legacy" {
				require.NoError(t, st.DeleteRequest(ctx, req.ID))
			}
		}
	})

	stdout, stderr, code := runQuarkWithHome(t, home, "export", "Shop", out)
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, `Exported 4 requests from "Shop"`)
	_, err := os.Stat(filepath.Join(out, "shop", "orders", "legacy.yaml"))
	assert.ErrorIs(t, err, os.ErrNotExist, "the file for the deleted request is removed")
	_, err = os.Stat(filepath.Join(out, "shop", "orders", "create-order.yaml"))
	assert.NoError(t, err, "files for requests that remain are rewritten")
	data, err := os.ReadFile(notes)
	require.NoError(t, err)
	assert.Equal(t, "keep me", string(data))
}

func TestCLI_FilesCommandsReportErrors(t *testing.T) {
	home, _ := setupHomeWithCollection(t)
	seedShopTree(t, home)
	out := t.TempDir()

	_, stderr, code := runQuarkWithHome(t, home, "export", "Missing", out)
	assert.NotEqual(t, 0, code)
	assert.Contains(t, stderr, "export:")
	assert.Contains(t, stderr, "not found")

	// A plain file where the export would write its directory blocks the export.
	blocked := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(blocked, "shop"), []byte("x"), 0o600))
	_, stderr, code = runQuarkWithHome(t, home, "export", "Shop", blocked)
	assert.NotEqual(t, 0, code)
	assert.Contains(t, stderr, "is not a directory")

	_, stderr, code = runQuarkWithHome(t, home, "import", "dir", t.TempDir())
	assert.NotEqual(t, 0, code)
	assert.Contains(t, stderr, "no collection.yaml")

	_, stderr, code = runQuarkWithHome(t, home, "import", "dir", out, "--on-duplicate", "bogus")
	assert.NotEqual(t, 0, code)
	assert.Contains(t, stderr, "invalid --on-duplicate")
}
