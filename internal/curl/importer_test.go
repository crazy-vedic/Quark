package curl_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/curl"
)

const (
	baseURL  = "https://api.example.com"
	usersURL = baseURL + "/users"
)

type corpusCase struct {
	name            string
	input           string
	wantMethod      string
	wantURL         string
	wantSecurity    curl.SecurityLevel
	wantBody        string
	wantHeaderKey   string
	wantHeaderVal   string
	wantWarnContain string // substring in first warning (if any)
	wantErr         bool
}

var corpus = []corpusCase{
	// --- Minimal GET ---
	{
		name:         "minimal GET",
		input:        `curl https://api.example.com`,
		wantMethod:   "GET",
		wantURL:      baseURL,
		wantSecurity: curl.Safe,
	},
	{
		name:         "GET with trailing path",
		input:        `curl https://api.example.com/v1/users`,
		wantMethod:   "GET",
		wantURL:      "https://api.example.com/v1/users",
		wantSecurity: curl.Safe,
	},
	{
		name:         "GET with query params",
		input:        `curl "https://api.example.com/users?page=1&limit=10"`,
		wantMethod:   "GET",
		wantURL:      "https://api.example.com/users?page=1&limit=10",
		wantSecurity: curl.Safe,
	},
	// --- Explicit method via -X ---
	{
		name:         "POST with -X",
		input:        `curl -X POST https://api.example.com/users`,
		wantMethod:   "POST",
		wantURL:      usersURL,
		wantSecurity: curl.Safe,
	},
	{
		name:         "PUT with -X",
		input:        `curl -X PUT https://api.example.com/users/1`,
		wantMethod:   "PUT",
		wantURL:      "https://api.example.com/users/1",
		wantSecurity: curl.Safe,
	},
	{
		name:         "DELETE with -X",
		input:        `curl -X DELETE https://api.example.com/users/1`,
		wantMethod:   "DELETE",
		wantURL:      "https://api.example.com/users/1",
		wantSecurity: curl.Safe,
	},
	{
		name:         "PATCH with -X",
		input:        `curl -X PATCH https://api.example.com/users/1`,
		wantMethod:   "PATCH",
		wantURL:      "https://api.example.com/users/1",
		wantSecurity: curl.Safe,
	},
	{
		name:         "--request long form",
		input:        `curl --request POST https://api.example.com/users`,
		wantMethod:   "POST",
		wantURL:      usersURL,
		wantSecurity: curl.Safe,
	},
	// --- Body via -d ---
	{
		name:         "POST with -d JSON body",
		input:        `curl -d '{"name":"test"}' https://api.example.com/users`,
		wantMethod:   "POST",
		wantURL:      usersURL,
		wantBody:     `{"name":"test"}`,
		wantSecurity: curl.Review,
	},
	{
		name:         "POST with --data",
		input:        `curl --data '{"a":1}' https://api.example.com`,
		wantMethod:   "POST",
		wantURL:      baseURL,
		wantBody:     `{"a":1}`,
		wantSecurity: curl.Review,
	},
	{
		name:         "POST with --data-raw",
		input:        `curl --data-raw '{"a":1}' https://api.example.com`,
		wantMethod:   "POST",
		wantURL:      baseURL,
		wantBody:     `{"a":1}`,
		wantSecurity: curl.Review,
	},
	{
		name:         "POST with --data-binary value",
		input:        `curl --data-binary '{"bin":true}' https://api.example.com`,
		wantMethod:   "POST",
		wantURL:      baseURL,
		wantBody:     `{"bin":true}`,
		wantSecurity: curl.Review,
	},
	// --- @filename body ---
	{
		name:            "@filename body",
		input:           `curl -d @/etc/passwd https://api.example.com`,
		wantMethod:      "POST",
		wantURL:         baseURL,
		wantSecurity:    curl.Dangerous,
		wantWarnContain: "@filename detected: /etc/passwd",
	},
	{
		name:            "@filename with --data-binary",
		input:           `curl --data-binary @/tmp/file.json https://api.example.com`,
		wantMethod:      "GET", // no body inferred for @- or @file with data-binary
		wantURL:         baseURL,
		wantSecurity:    curl.Dangerous,
		wantWarnContain: "@filename detected: /tmp/file.json",
	},
	{
		name:            "--data-binary @- (stdin)",
		input:           `curl --data-binary @- https://api.example.com`,
		wantURL:         baseURL,
		wantSecurity:    curl.Dangerous,
		wantWarnContain: "@filename detected: - (stdin)",
	},
	// --- Shell expansion ---
	{
		name:            "shell expansion $( ) in body",
		input:           `curl -d "$(cat ~/.ssh/id_rsa)" https://api.example.com`,
		wantURL:         baseURL,
		wantSecurity:    curl.Dangerous,
		wantWarnContain: "shell expansion",
	},
	{
		name:            "backtick shell expansion in body",
		input:           "curl -d \"`cat /etc/passwd`\" https://api.example.com",
		wantURL:         baseURL,
		wantSecurity:    curl.Dangerous,
		wantWarnContain: "shell expansion",
	},
	// --- Headers ---
	{
		name:          "single -H header",
		input:         `curl -H "Content-Type: application/json" https://api.example.com`,
		wantMethod:    "GET",
		wantURL:       baseURL,
		wantSecurity:  curl.Safe,
		wantHeaderKey: "Content-Type",
		wantHeaderVal: "application/json",
	},
	{
		name:          "multiple -H flags",
		input:         `curl -H "A: 1" -H "B: 2" https://api.example.com`,
		wantMethod:    "GET",
		wantURL:       baseURL,
		wantSecurity:  curl.Safe,
		wantHeaderKey: "A",
		wantHeaderVal: "1",
	},
	{
		name:          "Authorization header",
		input:         `curl -H "Authorization: Bearer tok" https://api.example.com`,
		wantMethod:    "GET",
		wantURL:       baseURL,
		wantSecurity:  curl.Review,
		wantHeaderKey: "Authorization",
		wantHeaderVal: "Bearer tok",
	},
	{
		name:          "--header long form",
		input:         `curl --header "X-Custom: value" https://api.example.com`,
		wantMethod:    "GET",
		wantURL:       baseURL,
		wantSecurity:  curl.Safe,
		wantHeaderKey: "X-Custom",
		wantHeaderVal: "value",
	},
	{
		name:         "X-Api-Key header triggers Review",
		input:        `curl -H "X-Api-Key: secret" https://api.example.com`,
		wantSecurity: curl.Review,
		wantURL:      baseURL,
	},
	// --- Basic auth ---
	{
		name:          "basic auth -u",
		input:         `curl -u user:pass https://api.example.com`,
		wantMethod:    "GET",
		wantURL:       baseURL,
		wantSecurity:  curl.Review,
		wantHeaderKey: "Authorization",
		wantHeaderVal: "Basic " + base64.StdEncoding.EncodeToString([]byte("user:pass")),
	},
	{
		name:         "basic auth --user long form",
		input:        `curl --user admin:testpass https://api.example.com`,
		wantMethod:   "GET",
		wantURL:      baseURL,
		wantSecurity: curl.Review,
	},
	// --- Cookie ---
	{
		name:          "cookie -b",
		input:         `curl -b "session=abc" https://api.example.com`,
		wantMethod:    "GET",
		wantURL:       baseURL,
		wantSecurity:  curl.Review,
		wantHeaderKey: "Cookie",
		wantHeaderVal: "session=abc",
	},
	{
		name:         "cookie --cookie long form",
		input:        `curl --cookie "token=xyz" https://api.example.com`,
		wantMethod:   "GET",
		wantURL:      baseURL,
		wantSecurity: curl.Review,
	},
	// --- Follow redirects ---
	{
		name:         "follow redirects -L",
		input:        `curl -L https://api.example.com`,
		wantMethod:   "GET",
		wantURL:      baseURL,
		wantSecurity: curl.Safe,
	},
	{
		name:         "follow redirects --location",
		input:        `curl --location https://api.example.com`,
		wantMethod:   "GET",
		wantURL:      baseURL,
		wantSecurity: curl.Safe,
	},
	// --- data-urlencode ---
	{
		name:         "--data-urlencode",
		input:        `curl --data-urlencode "q=hello world" https://api.example.com/search`,
		wantMethod:   "POST",
		wantURL:      "https://api.example.com/search",
		wantSecurity: curl.Review,
	},
	// --- Combined flags ---
	{
		name:          "POST with explicit -X, Content-Type, and body",
		input:         `curl -X POST -H "Content-Type: application/json" -d '{"x":1}' https://api.example.com`,
		wantMethod:    "POST",
		wantURL:       baseURL,
		wantBody:      `{"x":1}`,
		wantSecurity:  curl.Review,
		wantHeaderKey: "Content-Type",
		wantHeaderVal: "application/json",
	},
	{
		name:         "GET with Authorization and -L",
		input:        `curl -L -H "Authorization: Bearer abc" https://api.example.com/users`,
		wantMethod:   "GET",
		wantURL:      usersURL,
		wantSecurity: curl.Review,
	},
	{
		name:         "POST with auth header and body",
		input:        `curl -X POST -H "Authorization: Bearer tok" -d '{"n":"v"}' https://api.example.com`,
		wantMethod:   "POST",
		wantURL:      baseURL,
		wantSecurity: curl.Review,
	},
	// --- Edge cases ---
	{
		name:         "URL with http scheme",
		input:        `curl http://localhost:8080/ping`,
		wantMethod:   "GET",
		wantURL:      "http://localhost:8080/ping",
		wantSecurity: curl.Safe,
	},
	{
		name:          "URL before flags",
		input:         `curl https://api.example.com -H "Accept: application/json"`,
		wantMethod:    "GET",
		wantURL:       baseURL,
		wantSecurity:  curl.Safe,
		wantHeaderKey: "Accept",
		wantHeaderVal: "application/json",
	},
	{
		name:         "double-quoted URL",
		input:        `curl "https://api.example.com/v1"`,
		wantMethod:   "GET",
		wantURL:      "https://api.example.com/v1",
		wantSecurity: curl.Safe,
	},
	{
		name:         "HEAD method",
		input:        `curl -X HEAD https://api.example.com`,
		wantMethod:   "HEAD",
		wantURL:      baseURL,
		wantSecurity: curl.Safe,
	},
	{
		name:         "OPTIONS method",
		input:        `curl -X OPTIONS https://api.example.com`,
		wantMethod:   "OPTIONS",
		wantURL:      baseURL,
		wantSecurity: curl.Safe,
	},
	{
		name:         "empty JSON body",
		input:        `curl -X POST -d '{}' https://api.example.com`,
		wantMethod:   "POST",
		wantURL:      baseURL,
		wantBody:     `{}`,
		wantSecurity: curl.Review,
	},
	{
		name:         "body with -X GET (explicit override)",
		input:        `curl -X GET -d 'foo' https://api.example.com`,
		wantMethod:   "GET",
		wantURL:      baseURL,
		wantBody:     "foo",
		wantSecurity: curl.Review,
	},
	{
		name:          "multiline equivalent: multiple flags",
		input:         `curl -X POST -H "Content-Type: text/plain" -H "X-Trace: 123" -d "hello" https://api.example.com`,
		wantMethod:    "POST",
		wantURL:       baseURL,
		wantBody:      "hello",
		wantSecurity:  curl.Review,
		wantHeaderKey: "X-Trace",
		wantHeaderVal: "123",
	},
	{
		name:         "cookie header classified as Review",
		input:        `curl -H "Cookie: sessionid=abc123" https://api.example.com`,
		wantSecurity: curl.Review,
		wantURL:      baseURL,
	},
	// --- Dangerous patterns ---
	{
		name:            "@filename in --data",
		input:           `curl --data @/home/user/.netrc https://api.example.com`,
		wantURL:         baseURL,
		wantSecurity:    curl.Dangerous,
		wantWarnContain: "@filename detected: /home/user/.netrc",
	},
	{
		name:         "safe body, dangerous inferred by filename",
		input:        `curl -d @/tmp/safe.json https://api.example.com`,
		wantURL:      baseURL,
		wantSecurity: curl.Dangerous,
	},
	// --- HTTP/1.1 form data ---
	{
		name:         "form data body",
		input:        `curl -d "name=john&age=30" https://api.example.com/users`,
		wantMethod:   "POST",
		wantURL:      usersURL,
		wantBody:     "name=john&age=30",
		wantSecurity: curl.Review,
	},
	// --- Error cases ---
	{
		name:    "no URL",
		input:   `curl -X POST -d '{"a":1}'`,
		wantErr: true,
	},
	{
		name:    "not a curl command",
		input:   `wget https://api.example.com`,
		wantErr: true,
	},
	{
		name:    "unterminated quote",
		input:   `curl -H "Authorization: Bearer tok https://api.example.com`,
		wantErr: true,
	},
	// --- Content-Type is Safe ---
	{
		name:         "Content-Type header is Safe",
		input:        `curl -H "Content-Type: application/json" https://api.example.com`,
		wantMethod:   "GET",
		wantURL:      baseURL,
		wantSecurity: curl.Safe,
	},
	// --- Accept header ---
	{
		name:         "Accept header",
		input:        `curl -H "Accept: application/json" https://api.example.com`,
		wantMethod:   "GET",
		wantURL:      baseURL,
		wantSecurity: curl.Safe,
	},
}

