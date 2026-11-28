package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

func newFilesTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "quark.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func runFilesCmd(t *testing.T, cmd *cobra.Command, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	cmd.SetContext(context.Background())
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestExportThenImportDir(t *testing.T) {
	ctx := context.Background()
	src := newFilesTestStore(t)
	col := &domain.Collection{Name: "Shop"}
	require.NoError(t, src.SaveCollection(ctx, col))
	require.NoError(t, src.SaveRequest(ctx, &domain.Request{
		CollectionID: col.ID, Name: "Order", Method: "POST", URL: "http://shop/orders",
		Body: "{\n  \"id\": 1\n}", Enabled: true,
	}))
	envs, err := src.ListCollectionEnvironments(ctx, col.ID)
	require.NoError(t, err)
	envs[0].SetVars(map[string]string{"host": "shop"})
	require.NoError(t, src.SaveEnvironment(ctx, envs[0]))

	out := t.TempDir()
	stdout, _, err := runFilesCmd(t, NewExportCmd(src), "Shop", out)
	require.NoError(t, err)
	assert.Contains(t, stdout, `Exported 1 requests from "Shop" to `)
	data, err := os.ReadFile(filepath.Join(out, "shop", "order.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "body: |-\n  {\n    \"id\": 1\n  }\n")

	dst := newFilesTestStore(t)
	stdout, _, err = runFilesCmd(t, NewImportDirCmd(dst), out)
	require.NoError(t, err)
	assert.Equal(t, "Imported 1 requests and 1 environments into \"Shop\"\n", stdout)

	stdout, _, err = runFilesCmd(t, NewImportDirCmd(dst), filepath.Join(out, "shop"))
	require.NoError(t, err)
	assert.Equal(t, "Imported 1 requests and 1 environments into \"Shop 1\" (duplicated)\n", stdout)

	stdout, _, err = runFilesCmd(t, NewImportDirCmd(dst), out, "--on-duplicate", "skip")
	require.NoError(t, err)
	assert.Equal(t, "Skipped \"Shop\" (1 requests)\n", stdout)

	stdout, _, err = runFilesCmd(t, NewImportDirCmd(dst), out, "--on-duplicate", "merge")
	require.NoError(t, err)
	assert.Equal(t,
		"Imported 0 requests and 1 environments into \"Shop\" (merged), 1 already present\n",
		stdout)

	stdout, _, err = runFilesCmd(t, NewImportDirCmd(dst), out, "--on-duplicate", "replace")
	require.NoError(t, err)
	assert.Contains(t, stdout, "(replaced)")

	stdout, _, err = runFilesCmd(t, NewImportDirCmd(dst), out, "-n", "Renamed")
	require.NoError(t, err)
	assert.Contains(t, stdout, `into "Renamed"`)
}

func TestImportDirWarnsOnConflictingVariables(t *testing.T) {
	ctx := context.Background()
	st := newFilesTestStore(t)
	dir := t.TempDir()
	require.NoError(t,
		os.WriteFile(filepath.Join(dir, "collection.yaml"), []byte("name: S\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "environments"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "environments", "default.yaml"),
		[]byte("name: default\nvariables:\n  k: new\n"), 0o600))
	col := &domain.Collection{Name: "S"}
	require.NoError(t, st.SaveCollection(ctx, col))
	envs, err := st.ListCollectionEnvironments(ctx, col.ID)
	require.NoError(t, err)
	envs[0].SetVars(map[string]string{"k": "old"})
	require.NoError(t, st.SaveEnvironment(ctx, envs[0]))

	_, stderr, err := runFilesCmd(t, NewImportDirCmd(st), dir, "--on-duplicate", "merge")
	require.NoError(t, err)
	assert.Contains(t, stderr, `Warning: S: environment "default" variable "k" already set`)
}

func TestFilesCommandErrors(t *testing.T) {
	st := newFilesTestStore(t)
	_, _, err := runFilesCmd(t, NewExportCmd(st), "Missing", t.TempDir())
	require.ErrorContains(t, err, "not found")

	_, _, err = runFilesCmd(t, NewImportDirCmd(st), t.TempDir(), "--on-duplicate", "nope")
	require.ErrorContains(t, err, "invalid --on-duplicate")

	_, _, err = runFilesCmd(t, NewImportDirCmd(st), t.TempDir())
	require.ErrorContains(t, err, "no collection.yaml")

	root := t.TempDir()
	for _, name := range []string{"a", "b"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, name), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(root, name, "collection.yaml"),
			[]byte("name: "+name+"\n"), 0o600))
	}
	_, _, err = runFilesCmd(t, NewImportDirCmd(st), root, "-n", "X")
	require.ErrorContains(t, err, "needs a single collection")

	require.NoError(t,
		os.WriteFile(filepath.Join(root, "a", "bad.yaml"), []byte("bogus: 1\n"), 0o600))
	_, _, err = runFilesCmd(t, NewImportDirCmd(st), root)
	require.ErrorContains(t, err, "field bogus not found")
}

func TestFilesCommandCompletion(t *testing.T) {
	st := newFilesTestStore(t)
	require.NoError(t, st.SaveCollection(context.Background(), &domain.Collection{Name: "Shop"}))
	export := NewExportCmd(st)
	export.SetContext(context.Background())
	values, _ := export.ValidArgsFunction(export, nil, "")
	assert.NotEmpty(t, values)
	_, directive := export.ValidArgsFunction(export, []string{"Shop"}, "")
	assert.Equal(t, cobra.ShellCompDirectiveFilterDirs, directive)
	importDir := NewImportDirCmd(st)
	_, directive = importDir.ValidArgsFunction(importDir, nil, "")
	assert.Equal(t, cobra.ShellCompDirectiveFilterDirs, directive)
}
