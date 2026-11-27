package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

const petShopYAML = `openapi: 3.0.3
info:
  title: Pet Shop
  description: Demo API
servers:
  - url: "{scheme}://{host}/v1/"
    variables:
      scheme:
        default: https
      host:
        default: api.example.com
security:
  - bearerAuth: []
paths:
  /pets/{petId}:
    parameters:
      - name: X-Trace
        in: header
        required: true
        example: abc
    get:
      tags: [pets]
      summary: Get a pet
      operationId: getPet
      parameters:
        - name: petId
          in: path
          required: true
          schema:
            type: integer
            example: 42
        - name: verbose
          in: query
          required: false
          schema:
            type: boolean
        - name: fields
          in: query
          required: true
          schema:
            type: string
            example: id,name
      responses:
        "200":
          description: ok
  /pets:
    post:
      tags: [pets]
      operationId: createPet
      security: []
      requestBody:
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/Pet'
      responses:
        "201":
          description: created
    get:
      operationId: listAll
      security:
        - apiKeyAuth: []
      responses:
        "200":
          description: ok
  /health:
    get:
      summary: Health
      security:
        - oauth: [read]
      responses:
        "200":
          description: ok
  /upload:
    post:
      tags: [files]
      requestBody:
        content:
          multipart/form-data:
            schema:
              type: object
      responses:
        "200":
          description: ok
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
    apiKeyAuth:
      type: apiKey
      in: header
      name: X-API-Key
    oauth:
      type: oauth2
      flows: {}
  schemas:
    Pet:
      type: object
      properties:
        name:
          type: string
        age:
          type: integer
          example: 3
        tags:
          type: array
          items:
            type: string
        owner:
          $ref: '#/components/schemas/Owner'
    Owner:
      type: object
      properties:
        email:
          type: string
          example: a@example.com
`

func convert(t *testing.T, doc string) *Result {
	t.Helper()
	res, err := Convert([]byte(doc))
	require.NoError(t, err)
	return res
}

