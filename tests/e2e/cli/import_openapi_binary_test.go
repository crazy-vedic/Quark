//go:build e2e

package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

// writeSpec writes an OpenAPI document into a fresh directory and returns its
// path.
func writeSpec(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// importedRequests opens the home database and returns every request in the
// root collection named col and in its folders, keyed by request name.
func importedRequests(t *testing.T, home, col string) map[string]*domain.Request {
	t.Helper()
	st, err := store.New(filepath.Join(home, ".quark", "quark.db"), store.WithCacheSize(100))
	require.NoError(t, err)
	defer st.Close()
	ctx := context.Background()
	all, err := st.ListCollections(ctx)
	require.NoError(t, err)

	byID := map[string]*domain.Collection{}
	var root *domain.Collection
	for _, c := range all {
		byID[c.ID] = c
		if c.ParentID == "" && c.Name == col {
			require.Nil(t, root, "more than one root collection named %q", col)
			root = c
		}
	}
	require.NotNil(t, root, "no root collection named %q", col)

	out := map[string]*domain.Request{}
	for _, c := range all {
		if !inCollectionTree(c, root.ID, byID) {
			continue
		}
		reqs, err := st.ListRequests(ctx, c.ID)
		require.NoError(t, err)
		for _, r := range reqs {
			out[r.Name] = r
		}
	}
	return out
}

// inCollectionTree reports whether c is the root or one of its descendants.
func inCollectionTree(c *domain.Collection, rootID string, byID map[string]*domain.Collection) bool {
	for id := c.ID; id != ""; {
		if id == rootID {
			return true
		}
		parent, ok := byID[id]
		if !ok {
			return false
		}
		id = parent.ParentID
	}
	return false
}

// richSpec covers parameters, security schemes, references, bodies, examples
// and the constructs that are reported as warnings.
const richSpec = `
openapi: 3.0.3
info:
  title: Payments API
  description: Sample payments service
servers:
  - url: https://{env}.pay.example.com/v{version}/
    variables:
      env:
        default: api
      version:
        default: "2"
  - url: https://backup.example.com
security:
  - keyQuery: []
webhooks:
  newCharge:
    post:
      responses:
        '200':
          description: ok
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: Bearer
    basicAuth:
      type: http
      scheme: basic
    keyHeader:
      type: apiKey
      in: header
      name: X-Api-Key
    keyQuery:
      type: apiKey
      in: query
      name: api_key
    keyCookie:
      type: apiKey
      in: cookie
      name: sid
    oauth:
      type: oauth2
      flows: {}
  parameters:
    PageSize:
      name: limit
      in: query
      required: true
      schema:
        type: integer
        default: 20
  schemas:
    Charge:
      type: object
      properties:
        id:
          type: string
          example: ch_1
        amount:
          type: integer
        currency:
          type: string
          enum: [usd, eur]
        paid:
          type: boolean
        tags:
          type: array
          items:
            type: string
        meta:
          type: object
          properties:
            source:
              type: string
        nothing: {}
        kind:
          type: string
          default: card
    Extended:
      allOf:
        - $ref: '#/components/schemas/Charge'
        - type: object
          properties:
            extra:
              type: number
    Choice:
      oneOf:
        - $ref: '#/components/schemas/Charge'
        - type: string
    Loop:
      type: object
      properties:
        self:
          $ref: '#/components/schemas/Loop'
    Pointer:
      type: string
      example: "a/b~c"
  requestBodies:
    ChargeBody:
      content:
        application/json:
          schema:
            $ref: '#/components/schemas/Charge'
paths:
  /charges/{chargeId}:
    parameters:
      - name: chargeId
        in: path
        required: true
        example: ch_42
      - name: X-Trace
        in: header
        required: true
        example: trace-1
      - name: fields
        in: query
        required: true
        example: [id, amount]
      - name: sort
        in: query
        required: false
        example: asc
    get:
      tags: [Charges]
      summary: Get a charge
      security:
        - bearerAuth: []
      parameters:
        - $ref: '#/components/parameters/PageSize'
        - name: X-Trace
          in: header
          required: true
          examples:
            first:
              value: override-trace
            second:
              value: other
        - name: filter
          in: query
          required: true
          example:
            b: 1
          schema:
            type: object
        - name: meta
          in: query
          required: true
          schema:
            type: string
            enum: [x, y]
        - name: amounts
          in: query
          required: true
          schema:
            type: number
            example: 12.5
        - name: nothing
          in: query
          required: true
        - name: session
          in: cookie
          required: true
          example: abc
      responses:
        '200':
          description: ok
      callbacks:
        onEvent:
          '{$request.body#/url}':
            post:
              responses:
                '200':
                  description: ok
    put:
      tags: [Charges]
      operationId: updateCharge
      security:
        - basicAuth: []
      requestBody:
        $ref: '#/components/requestBodies/ChargeBody'
      responses:
        '200':
          description: ok
    delete:
      tags: [Charges]
      security:
        - keyHeader: []
      responses:
        '204':
          description: gone
    patch:
      security:
        - keyQuery: []
      requestBody:
        content:
          application/vnd.api+json:
            example:
              data:
                type: charges
    head:
      security:
        - keyCookie: []
    options:
      security:
        - oauth: []
    trace:
      security:
        - {}
  /charges:
    post:
      tags: [Charges]
      summary: Create a charge
      servers:
        - url: https://op.example.com
      security:
        - bearerAuth: []
          basicAuth: []
        - keyHeader: []
      requestBody:
        content:
          application/json:
            examples:
              first:
                value:
                  id: ch_new
                  tags: [a]
              second:
                value:
                  id: ch_other
      responses:
        '201':
          description: created
    get:
      tags: [Charges]
      summary: List charges
      parameters:
        - $ref: '#/components/parameters/PageSize'
      security:
        - {}
      responses:
        '200':
          description: ok
  /charges-copy:
    get:
      tags: [Charges]
      summary: List charges
      security:
        - missingScheme: []
      responses:
        '200':
          description: ok
  /extended:
    post:
      tags: [Charges]
      summary: Create extended
      requestBody:
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/Extended'
      responses:
        '201':
          description: created
  /choice:
    put:
      tags: [Charges]
      summary: Choice body
      requestBody:
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/Choice'
      responses:
        '200':
          description: ok
  /loop:
    post:
      tags: [Charges]
      summary: Recursive body
      requestBody:
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/Loop'
      responses:
        '200':
          description: ok
  /pointer:
    get:
      tags: [Charges]
      summary: Pointer example
      parameters:
        - name: p
          in: query
          required: true
          schema:
            $ref: '#/components/schemas/Pointer'
        - name: fromExternal
          in: query
          required: true
          schema:
            $ref: 'https://other.example.com/schema.yaml'
        - name: unresolved
          in: query
          required: true
          schema:
            $ref: '#/components/schemas/Nope'
        - name: viaArray
          in: query
          required: true
          schema:
            $ref: '#/paths/~1charges/get/tags/0'
        - name: badIndex
          in: query
          required: true
          schema:
            $ref: '#/paths/~1charges/get/tags/9'
        - name: throughString
          in: query
          required: true
          schema:
            $ref: '#/components/schemas/Pointer/type/deeper'
      responses:
        '200':
          description: ok
  /forms:
    post:
      tags: [Forms]
      summary: Submit a form
      requestBody:
        content:
          application/x-www-form-urlencoded:
            schema:
              type: object
              properties:
                name:
                  type: string
          multipart/form-data:
            schema:
              type: object
      responses:
        '200':
          description: ok
`

func TestCLI_ImportOpenAPIMapsParametersSecurityAndWarnings(t *testing.T) {
	home, _ := setupHomeWithCollection(t)
	spec := writeSpec(t, "payments.yaml", richSpec)

	out, errOut, code := runQuarkWithHome(t, home, "import-openapi", spec)
	require.Equal(t, 0, code, "stderr: %s", errOut)
	assert.Contains(t, out, `Imported`)
	assert.Contains(t, out, `into "Payments API"`)
	for _, warning := range []string{
		"Warning: cookie parameters are not imported",
		"Warning: callbacks are not imported",
		"Warning: webhooks are not imported",
		"Warning: per-operation servers are ignored",
		"Warning: only the first server",
		"Warning: TRACE operations are not imported",
		"Warning: security requirements that combine several schemes are not supported",
		`Warning: security scheme "oauth" (oauth2) is not supported`,
		`Warning: external $ref "https://other.example.com/schema.yaml" is not followed`,
		`Warning: unresolved $ref "#/components/schemas/Nope"`,
		"Warning: POST /forms: application/x-www-form-urlencoded request bodies are not generated",
	} {
		assert.Contains(t, errOut, warning)
	}

	reqs := importedRequests(t, home, "Payments API")

	get := reqs["Get a charge"]
	require.NotNil(t, get, "imported requests: %v", keysOf(reqs))
	assert.Equal(t, "GET", get.Method)
	assert.Equal(t, domain.AuthTypeBearer, get.AuthType)
	assert.Equal(t,
		"{{baseUrl}}/charges/{{chargeId}}?amounts=12.5&fields=id%2Camount&filter=%7B%22b%22%3A1%7D&limit=20&meta=x&nothing=",
		get.URL)
	var headers map[string]string
	require.NoError(t, json.Unmarshal([]byte(get.Headers), &headers))
	assert.Equal(t, "override-trace", headers["X-Trace"], "a documented example wins")

	created := reqs["Create a charge"]
	require.NotNil(t, created)
	assert.Equal(t, "POST", created.Method)
	assert.Equal(t, "application/json", headerValue(t, created.Headers, "Content-Type"))
	assert.Contains(t, created.Body, `"id": "ch_new"`)
	assert.Equal(t, domain.AuthTypeAPIKey, created.AuthType,
		"the combined requirement is skipped and the next one is used")

	deleted := reqs["DELETE -charges-{chargeId}"]
	require.NotNil(t, deleted, "imported requests: %v", keysOf(reqs))
	assert.Equal(t, domain.AuthTypeAPIKey, deleted.AuthType)
	assert.Equal(t, "X-Api-Key", authField(t, deleted.AuthConfig, "name"))

	updated := reqs["updateCharge"]
	require.NotNil(t, updated)
	assert.Equal(t, domain.AuthTypeBasic, updated.AuthType)
	assert.Contains(t, updated.Body, `"amount": 0`)

	extended := reqs["Create extended"]
	require.NotNil(t, extended)
	assert.Contains(t, extended.Body, `"extra": 0`, "allOf members are merged")

	choice := reqs["Choice body"]
	require.NotNil(t, choice)
	assert.Contains(t, choice.Body, `"currency": "usd"`, "oneOf uses its first option")

	loop := reqs["Recursive body"]
	require.NotNil(t, loop)
	assert.Contains(t, loop.Body, `"self"`, "recursive schemas stop at the depth limit")

	assert.NotContains(t, reqs, "TRACE /charges/{chargeId}", "TRACE is not imported")
}

func TestCLI_ImportOpenAPIUsesDocumentDefaultsWhenServersMissing(t *testing.T) {
	home, _ := setupHomeWithCollection(t)
	spec := writeSpec(t, "bare.yaml", `
openapi: 3.1.0
info:
  title: Bare
paths:
  /ping:
    get:
      summary: Ping
      requestBody:
        description: no content here
      responses:
        '200':
          description: ok
`)

	out, errOut, code := runQuarkWithHome(t, home, "import-openapi", spec)
	require.Equal(t, 0, code, "stderr: %s", errOut)
	assert.Contains(t, out, `into "Bare"`)
	assert.Contains(t, errOut, "Warning: document has no servers; baseUrl is empty")

	reqs := importedRequests(t, home, "Bare")
	require.NotNil(t, reqs["Ping"])
	assert.Equal(t, "{{baseUrl}}/ping", reqs["Ping"].URL)
	assert.Empty(t, reqs["Ping"].Body)
}

func TestCLI_ImportOpenAPIWarnsOnRelativeServerURL(t *testing.T) {
	home, _ := setupHomeWithCollection(t)
	spec := writeSpec(t, "relative.yaml", `
openapi: 3.0.0
info:
  title: Relative
servers:
  - url: /api/v1/
paths:
  /ping:
    get:
      responses:
        '200':
          description: ok
`)

	out, errOut, code := runQuarkWithHome(t, home, "import-openapi", spec)
	require.Equal(t, 0, code, "stderr: %s", errOut)
	assert.Contains(t, out, `into "Relative"`)
	assert.Contains(t, errOut, `Warning: server URL "/api/v1/" is relative; set baseUrl in the environment`)
}

func TestCLI_ImportOpenAPIRejectsUnsupportedDocuments(t *testing.T) {
	home, _ := setupHomeWithCollection(t)
	cases := map[string]struct {
		content string
		want    string
	}{
		"invalid yaml": {
			content: "openapi: [unclosed",
			want:    "parse document",
		},
		"top level list": {
			content: "- openapi: 3.0.0\n",
			want:    "the top level must be an object",
		},
		"swagger 2": {
			content: "swagger: '2.0'\ninfo: {title: Old}\npaths: {}\n",
			want:    "swagger 2.0 is not supported",
		},
		"openapi 2": {
			content: "openapi: '2.0'\npaths: {}\n",
			want:    `unsupported OpenAPI version "2.0"`,
		},
		"no operations": {
			content: "openapi: 3.0.0\ninfo: {title: Empty}\npaths: {}\n",
			want:    "no operations found in paths",
		},
		"only trace": {
			content: "openapi: 3.0.0\npaths:\n  /x:\n    trace:\n      responses:\n        '200': {description: ok}\n",
			want:    "no operations found in paths",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			spec := writeSpec(t, "bad.yaml", tc.content)
			_, errOut, code := runQuarkWithHome(t, home, "import-openapi", spec)
			assert.NotEqual(t, 0, code)
			assert.Contains(t, errOut, tc.want)
		})
	}

	_, errOut, code := runQuarkWithHome(t, home, "import-openapi", filepath.Join(home, "missing.yaml"))
	assert.NotEqual(t, 0, code)
	assert.Contains(t, errOut, "import-openapi:")
}

