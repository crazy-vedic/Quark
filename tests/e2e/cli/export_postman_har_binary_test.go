//go:build e2e

package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

// withStore runs fn with the home database open.
func withStore(t *testing.T, home string, fn func(st *store.Store)) {
	t.Helper()
	st, err := store.New(filepath.Join(home, ".quark", "quark.db"), store.WithCacheSize(100))
	require.NoError(t, err)
	defer st.Close()
	fn(st)
}

// seedCollection saves a collection (a folder when parentID is set) and
// returns its ID.
func seedCollection(t *testing.T, home, name, parentID, description string) string {
	t.Helper()
	id := uuid.New().String()
	withStore(t, home, func(st *store.Store) {
		require.NoError(t, st.SaveCollection(context.Background(), &domain.Collection{
			ID: id, ParentID: parentID, Name: name, Description: description,
		}))
	})
	return id
}

// setDefaultVars replaces the variables of the collection's default environment.
func setDefaultVars(t *testing.T, home, colID string, vars map[string]string) {
	t.Helper()
	withStore(t, home, func(st *store.Store) {
		ctx := context.Background()
		envs, err := st.ListEnvironments(ctx, colID)
		require.NoError(t, err)
		for _, env := range envs {
			if env.Name == "default" {
				env.SetVars(vars)
				require.NoError(t, st.SaveEnvironment(ctx, env))
				return
			}
		}
		require.FailNow(t, "no default environment")
	})
}

// decodeJSON decodes a command's stdout into a generic document.
func decodeJSON(t *testing.T, out string) map[string]any {
	t.Helper()
	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &doc), "stdout: %s", out)
	return doc
}

// postmanItems returns the items of a Postman list, keyed by name.
func postmanItems(t *testing.T, items []any) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		require.True(t, ok)
		out[item["name"].(string)] = item
	}
	return out
}

func TestCLI_ExportPostmanWritesTreeAuthBodiesAndVariables(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	setDefaultVars(t, home, colID, map[string]string{"baseUrl": "https://example.test", "token": "t-1"})
	seedCollection(t, home, "Empty", colID, "")
	usersID := seedCollection(t, home, "Users", colID, "")
	adminID := seedCollection(t, home, "Admin", usersID, "")

	seed := func(parent string, req domain.Request) {
		seedRequest(t, home, parent, &req)
	}
	seed(colID, domain.Request{
		Name: "login", Method: "POST", URL: "https://example.test/login",
		Headers: `{"Content-Type":["application/json; charset=utf-8"],"X-Multi":["a","b"]}`,
		Body:    `{"user":"u"}`, AuthType: "basic", AuthConfig: `{"username":"u","password":"p"}`,
	})
	seed(colID, domain.Request{
		Name: "me", Method: "GET", URL: "https://example.test/me",
		AuthType: "bearer", AuthConfig: `{"token":"tok"}`,
	})
	seed(colID, domain.Request{
		Name: "header-key", Method: "GET", URL: "https://example.test/k",
		AuthType: "api_key", AuthConfig: `{"name":"X-Key","value":"v","in":"header"}`,
	})
	seed(colID, domain.Request{
		Name: "query-key", Method: "GET", URL: "https://example.test/q",
		AuthType: "api_key", AuthConfig: `{"name":"api_key","value":"v","in":"query"}`,
	})
	seed(colID, domain.Request{
		Name: "xml-body", Method: "PUT", URL: "https://example.test/x",
		Headers: `{"Content-Type":["application/xml"]}`, Body: "<a/>",
	})
	seed(colID, domain.Request{
		Name: "html-body", Method: "POST", URL: "https://example.test/h",
		Headers: `{"content-type":["text/html"]}`, Body: "<p/>",
	})
	seed(colID, domain.Request{
		Name: "text-body", Method: "POST", URL: "https://example.test/t",
		Headers: `{"Content-Type":["text/plain"]}`, Body: "hi",
	})
	seed(colID, domain.Request{
		Name: "js-body", Method: "POST", URL: "https://example.test/j",
		Headers: `{"Content-Type":["application/javascript"]}`, Body: "x()",
	})
	seed(colID, domain.Request{
		Name: "soap-body", Method: "POST", URL: "https://example.test/s",
		Headers: `{"Content-Type":["application/soap+xml"]}`, Body: "<e/>",
	})
	seed(colID, domain.Request{
		Name: "csv-body", Method: "POST", URL: "https://example.test/c",
		Headers: `{"Content-Type":["text/csv"]}`, Body: "a,b",
	})
	seed(usersID, domain.Request{Name: "list", Method: "GET", URL: "https://example.test/users"})
	seed(adminID, domain.Request{Name: "ban", Method: "DELETE", URL: "https://example.test/ban"})

	out, errOut, code := runQuarkWithHome(t, home, "export-postman", "API")
	require.Equal(t, 0, code, "stderr: %s", errOut)
	doc := decodeJSON(t, out)

	info := doc["info"].(map[string]any)
	assert.Equal(t, "API", info["name"])
	assert.Equal(t, "https://schema.getpostman.com/json/collection/v2.1.0/collection.json", info["schema"])
	variables := doc["variable"].([]any)
	assert.Len(t, variables, 2)
	assert.Contains(t, out, `"key": "baseUrl"`)

	items := postmanItems(t, doc["item"].([]any))
	assert.NotContains(t, items, "Empty", "folders without requests are left out")
	require.Contains(t, items, "Users")
	users := items["Users"]["item"].([]any)
	userItems := postmanItems(t, users)
	require.Contains(t, userItems, "list")
	require.Contains(t, userItems, "Admin")
	adminItems := postmanItems(t, userItems["Admin"]["item"].([]any))
	require.Contains(t, adminItems, "ban")

	login := items["login"]["request"].(map[string]any)
	assert.Equal(t, "POST", login["method"])
	body := login["body"].(map[string]any)
	assert.Equal(t, "raw", body["mode"])
	assert.Equal(t, "json", body["options"].(map[string]any)["raw"].(map[string]any)["language"])
	basic := login["auth"].(map[string]any)
	assert.Equal(t, "basic", basic["type"])
	headers := login["header"].([]any)
	assert.Len(t, headers, 3, "one entry per header value")

	me := items["me"]["request"].(map[string]any)
	assert.Equal(t, "bearer", me["auth"].(map[string]any)["type"])

	headerKey := items["header-key"]["request"].(map[string]any)["auth"].(map[string]any)
	assert.Equal(t, "apikey", headerKey["type"])
	assert.Contains(t, out, `"value": "X-Key"`)

	assert.Equal(t, "xml", languageOfItem(t, items["xml-body"]))
	assert.Equal(t, "html", languageOfItem(t, items["html-body"]))
	assert.Equal(t, "text", languageOfItem(t, items["text-body"]))
	assert.Equal(t, "javascript", languageOfItem(t, items["js-body"]))
	assert.Equal(t, "xml", languageOfItem(t, items["soap-body"]))
	_, hasOptions := items["csv-body"]["request"].(map[string]any)["body"].(map[string]any)["options"]
	assert.False(t, hasOptions, "unknown content types get no language")
}

