package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/postman"
	"github.com/crazy-vedic/quark/internal/store"
)

func newExportTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "quark.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// seedExportCollection saves API with one request, a Users folder holding
// another, and a default environment with baseUrl.
func seedExportCollection(t *testing.T, st *store.Store) *domain.Collection {
	t.Helper()
	ctx := context.Background()
	api := &domain.Collection{Name: "API", Description: "Main API"}
	require.NoError(t, st.SaveCollection(ctx, api))
	users := &domain.Collection{Name: "Users", ParentID: api.ID}
	require.NoError(t, st.SaveCollection(ctx, users))
	require.NoError(t, st.SaveRequest(ctx, &domain.Request{
		CollectionID: api.ID, Name: "Create item", Method: "POST",
		URL:      "{{baseUrl}}/items",
		Headers:  `{"Content-Type":"application/json"}`,
		Body:     `{"name":"widget"}`,
		AuthType: domain.AuthTypeBearer,
		AuthConfig: domain.MustAuthConfigJSON(domain.AuthConfig{
			Token: "{{token}}",
		}),
		Enabled: true,
	}))
	require.NoError(t, st.SaveRequest(ctx, &domain.Request{
		CollectionID: users.ID, Name: "Get user", Method: "GET",
		URL: "{{baseUrl}}/users/1", Enabled: true,
	}))
	envs, err := st.ListCollectionEnvironments(ctx, api.ID)
	require.NoError(t, err)
	require.Len(t, envs, 1)
	envs[0].SetVars(map[string]string{"baseUrl": "https://api.example.com"})
	require.NoError(t, st.SaveEnvironment(ctx, envs[0]))
	return api
}

func TestExportPostmanReimportsWithImporter(t *testing.T) {
	st := newExportTestStore(t)
	seedExportCollection(t, st)

	cmd := NewExportPostmanCmd(st)
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"API"})
	cmd.SetContext(context.Background())
	require.NoError(t, cmd.Execute())
	assert.Empty(t, errOut.String())

	col, err := postman.Parse(bytes.NewReader(out.Bytes()))
	require.NoError(t, err)
	assert.Equal(t, "API", col.Info.Name)
	assert.Equal(t, "Main API", col.Info.Description)

	res := postman.ImportCollection(col)
	assert.Empty(t, res.Warnings)
	assert.Equal(t, map[string]string{"baseUrl": "https://api.example.com"},
		res.CollectionVariables)
	require.Len(t, res.Requests, 2)

	create := res.Requests[0]
	assert.Equal(t, "Create item", create.Name)
	assert.Equal(t, "POST", create.Method)
	assert.Equal(t, "{{baseUrl}}/items", create.URL)
	assert.Equal(t, `{"name":"widget"}`, create.Body)
	assert.JSONEq(t,
		`{"Content-Type":"application/json","Authorization":"Bearer {{token}}"}`,
		create.Headers)

	assert.Equal(t, "Get user", res.Requests[1].Name)
	require.Len(t, res.Groups, 2)
	assert.Equal(t, "Users", res.Groups[1].Path)
}

func TestExportPostmanIsDeterministic(t *testing.T) {
	st := newExportTestStore(t)
	seedExportCollection(t, st)
	run := func() []byte {
		cmd := NewExportPostmanCmd(st)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetArgs([]string{"API"})
		cmd.SetContext(context.Background())
		require.NoError(t, cmd.Execute())
		return out.Bytes()
	}
	first := run()
	assert.Equal(t, first, run())
}

func TestExportPostmanUnknownCollection(t *testing.T) {
	st := newExportTestStore(t)
	cmd := NewExportPostmanCmd(st)
	cmd.SetArgs([]string{"Missing"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetContext(context.Background())
	err := cmd.Execute()
	require.ErrorContains(t, err, "export-postman:")
}

// runExportCmd runs a command with args and returns its stdout.
func runExportCmd(t *testing.T, cmd *cobra.Command, args ...string) []byte {
	t.Helper()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs(args)
	cmd.SetContext(context.Background())
	require.NoError(t, cmd.Execute())
	return out.Bytes()
}
