package postman

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

func exportFixture() (*ExportFolder, map[string]string) {
	root := &ExportFolder{
		Name: "API",
		Requests: []*domain.Request{
			{
				Name: "Create item", Method: "POST", URL: "{{baseUrl}}/items",
				Headers:  `{"Content-Type":"application/json","X-Trace":"t-1"}`,
				Body:     "{\n  \"name\": \"widget\"\n}",
				AuthType: domain.AuthTypeBearer,
				AuthConfig: domain.MustAuthConfigJSON(domain.AuthConfig{
					Token: "{{token}}",
				}),
			},
			{Name: "Ping", Method: "GET", URL: "{{baseUrl}}/ping"},
			{
				Name: "Login", Method: "GET", URL: "{{baseUrl}}/login",
				AuthType: domain.AuthTypeBasic,
				AuthConfig: domain.MustAuthConfigJSON(domain.AuthConfig{
					Username: "u", Password: "p",
				}),
			},
			{
				Name: "List items", Method: "GET", URL: "{{baseUrl}}/items?limit=10",
				Headers:  `{"Accept":"*/*"}`,
				AuthType: domain.AuthTypeAPIKey,
				AuthConfig: domain.MustAuthConfigJSON(domain.AuthConfig{
					Name: "X-Key", Value: "secret", In: domain.AuthAPIKeyInHeader,
				}),
			},
		},
		Folders: []*ExportFolder{
			{
				Name: "Users",
				Requests: []*domain.Request{
					{Name: "Get user", Method: "GET", URL: "{{baseUrl}}/users/1"},
				},
			},
			// A folder without requests has nothing to export.
			{Name: "Empty"},
		},
	}
	vars := map[string]string{"baseUrl": "https://api.example.com", "token": "abc"}
	return root, vars
}

// TestExportCollectionReimports exports a tree and imports the file again
// with the importer, then compares what the importer produces with the
// original requests. The importer turns auth into Authorization headers, so
// the expected headers include them.
func TestExportCollectionReimports(t *testing.T) {
	root, vars := exportFixture()
	data, err := ExportCollection("API", "Main API", root, vars)
	require.NoError(t, err)

	col, err := Parse(bytes.NewReader(data))
	require.NoError(t, err)
	assert.Equal(t, "API", col.Info.Name)
	assert.Equal(t, "Main API", col.Info.Description)
	assert.Equal(t, SchemaV21, col.Info.Schema)

	res := ImportCollection(col)
	assert.Empty(t, res.Warnings)
	assert.Equal(t, vars, res.CollectionVariables)

	want := map[string]struct {
		method, url, body string
		headers           map[string]string
	}{
		"Create item": {
			method: "POST", url: "{{baseUrl}}/items", body: "{\n  \"name\": \"widget\"\n}",
			headers: map[string]string{
				"Content-Type":  "application/json",
				"X-Trace":       "t-1",
				"Authorization": "Bearer {{token}}",
			},
		},
		"Ping": {method: "GET", url: "{{baseUrl}}/ping", headers: map[string]string{}},
		"Login": {
			method: "GET", url: "{{baseUrl}}/login",
			headers: map[string]string{"Authorization": "Basic dTpw"},
		},
		"List items": {
			method: "GET", url: "{{baseUrl}}/items?limit=10",
			headers: map[string]string{"Accept": "*/*", "X-Key": "secret"},
		},
		"Get user": {
			method: "GET", url: "{{baseUrl}}/users/1", headers: map[string]string{},
		},
	}
	require.Len(t, res.Requests, len(want))
	for _, got := range res.Requests {
		exp, ok := want[got.Name]
		require.True(t, ok, "unexpected request %q", got.Name)
		assert.Equal(t, exp.method, got.Method, got.Name)
		assert.Equal(t, exp.url, got.URL, got.Name)
		assert.Equal(t, exp.body, got.Body, got.Name)
		headers := map[string]string{}
		require.NoError(t, json.Unmarshal([]byte(got.Headers), &headers), got.Name)
		assert.Equal(t, exp.headers, headers, got.Name)
	}
	assert.NotContains(t, string(data), `"Empty"`)
	require.Len(t, res.Groups, 2, "the root and Users folders")
	assert.Equal(t, "Users", res.Groups[1].Path)
}

func TestExportCollectionIsDeterministic(t *testing.T) {
	root, vars := exportFixture()
	first, err := ExportCollection("API", "", root, vars)
	require.NoError(t, err)
	for range 5 {
		again, err := ExportCollection("API", "", root, vars)
		require.NoError(t, err)
		assert.Equal(t, first, again)
	}
}

func TestExportCollectionShape(t *testing.T) {
	root, vars := exportFixture()
	data, err := ExportCollection("API", "", root, vars)
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(data, &doc))
	info := objectOf(t, doc["info"])
	assert.Equal(t, SchemaV21, info["schema"])
	assert.NotContains(t, info, "description", "empty description is omitted")

	items, ok := doc["item"].([]any)
	require.True(t, ok, "item is an array")
	require.Len(t, items, 5)
	create := objectOf(t, objectOf(t, items[0])["request"])
	body := objectOf(t, create["body"])
	assert.Equal(t, "raw", body["mode"])
	options := objectOf(t, body["options"])
	assert.Equal(t, "json", objectOf(t, objectOf(t, options["raw"]))["language"])
	assert.Equal(t, "bearer", objectOf(t, create["auth"])["type"])

	ping := objectOf(t, objectOf(t, items[1])["request"])
	assert.NotContains(t, ping, "body", "requests without a body have no body object")
	assert.NotContains(t, ping, "auth", "requests without auth have no auth object")

	variables, ok := doc["variable"].([]any)
	require.True(t, ok, "variable is an array")
	require.Len(t, variables, 2)
	assert.Equal(t, "baseUrl", objectOf(t, variables[0])["key"], "variables are sorted")
}

// objectOf returns v as a JSON object, failing the test if it is not one.
func objectOf(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	require.True(t, ok, "expected a JSON object, got %T", v)
	return m
}

func TestExportCollectionRejectsUnmappableAuth(t *testing.T) {
	root := &ExportFolder{Name: "API", Requests: []*domain.Request{{
		Name: "Digest", Method: "GET", URL: "http://x", AuthType: "digest",
		AuthConfig: "{}",
	}}}
	_, err := ExportCollection("API", "", root, nil)
	require.ErrorContains(t, err, `auth type "digest" has no Postman equivalent`)
}

func TestExportCollectionRejectsBadHeaders(t *testing.T) {
	root := &ExportFolder{Name: "API", Requests: []*domain.Request{{
		Name: "Bad", Method: "GET", URL: "http://x", Headers: "not json",
	}}}
	_, err := ExportCollection("API", "", root, nil)
	require.ErrorContains(t, err, `request "Bad": headers`)
}
