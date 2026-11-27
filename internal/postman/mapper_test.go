package postman

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
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

const disabledHeaderCollection = `{
	"info": {"name": "Test", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
	"item": [{
		"name": "Headers",
		"request": {
			"method": "GET",
			"url": "https://example.com/api",
			"header": [
				{"key": "X-Active", "value": "yes"},
				{"key": "X-Debug-Token", "value": "old-secret", "disabled": true},
				{"key": "X-Off-And-Empty", "value": "", "disabled": true}
			]
		}
	}]
}`

// A header the user switched off in Postman ("disabled": true) is not imported
// by default, and a warning names it without printing its value.
func TestMapper_SkipsDisabledHeaders(t *testing.T) {
	result, err := NewImporter().Parse(strings.NewReader(disabledHeaderCollection))
	assert.NoError(t, err)
	assert.Len(t, result.Requests, 1)
	assert.Contains(t, result.Requests[0].Headers, "X-Active")
	assert.NotContains(t, result.Requests[0].Headers, "X-Debug-Token")
	assert.NotContains(t, result.Requests[0].Headers, "old-secret")

	assert.Equal(t, []string{
		`request "Headers": header "X-Debug-Token" is disabled in Postman and was not imported ` +
			`(use --include-disabled to import it as an active header)`,
	}, result.Warnings)
}

// WithDisabledHeaders imports them as active and warns that it did.
func TestMapper_WithDisabledHeadersImportsThemAndWarns(t *testing.T) {
	im := NewImporter(WithDisabledHeaders())
	result, err := im.Parse(strings.NewReader(disabledHeaderCollection))
	assert.NoError(t, err)
	assert.Len(t, result.Requests, 1)
	assert.Contains(t, result.Requests[0].Headers, `"X-Debug-Token":"old-secret"`)

	assert.Equal(t, []string{
		`request "Headers": header "X-Debug-Token" is disabled in Postman but was imported as an ` +
			`active header because --include-disabled was given`,
	}, result.Warnings)
}

// With no disabled headers there is nothing to warn about, with or without the option.
func TestMapper_NoDisabledHeadersNoWarning(t *testing.T) {
	collection := `{
		"info": {"name": "Test", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
		"item": [{"name": "H", "request": {"method": "GET", "url": "https://example.com",
			"header": [{"key": "X-A", "value": "1"}, {"key": "X-B", "value": "2", "disabled": false}]}}]
	}`
	for _, im := range []*Importer{NewImporter(), NewImporter(WithDisabledHeaders())} {
		result, err := im.Parse(strings.NewReader(collection))
		assert.NoError(t, err)
		assert.Empty(t, result.Warnings)
		assert.Contains(t, result.Requests[0].Headers, "X-B")
	}
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
	assert.Equal(
		t,
		"grant_type=client+credentials&client_id={{client_id}}",
		result.Requests[0].Body,
	)
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

func TestMapper_BodyTypes(t *testing.T) {
	jsonBody := `{
		"info": {"name": "Test", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
		"item": [
			{"name": "Form", "request": {"method": "POST", "url": "https://example.com/a",
				"body": {"mode": "urlencoded", "urlencoded": [{"key": "a", "value": "1"}]}}},
			{"name": "Raw", "request": {"method": "POST", "url": "https://example.com/b",
				"body": {"mode": "raw", "raw": "{\"x\":1}", "options": {"raw": {"language": "json"}}}}},
			{"name": "Empty", "request": {"method": "GET", "url": "https://example.com/c"}}
		]
	}`
	result, err := NewImporter().Parse(strings.NewReader(jsonBody))
	require.NoError(t, err)
	require.Len(t, result.Requests, 3)
	assert.Equal(t, domain.BodyTypeFormURLEncoded, result.Requests[0].BodyType)
	assert.Equal(t, "a=1", result.Requests[0].Body)
	assert.Equal(t, domain.BodyTypeRaw, result.Requests[1].BodyType)
	assert.Equal(t, domain.BodyTypeRaw, result.Requests[2].BodyType)
}
