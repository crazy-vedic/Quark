package postman

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMapper_ContentTypeFromBodyLanguage(t *testing.T) {
	json := `{
		"info": {"name": "Test", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
		"item": [{
			"name": "JSON Test",
			"request": {
				"method": "POST",
				"url": "https://example.com/api",
				"body": {
					"mode": "raw",
					"raw": "{\"test\": true}",
					"options": {
						"raw": {"language": "json"}
					}
				}
			}
		}]
	}`

	imp := NewImporter()
	result, err := imp.Parse(strings.NewReader(json))
	assert.NoError(t, err)
	assert.Len(t, result.Requests, 1)
	assert.Contains(t, result.Requests[0].Headers, `"Content-Type"`)
	assert.Contains(t, result.Requests[0].Headers, `application/json`)
}

func TestMapper_URLProtocolExtracted(t *testing.T) {
	json := `{
		"info": {"name": "Test", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
		"item": [{
			"name": "HTTP Test",
			"request": {
				"method": "GET",
				"url": {
					"raw": "http://example.com/api",
					"protocol": "http",
					"host": ["example", "com"],
					"path": ["api"]
				}
			}
		}]
	}`

	imp := NewImporter()
	result, err := imp.Parse(strings.NewReader(json))
	assert.NoError(t, err)
	assert.Len(t, result.Requests, 1)
	assert.Equal(t, "http://example.com/api", result.Requests[0].URL)
}

func TestMapper_URLProtocolFromStructuredURL(t *testing.T) {
	json := `{
		"info": {"name": "Test", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
		"item": [{
			"name": "HTTP Structured",
			"request": {
				"method": "GET",
				"url": {
					"protocol": "http",
					"host": ["example", "com"],
					"path": ["api"]
				}
			}
		}]
	}`

	imp := NewImporter()
	result, err := imp.Parse(strings.NewReader(json))
	assert.NoError(t, err)
	assert.Len(t, result.Requests, 1)
	assert.Equal(t, "http://example.com/api", result.Requests[0].URL)
}

func TestMapper_URLEncodedBody(t *testing.T) {
	jsonBody := `{
		"info": {"name": "Test", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
		"item": [{
			"name": "Token",
			"request": {
				"method": "POST",
				"url": "https://example.com/token",
				"body": {
					"mode": "urlencoded",
					"urlencoded": [
						{"key": "grant_type", "value": "client credentials"},
						{"key": "client_id", "value": "{{client_id}}"},
						{"key": "skip_me", "value": "nope", "disabled": true}
					]
				}
			}
		}]
	}`

	imp := NewImporter()
	result, err := imp.Parse(strings.NewReader(jsonBody))
	assert.NoError(t, err)
	assert.Empty(t, result.Warnings)
	assert.Equal(t, "grant_type=client+credentials&client_id={{client_id}}", result.Requests[0].Body)
	assert.Contains(t, result.Requests[0].Headers, `application/x-www-form-urlencoded`)
}

func TestMapper_FormDataBody(t *testing.T) {
	jsonBody := `{
		"info": {"name": "Test", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
		"item": [{
			"name": "Upload",
			"request": {
				"method": "POST",
				"url": "https://example.com/upload",
				"body": {
					"mode": "formdata",
					"formdata": [
						{"key": "purpose", "value": "extract", "type": "text"},
						{"key": "document", "type": "file", "src": "C:\\\\tmp\\\\doc.pdf"},
						{"key": "skip_me", "value": "nope", "type": "text", "disabled": true}
					]
				}
			}
		}]
	}`

	imp := NewImporter()
	result, err := imp.Parse(strings.NewReader(jsonBody))
	assert.NoError(t, err)
	assert.Empty(t, result.Warnings)
	assert.Contains(t, result.Requests[0].Body, `name="purpose"`)
	assert.Contains(t, result.Requests[0].Body, "extract")
	assert.Contains(t, result.Requests[0].Body, `name="document"; filename="doc.pdf"`)
	assert.NotContains(t, result.Requests[0].Body, "skip_me")

	var headers map[string]string
	assert.NoError(t, json.Unmarshal([]byte(result.Requests[0].Headers), &headers))
	assert.Equal(t, "multipart/form-data; boundary=quark-postman-boundary", headers["Content-Type"])
}
