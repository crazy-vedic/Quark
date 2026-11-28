package postman_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/postman"
)

func TestImporter_MinimalCollection(t *testing.T) {
	json := `{
		"info": {"name": "Test Collection", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
		"item": [
			{
				"name": "Get User",
				"request": {
					"method": "GET",
					"url": {"raw": "https://api.example.com/users/1"}
				}
			}
		]
	}`

	im := postman.NewImporter()
	result, err := im.Parse(strings.NewReader(json))
	require.NoError(t, err)
	assert.Equal(t, "Test Collection", result.CollectionName)
	assert.Len(t, result.Requests, 1)
	assert.Equal(t, "Get User", result.Requests[0].Name)
	assert.Equal(t, "GET", result.Requests[0].Method)
	assert.Equal(t, "https://api.example.com/users/1", result.Requests[0].URL)
	assert.Equal(t, postman.Safe, result.Security)
}

// An "@" in a query value (an e-mail address) is not a credential. Only
// userinfo in the authority part (https://user:pass@host) is.
func TestImporter_SecurityScoreOnlyFlagsURLUserinfo(t *testing.T) {
	score := func(rawURL string) postman.SecurityLevel {
		collection := `{
			"info": {"name": "T", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
			"item": [{"name": "R", "request": {"method": "GET", "url": "` + rawURL + `"}}]
		}`
		result, err := postman.NewImporter().Parse(strings.NewReader(collection))
		require.NoError(t, err)
		return result.Security
	}

	assert.Equal(t, postman.Safe, score("https://example.com/users?email=a@b.com"))
	assert.Equal(t, postman.Safe, score("{{base_url}}/users/a@b.com?at=10:30"))
	assert.Equal(t, postman.Dangerous, score("https://user:pass@example.com/users"))
	assert.Equal(t, postman.Dangerous, score("user:pass@example.com/users"))
}

func TestImporter_NestedFolders(t *testing.T) {
	json := `{
		"info": {"name": "API", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
		"item": [
			{
				"name": "Users",
				"item": [
					{
						"name": "Create",
						"request": {
							"method": "POST",
							"url": {"raw": "https://api.example.com/users"},
							"body": {"mode": "raw", "raw": "{\"name\":\"test\"}"}
						}
					}
				]
			}
		]
	}`

	im := postman.NewImporter()
	result, err := im.Parse(strings.NewReader(json))
	require.NoError(t, err)
	assert.Equal(t, "API", result.CollectionName)
	assert.Len(t, result.Requests, 1)
	assert.Equal(t, "Create", result.Requests[0].Name)
	require.Len(t, result.Groups, 1)
	assert.Equal(t, "Users", result.Groups[0].Path)
	assert.Equal(t, "Create", result.Groups[0].Requests[0].Name)
	assert.Equal(t, "POST", result.Requests[0].Method)
	assert.Equal(t, "{\"name\":\"test\"}", result.Requests[0].Body)
}

func TestImporter_HeadersAndAuth(t *testing.T) {
	json := `{
		"info": {"name": "Auth Test", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
		"item": [
			{
				"name": "Get Data",
				"request": {
					"method": "GET",
					"url": {"raw": "https://api.example.com/data"},
					"header": [
						{"key": "Authorization", "value": "Bearer tok123"},
						{"key": "Content-Type", "value": "application/json"}
					]
				}
			}
		]
	}`

	im := postman.NewImporter()
	result, err := im.Parse(strings.NewReader(json))
	require.NoError(t, err)
	assert.Len(t, result.Requests, 1)
	assert.Contains(t, result.Requests[0].Headers, "Authorization")
	assert.Contains(t, result.Requests[0].Headers, "Bearer")
	assert.Contains(t, result.Requests[0].Headers, "Content-Type")
	assert.Equal(t, postman.Dangerous, result.Security)
}

func TestImporter_InvalidJSON(t *testing.T) {
	im := postman.NewImporter()
	_, err := im.Parse(strings.NewReader("not json"))
	assert.Error(t, err)
}

func TestImporter_EmptyCollection(t *testing.T) {
	json := `{
		"info": {"name": "Empty", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
		"item": []
	}`
	im := postman.NewImporter()
	result, err := im.Parse(strings.NewReader(json))
	require.NoError(t, err)
	assert.Empty(t, result.Requests)
}

