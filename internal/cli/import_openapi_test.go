package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

const openAPISpec = `openapi: 3.0.3
info:
  title: Shop
servers:
  - url: https://shop.example.com
paths:
  /orders/{id}:
    get:
      tags: [orders]
      summary: Get order
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
            example: "o-1"
      responses:
        "200":
          description: ok
  /orders:
    post:
      tags: [orders]
      summary: Create order
      security:
        - basicAuth: []
      requestBody:
        content:
          application/json:
            example:
              item: widget
      responses:
        "201":
          description: created
components:
  securitySchemes:
    basicAuth:
      type: http
      scheme: basic
`

func newOpenAPIStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "quark.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func writeOpenAPISpec(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shop.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func runOpenAPICmd(t *testing.T, st ImportOpenAPIStore, args ...string) (string, string, error) {
	t.Helper()
	cmd := NewImportOpenAPICmd(st)
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	cmd.SetContext(context.Background())
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

func rootCollectionsNamed(t *testing.T, st *store.Store, name string) []*domain.Collection {
	t.Helper()
	cols, err := st.ListCollections(context.Background())
	require.NoError(t, err)
	var out []*domain.Collection
	for _, c := range cols {
		if c.ParentID == "" && c.Name == name {
			out = append(out, c)
		}
	}
	return out
}

func TestImportOpenAPISavesFoldersRequestsAndEnvironment(t *testing.T) {
	ctx := context.Background()
	st := newOpenAPIStore(t)
	spec := writeOpenAPISpec(t, openAPISpec)

	stdout, stderr, err := runOpenAPICmd(t, st, spec)
	require.NoError(t, err)
	assert.Equal(t, "Imported 2 requests (1 folders) into \"Shop\"\n", stdout)
	assert.Empty(t, stderr)

	roots := rootCollectionsNamed(t, st, "Shop")
	require.Len(t, roots, 1)
	children, err := st.ListChildCollections(ctx, roots[0].ID)
	require.NoError(t, err)
	require.Len(t, children, 1)
	assert.Equal(t, "orders", children[0].Name)

	reqs, err := st.ListRequests(ctx, children[0].ID)
	require.NoError(t, err)
	require.Len(t, reqs, 2)
	byName := map[string]*domain.Request{}
	for _, r := range reqs {
		byName[r.Name] = r
	}
	get := byName["Get order"]
	require.NotNil(t, get)
	assert.Equal(t, "{{baseUrl}}/orders/{{id}}", get.URL)
	assert.True(t, get.Enabled)
	post := byName["Create order"]
	require.NotNil(t, post)
	assert.Equal(t, domain.AuthTypeBasic, post.AuthType)
	assert.JSONEq(t, `{"Content-Type":"application/json"}`, post.Headers)
	assert.JSONEq(t, `{"item":"widget"}`, post.Body)

	envs, err := st.ListCollectionEnvironments(ctx, roots[0].ID)
	require.NoError(t, err)
	require.Len(t, envs, 1)
	vars, err := envs[0].DecodeVars()
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"baseUrl":  "https://shop.example.com",
		"id":       "o-1",
		"username": "",
		"password": "",
	}, vars)
}

func TestImportOpenAPIDuplicateActions(t *testing.T) {
	st := newOpenAPIStore(t)
	spec := writeOpenAPISpec(t, openAPISpec)
	_, _, err := runOpenAPICmd(t, st, spec)
	require.NoError(t, err)

	stdout, _, err := runOpenAPICmd(t, st, spec)
	require.NoError(t, err)
	assert.Equal(t, "Imported 2 requests (1 folders) into \"Shop 1\" (duplicated)\n", stdout)

	stdout, _, err = runOpenAPICmd(t, st, spec, "--on-duplicate", "skip")
	require.NoError(t, err)
	assert.Equal(t, "Skipped \"Shop\" (2 requests)\n", stdout)
	assert.Len(t, rootCollectionsNamed(t, st, "Shop"), 1)

	stdout, _, err = runOpenAPICmd(t, st, spec, "--on-duplicate", "replace")
	require.NoError(t, err)
	assert.Equal(t, "Imported 2 requests (1 folders) into \"Shop\" (replaced)\n", stdout)
	assert.Len(t, rootCollectionsNamed(t, st, "Shop"), 1, "the old copy is deleted")
	assert.Len(t, rootCollectionsNamed(t, st, "Shop 1"), 1, "other copies stay")

	_, _, err = runOpenAPICmd(t, st, spec, "--on-duplicate", "merge")
	require.ErrorContains(t, err, "invalid --on-duplicate")
}

const openAPIOAuthSpec = `openapi: 3.0.3
info:
  title: Shop
paths:
  /ping:
    get:
      security:
        - oauth: [read]
      responses:
        "200":
          description: ok
components:
  securitySchemes:
    oauth:
      type: oauth2
      flows: {}
`

func TestImportOpenAPINameOverrideAndWarnings(t *testing.T) {
	st := newOpenAPIStore(t)
	spec := writeOpenAPISpec(t, openAPIOAuthSpec)

	stdout, stderr, err := runOpenAPICmd(t, st, spec, "-n", "Renamed")
	require.NoError(t, err)
	assert.Equal(t, "Imported 1 requests (0 folders) into \"Renamed\"\n", stdout)
	assert.Contains(t, stderr, `Warning: security scheme "oauth" (oauth2) is not supported`)
	assert.Contains(t, stderr, "no servers")
	assert.Len(t, rootCollectionsNamed(t, st, "Renamed"), 1)
}

func TestImportOpenAPIRejectsBadInput(t *testing.T) {
	st := newOpenAPIStore(t)
	_, _, err := runOpenAPICmd(t, st, filepath.Join(t.TempDir(), "missing.yaml"))
	require.ErrorContains(t, err, "import-openapi:")

	bad := writeOpenAPISpec(t, "swagger: '2.0'\npaths: {}\n")
	_, _, err = runOpenAPICmd(t, st, bad)
	require.ErrorContains(t, err, "swagger 2.0 is not supported")
}