// languageOfItem returns the Postman body language of an item's request.
func languageOfItem(t *testing.T, item map[string]any) string {
	t.Helper()
	body := item["request"].(map[string]any)["body"].(map[string]any)
	return body["options"].(map[string]any)["raw"].(map[string]any)["language"].(string)
}

func TestCLI_ExportPostmanIncludesCollectionDescription(t *testing.T) {
	home, _ := setupHomeWithCollection(t)
	docsID := seedCollection(t, home, "Docs", "", "How to call the API")
	seedRequest(t, home, docsID, &domain.Request{Name: "ping", Method: "GET", URL: "https://example.test/ping"})

	out, errOut, code := runQuarkWithHome(t, home, "export-postman", "Docs")
	require.Equal(t, 0, code, "stderr: %s", errOut)
	info := decodeJSON(t, out)["info"].(map[string]any)
	assert.Equal(t, "How to call the API", info["description"])
}

func TestCLI_ExportPostmanReportsUnexportableRequests(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	seedRequest(t, home, colID, &domain.Request{
		Name: "bad-headers", Method: "GET", URL: "https://example.test/a", Headers: "{bad",
	})
	_, errOut, code := runQuarkWithHome(t, home, "export-postman", "API")
	assert.NotEqual(t, 0, code)
	assert.Contains(t, errOut, `export-postman: request "bad-headers": headers:`)

	withStore(t, home, func(st *store.Store) {
		require.NoError(t, st.DeleteRequest(context.Background(), findRequestID(t, st, colID, "bad-headers")))
	})
	seedRequest(t, home, colID, &domain.Request{
		Name: "bad-auth", Method: "GET", URL: "https://example.test/b",
		AuthType: "bearer", AuthConfig: "{bad",
	})
	_, errOut, code = runQuarkWithHome(t, home, "export-postman", "API")
	assert.NotEqual(t, 0, code)
	assert.Contains(t, errOut, `request "bad-auth": auth config`)

	withStore(t, home, func(st *store.Store) {
		require.NoError(t, st.DeleteRequest(context.Background(), findRequestID(t, st, colID, "bad-auth")))
	})
	seedRequest(t, home, colID, &domain.Request{
		Name: "digest", Method: "GET", URL: "https://example.test/c", AuthType: "digest", AuthConfig: "{}",
	})
	_, errOut, code = runQuarkWithHome(t, home, "export-postman", "API")
	assert.NotEqual(t, 0, code)
	assert.Contains(t, errOut, `auth type "digest" has no Postman equivalent`)

	_, errOut, code = runQuarkWithHome(t, home, "export-postman", "NoSuchCollection")
	assert.NotEqual(t, 0, code)
	assert.Contains(t, errOut, "export-postman:")
}

// findRequestID returns the ID of the request named name in collection colID.
func findRequestID(t *testing.T, st *store.Store, colID, name string) string {
	t.Helper()
	reqs, err := st.ListRequests(context.Background(), colID)
	require.NoError(t, err)
	for _, r := range reqs {
		if r.Name == name {
			return r.ID
		}
	}
	require.FailNow(t, "no request named %q", name)
	return ""
}