func TestImporter_VariablesInURL(t *testing.T) {
	json := `{
		"info": {"name": "Vars", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
		"variable": [{"key": "base_url", "value": "https://api.example.com"}],
		"item": [
			{
				"name": "Get",
				"request": {
					"method": "GET",
					"url": {"raw": "{{base_url}}/users"}
				}
			}
		]
	}`

	im := postman.NewImporter()
	result, err := im.Parse(strings.NewReader(json))
	require.NoError(t, err)
	assert.Equal(t, "{{base_url}}/users", result.Requests[0].URL)
	assert.Equal(
		t,
		map[string]string{"base_url": "https://api.example.com"},
		result.CollectionVariables,
	)
	assert.Equal(t, postman.Safe, result.Security)
}

func TestImporter_CollectionVariablesScalarsDisabledEmptyAndDuplicates(t *testing.T) {
	json := `{
		"info": {"name": "Vars", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
		"variable": [
			{"key":"string","value":"value"},
			{"key":"number","value":42},
			{"key":"boolean","value":true},
			{"key":"null","value":null},
			{"key":"disabled","value":"secret","disabled":true},
			{"key":"","value":"ignored"},
			{"key":"string","value":"later"}
		],
		"item": []
	}`
	result, err := postman.NewImporter().Parse(strings.NewReader(json))
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"string": "value", "number": "42", "boolean": "true", "null": "",
	}, result.CollectionVariables)
	assert.NotContains(t, result.CollectionVariables, "disabled")
	assert.Contains(t, result.Warnings, "collection variable with empty key skipped")
	assert.Contains(t, result.Warnings, `duplicate collection variable "string" ignored`)
}

func TestImporter_CollectionVariableRejectsNonScalarValue(t *testing.T) {
	json := `{"info":{"name":"Vars","schema":"v2.1"},"variable":[{"key":"bad","value":{"nested":true}}],"item":[]}`
	_, err := postman.NewImporter().Parse(strings.NewReader(json))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected a JSON scalar")
	assert.NotContains(t, err.Error(), "nested")
}

func TestImporter_CollectionAndFolderAuthBecomeDefaults(t *testing.T) {
	collection := `{
		"info": {"name": "API", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
		"auth": {"type": "bearer", "bearer": [{"key": "token", "value": "root-token", "type": "string"}]},
		"item": [
			{
				"name": "Admin",
				"auth": {"type": "basic", "basic": [
					{"key": "username", "value": "admin", "type": "string"},
					{"key": "password", "value": "pw", "type": "string"}
				]},
				"item": [
					{"name": "Inherits", "request": {"method": "GET", "url": {"raw": "https://api.example.com/a"}}},
					{"name": "No auth", "request": {"method": "GET", "url": {"raw": "https://api.example.com/b"}, "auth": {"type": "noauth"}}}
				]
			},
			{
				"name": "Plain",
				"item": [
					{"name": "Oauth", "request": {"method": "GET", "url": {"raw": "https://api.example.com/c"}, "auth": {"type": "oauth2"}}}
				]
			}
		]
	}`
	result, err := postman.NewImporter().Parse(strings.NewReader(collection))
	require.NoError(t, err)

	assert.Equal(t, "bearer", result.AuthType)
	assert.JSONEq(t, `{"token":"root-token"}`, result.AuthConfig)

	groups := make(map[string]postman.RequestGroup)
	for _, g := range result.Groups {
		groups[g.Path] = g
	}
	assert.Equal(t, "basic", groups["Admin"].AuthType)
	assert.JSONEq(t, `{"username":"admin","password":"pw"}`, groups["Admin"].AuthConfig)
	assert.Empty(t, groups["Plain"].AuthType, "a folder without auth inherits")

	byName := make(map[string]*domain.Request)
	for _, r := range result.Requests {
		byName[r.Name] = r
	}
	assert.Equal(t, domain.AuthTypeInherit, byName["Inherits"].AuthType,
		"a request without auth inherits from its folder and collection")
	assert.Empty(t, byName["No auth"].AuthType, "noauth is an explicit none")
	assert.Empty(t, byName["Oauth"].AuthType,
		"an unsupported request auth type maps to headers or nothing, as before")
}

func TestImporter_UnsupportedCollectionAuthWarns(t *testing.T) {
	collection := `{
		"info": {"name": "API", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
		"auth": {"type": "oauth2", "oauth2": []},
		"item": []
	}`
	result, err := postman.NewImporter().Parse(strings.NewReader(collection))
	require.NoError(t, err)
	assert.Empty(t, result.AuthType)
	assert.Contains(t, strings.Join(result.Warnings, "\n"),
		`collection auth type "oauth2" is not supported`)
}
