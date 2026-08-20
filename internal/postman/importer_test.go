package postman_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	assert.Equal(t, postman.Safe, result.Security)
}