func findRequest(t *testing.T, c *Collection, name string) *domain.Request {
	t.Helper()
	for _, r := range c.Requests {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("request %q not found in %q", name, c.Name)
	return nil
}

func findFolder(t *testing.T, c *Collection, name string) *Collection {
	t.Helper()
	for _, child := range c.Children {
		if child.Name == name {
			return child
		}
	}
	t.Fatalf("folder %q not found in %q", name, c.Name)
	return nil
}

func TestConvertBuildsFoldersRequestsAndEnvironment(t *testing.T) {
	res := convert(t, petShopYAML)
	root := res.Collection

	assert.Equal(t, "Pet Shop", root.Name)
	assert.Equal(t, "Demo API", root.Description)
	assert.Equal(t, 5, root.CountRequests())
	assert.Equal(t, map[string]string{
		"baseUrl": "https://api.example.com/v1",
		"petId":   "42",
		"token":   "",
		"apiKey":  "",
	}, root.Vars)

	// Folders are sorted by tag; untagged operations stay in the root.
	require.Len(t, root.Children, 2)
	assert.Equal(t, "files", root.Children[0].Name)
	assert.Equal(t, "pets", root.Children[1].Name)
	assert.Equal(t, []string{"Health", "listAll"}, requestNames(root.Requests))
	assert.Equal(t, []string{"createPet", "Get a pet"}, requestNames(root.Children[1].Requests))
}

func requestNames(reqs []*domain.Request) []string {
	names := make([]string, 0, len(reqs))
	for _, r := range reqs {
		names = append(names, r.Name)
	}
	return names
}

func TestConvertMapsPathQueryAndHeaderParameters(t *testing.T) {
	pets := findFolder(t, convert(t, petShopYAML).Collection, "pets")
	req := findRequest(t, pets, "Get a pet")

	assert.Equal(t, "GET", req.Method)
	assert.Equal(t, "{{baseUrl}}/pets/{{petId}}?fields=id%2Cname", req.URL)
	assert.JSONEq(t, `{"X-Trace":"abc"}`, req.Headers)
	assert.True(t, req.Enabled)
}

func TestConvertBuildsJSONBodyFromSchema(t *testing.T) {
	pets := findFolder(t, convert(t, petShopYAML).Collection, "pets")
	req := findRequest(t, pets, "createPet")

	assert.Equal(t, "POST", req.Method)
	assert.Equal(t, "{{baseUrl}}/pets", req.URL)
	assert.JSONEq(t, `{"Content-Type":"application/json"}`, req.Headers)
	assert.Equal(t, `{
  "age": 3,
  "name": "",
  "owner": {
    "email": "a@example.com"
  },
  "tags": [
    ""
  ]
}`, req.Body)
}

func TestConvertMapsAuthSchemes(t *testing.T) {
	res := convert(t, petShopYAML)
	pets := findFolder(t, res.Collection, "pets")

	// Global bearer security applies to the operation that declares none.
	get := findRequest(t, pets, "Get a pet")
	assert.Equal(t, domain.AuthTypeBearer, get.AuthType)
	assert.JSONEq(t, `{"token":"{{token}}"}`, get.AuthConfig)

	// An explicit empty security list means anonymous.
	create := findRequest(t, pets, "createPet")
	assert.Equal(t, domain.AuthTypeNone, create.AuthType)
	assert.Empty(t, create.AuthConfig)

	list := findRequest(t, res.Collection, "listAll")
	assert.Equal(t, domain.AuthTypeAPIKey, list.AuthType)
	assert.JSONEq(t, `{"name":"X-API-Key","value":"{{apiKey}}","in":"header"}`, list.AuthConfig)
}

func TestConvertWarnsAboutUnsupportedConstructs(t *testing.T) {
	res := convert(t, petShopYAML)

	assert.Contains(t, res.Warnings,
		`security scheme "oauth" (oauth2) is not supported; `+
			`requests that use it are sent without auth`)
	assert.Contains(t, res.Warnings,
		"POST /upload: multipart/form-data request bodies are not generated; fill in the body")
	health := findRequest(t, res.Collection, "Health")
	assert.Equal(t, domain.AuthTypeNone, health.AuthType)
}

func TestConvertJSONWithoutServersWarnsAndDuplicateNames(t *testing.T) {
	doc := `{
	  "openapi": "3.1.0",
	  "info": {"title": "JSON API"},
	  "paths": {
	    "/a": {"get": {"summary": "Same", "responses": {}}},
	    "/b": {"get": {"summary": "Same", "responses": {}}}
	  }
	}`
	res := convert(t, doc)

	assert.Equal(t, "JSON API", res.Collection.Name)
	assert.Equal(t, "", res.Collection.Vars[BaseURLVar])
	assert.Equal(t, []string{"Same", "Same (1)"}, requestNames(res.Collection.Requests))
	assert.Equal(t, "{{baseUrl}}/b", res.Collection.Requests[1].URL)
	assert.Contains(t, res.Warnings,
		"document has no servers; baseUrl is empty, set it in the environment")
}

func TestConvertRejectsUnsupportedDocuments(t *testing.T) {
	cases := map[string]struct {
		doc     string
		wantErr string
	}{
		"swagger 2": {
			doc:     `{"swagger": "2.0", "paths": {"/a": {"get": {}}}}`,
			wantErr: "swagger 2.0 is not supported",
		},
		"unknown version": {
			doc:     `{"openapi": "4.0.0", "paths": {"/a": {"get": {}}}}`,
			wantErr: `unsupported OpenAPI version "4.0.0"`,
		},
		"no operations": {
			doc:     `{"openapi": "3.0.0", "paths": {}}`,
			wantErr: "no operations found",
		},
		"not an object": {
			doc:     `- one`,
			wantErr: "the top level must be an object",
		},
		"invalid yaml": {
			doc:     "openapi: [",
			wantErr: "parse document",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Convert([]byte(tc.doc))
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestConvertIsDeterministic(t *testing.T) {
	first := convert(t, petShopYAML)
	for range 5 {
		again := convert(t, petShopYAML)
		assert.Equal(t, first, again)
	}
}