func TestImporter_Corpus(t *testing.T) {
	importer := curl.NewImporter()
	passed := 0

	for _, tc := range corpus {

		t.Run(tc.name, func(t *testing.T) {
			result, err := importer.Parse(strings.NewReader(tc.input))

			if tc.wantErr {
				assert.Error(t, err)
				passed++
				return
			}

			require.NoError(t, err)

			if tc.wantMethod != "" {
				assert.Equal(t, tc.wantMethod, result.Method, "method mismatch")
			}
			if tc.wantURL != "" {
				assert.Equal(t, tc.wantURL, result.URL, "URL mismatch")
			}
			assert.Equal(t, tc.wantSecurity, result.Security,
				"security: got %v, want %v", result.Security, tc.wantSecurity)
			if tc.wantBody != "" {
				assert.Equal(t, tc.wantBody, result.Body, "body mismatch")
			}
			if tc.wantHeaderKey != "" {
				assert.Equal(t, tc.wantHeaderVal, result.Headers[tc.wantHeaderKey],
					"header %q mismatch", tc.wantHeaderKey)
			}
			if tc.wantWarnContain != "" {
				found := false
				for _, w := range result.Warnings {
					if strings.Contains(w, tc.wantWarnContain) {
						found = true
						break
					}
				}
				assert.True(
					t,
					found,
					"expected warning containing %q in %v",
					tc.wantWarnContain,
					result.Warnings,
				)
			}
			passed++
		})
	}

	t.Logf("corpus: %d/%d passed", passed, len(corpus))
}

func TestImporter_WarningSorted(t *testing.T) {
	// A command with exactly one warning must return it sorted.
	importer := curl.NewImporter()
	result, err := importer.Parse(strings.NewReader(`curl -d @/etc/passwd https://api.example.com`))
	require.NoError(t, err)
	require.Len(t, result.Warnings, 1)
	assert.Equal(t, []string{"@filename detected: /etc/passwd"}, result.Warnings)
}