func TestCLI_ImportOpenAPIDuplicateHandling(t *testing.T) {
	home, _ := setupHomeWithCollection(t)
	spec := writeSpec(t, "dup.yaml", `
openapi: 3.0.0
info:
  title: Dup
paths:
  /a:
    get:
      summary: Same
      responses:
        '200': {description: ok}
  /b:
    get:
      summary: Same
      responses:
        '200': {description: ok}
`)

	out, errOut, code := runQuarkWithHome(t, home, "import-openapi", spec)
	require.Equal(t, 0, code, "stderr: %s", errOut)
	assert.Contains(t, out, `Imported 2 requests (0 folders) into "Dup"`)
	reqs := importedRequests(t, home, "Dup")
	assert.NotNil(t, reqs["Same"])
	assert.NotNil(t, reqs["Same (1)"], "a repeated name in one folder gets a counter")

	out, _, code = runQuarkWithHome(t, home, "import-openapi", spec)
	require.Equal(t, 0, code)
	assert.Contains(t, out, `into "Dup 1" (duplicated)`, "duplicate keeps both collections")

	out, _, code = runQuarkWithHome(t, home, "import-openapi", spec, "--on-duplicate", "skip")
	require.Equal(t, 0, code)
	assert.Contains(t, out, `Skipped "Dup" (2 requests)`)

	out, _, code = runQuarkWithHome(t, home, "import-openapi", spec, "--on-duplicate", "replace")
	require.Equal(t, 0, code)
	assert.Contains(t, out, `into "Dup" (replaced)`)

	_, errOut, code = runQuarkWithHome(t, home, "import-openapi", spec, "--on-duplicate", "merge")
	assert.NotEqual(t, 0, code)
	assert.Contains(t, errOut, `invalid --on-duplicate "merge"`)
}

func TestCLI_ImportOpenAPICollectionNameOverride(t *testing.T) {
	home, _ := setupHomeWithCollection(t)
	spec := writeSpec(t, "named.yaml", `
openapi: 3.0.0
info:
  title: Ignored Title
  description: Folder description
paths:
  /a:
    get:
      tags: [Pets]
      responses:
        '200': {description: ok}
`)

	out, errOut, code := runQuarkWithHome(t, home, "import-openapi", spec, "-n", "Renamed")
	require.Equal(t, 0, code, "stderr: %s", errOut)
	assert.Contains(t, out, `Imported 1 requests (1 folders) into "Renamed"`)
	reqs := importedRequests(t, home, "Renamed")
	require.NotNil(t, reqs["GET -a"], "names keep the label with '/' replaced")
}

func headerValue(t *testing.T, headers, name string) string {
	t.Helper()
	var m map[string]string
	require.NoError(t, json.Unmarshal([]byte(headers), &m))
	return m[name]
}

func authField(t *testing.T, config, field string) string {
	t.Helper()
	var m map[string]string
	require.NoError(t, json.Unmarshal([]byte(config), &m))
	return m[field]
}

func keysOf(m map[string]*domain.Request) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
