package postman

import (
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