// recordingServer answers /api with a JSON body and echoes the URL it saw.
func recordingServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/create" && r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Reply", "ok")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"created":true}`))
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("X-Seen-Auth", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte("seen"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCLI_ExportHARRecordsSentRequestsWithCredentials(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	srv := recordingServer(t)

	seedRequest(t, home, colID, &domain.Request{
		Name: "create", Method: "POST", URL: srv.URL + "/create?page=2&q=a%20b",
		Headers:  `{"Content-Type":["application/json"]}`,
		Body:     `{"name":"x"}`,
		AuthType: "api_key", AuthConfig: `{"name":"api_key","value":"secret","in":"query"}`,
	})
	seedRequest(t, home, colID, &domain.Request{
		Name: "secure", Method: "GET", URL: srv.URL + "/secure",
		AuthType: "bearer", AuthConfig: `{"token":"tok"}`,
	})
	seedRequest(t, home, colID, &domain.Request{
		Name: "basic", Method: "GET", URL: srv.URL + "/basic",
		AuthType: "basic", AuthConfig: `{"username":"user","password":"pass"}`,
	})
	seedRequest(t, home, colID, &domain.Request{
		Name: "header-key", Method: "GET", URL: srv.URL + "/hk",
		AuthType: "api_key", AuthConfig: `{"name":"X-Key","value":"v","in":"header"}`,
	})

	for _, name := range []string{"create", "secure", "basic", "header-key"} {
		_, errOut, code := runQuarkWithHome(t, home, "run", "API/"+name)
		require.Equal(t, 0, code, "run %s: %s", name, errOut)
	}

	out, _, code := runQuarkWithHome(t, home, "export-har", "API/create")
	require.Equal(t, 0, code)
	doc := decodeJSON(t, out)
	log := doc["log"].(map[string]any)
	assert.Equal(t, "1.2", log["version"])
	assert.Equal(t, "Quark", log["creator"].(map[string]any)["name"])
	entries := log["entries"].([]any)
	require.Len(t, entries, 1)
	entry := entries[0].(map[string]any)
	request := entry["request"].(map[string]any)
	assert.Equal(t, "POST", request["method"])
	assert.Equal(t, `{"name":"x"}`, request["postData"].(map[string]any)["text"])
	assert.Equal(t, "application/json", request["postData"].(map[string]any)["mimeType"])
	assert.Contains(t, out, `"name": "api_key"`, "the query credential is listed")
	assert.Contains(t, out, `"value": "secret"`)
	assert.Contains(t, out, `"value": "a b"`, "query values are unescaped")
	response := entry["response"].(map[string]any)
	assert.Equal(t, float64(201), response["status"])
	assert.Equal(t, "Created", response["statusText"])
	assert.Contains(t, out, `"name": "X-Reply"`)
	assert.Equal(t, `{"created":true}`, response["content"].(map[string]any)["text"])

	out, _, code = runQuarkWithHome(t, home, "export-har", "API/secure")
	require.Equal(t, 0, code)
	assert.Contains(t, out, `"value": "Bearer tok"`)

	out, _, code = runQuarkWithHome(t, home, "export-har", "API/basic")
	require.Equal(t, 0, code)
	assert.Contains(t, out, `"value": "Basic dXNlcjpwYXNz"`, "basic credentials are base64 encoded")

	out, _, code = runQuarkWithHome(t, home, "export-har", "API/header-key")
	require.Equal(t, 0, code)
	assert.Contains(t, out, `"name": "X-Key"`)
}

func TestCLI_ExportHARRecordsFailedSendsAndEmptyHistory(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	seedRequest(t, home, colID, &domain.Request{
		Name: "down", Method: "GET", URL: "http://127.0.0.1:1/unreachable",
	})
	seedRequest(t, home, colID, &domain.Request{
		Name: "never", Method: "GET", URL: "https://example.test/never",
	})

	_, _, code := runQuarkWithHome(t, home, "run", "API/down")
	assert.NotEqual(t, 0, code, "a connection failure is a run error")

	out, _, code := runQuarkWithHome(t, home, "export-har", "API/down")
	require.Equal(t, 0, code)
	entries := decodeJSON(t, out)["log"].(map[string]any)["entries"].([]any)
	require.Len(t, entries, 1)
	entry := entries[0].(map[string]any)
	assert.NotEmpty(t, entry["comment"], "the failure is recorded as the entry comment")
	assert.Equal(t, float64(0), entry["response"].(map[string]any)["status"])
	assert.Equal(t, "", entry["response"].(map[string]any)["statusText"])

	out, errOut, code := runQuarkWithHome(t, home, "export-har", "API/never")
	require.Equal(t, 0, code, "stderr: %s", errOut)
	assert.Contains(t, errOut, `No execution history for "API/never"`)
	entries = decodeJSON(t, out)["log"].(map[string]any)["entries"].([]any)
	assert.Empty(t, entries)

	_, errOut, code = runQuarkWithHome(t, home, "export-har", "API/NoSuchRequest")
	assert.NotEqual(t, 0, code)
	assert.Contains(t, errOut, "export-har: resolve request")
}
