package curl_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/domain"
)

func TestExportRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		request *domain.Request
		want    string
		wantErr string
	}{
		{
			name:    "nil request",
			wantErr: "request is required",
		},
		{
			name:    "blank URL",
			request: &domain.Request{URL: " \t"},
			wantErr: "request URL is required",
		},
		{
			name: "defaults method and ignores blank optional fields",
			request: &domain.Request{
				URL:      "https://example.test/path",
				Method:   "  ",
				Headers:  "  ",
				AuthType: "  ",
			},
			want: "curl -X 'GET' 'https://example.test/path'",
		},
		{
			name: "trims method, sorts headers, and safely quotes values and body",
			request: &domain.Request{
				Method:  " POST ",
				URL:     "https://example.test/items",
				Headers: `{"Z-Trace":"trace'unsafe","Content-Type":"application/json"}`,
				Body:    `{"name":"O'Reilly"}`,
			},
			want: "curl -X 'POST' -H 'Content-Type: application/json' " +
				"-H 'Z-Trace: trace'\\''unsafe' --data-raw '{\"name\":\"O'\\''Reilly\"}' " +
				"'https://example.test/items'",
		},
		{
			name:    "malformed headers",
			request: &domain.Request{URL: "https://example.test", Headers: "{"},
			wantErr: "parse request headers:",
		},
		{
			name: "bearer authorization",
			request: &domain.Request{
				URL: "https://example.test", AuthType: " BEARER ",
				AuthConfig: `{"token":"secret value"}`,
			},
			want: "curl -X 'GET' -H 'Authorization: Bearer secret value' 'https://example.test'",
		},
		{
			name: "basic authorization",
			request: &domain.Request{
				URL: "https://example.test", AuthType: domain.AuthTypeBasic,
				AuthConfig: `{"username":"api","password":"p'ass"}`,
			},
			want: "curl -X 'GET' -u 'api:p'\\''ass' 'https://example.test'",
		},
		{
			name: "API key defaults to header",
			request: &domain.Request{
				URL: "https://example.test", AuthType: domain.AuthTypeAPIKey,
				AuthConfig: `{"name":"X-Key","value":"key value"}`,
			},
			want: "curl -X 'GET' -H 'X-Key: key value' 'https://example.test'",
		},
		{
			name: "API key query preserves existing values",
			request: &domain.Request{
				URL:        "https://example.test/items?keep=one&tag=two&tag=three",
				AuthType:   domain.AuthTypeAPIKey,
				AuthConfig: `{"in":"query","name":"api_key","value":"a b&c"}`,
			},
			want: "curl -X 'GET' 'https://example.test/items?api_key=a+b%26c&keep=one&tag=two&tag=three'",
		},
		{
			name: "API key query replaces existing key",
			request: &domain.Request{
				URL:        "https://example.test/items?api_key=old&keep=yes",
				AuthType:   domain.AuthTypeAPIKey,
				AuthConfig: `{"in":"query","name":"api_key","value":"new"}`,
			},
			want: "curl -X 'GET' 'https://example.test/items?api_key=new&keep=yes'",
		},
		{
			name: "invalid auth config",
			request: &domain.Request{
				URL:        "https://example.test",
				AuthType:   domain.AuthTypeBearer,
				AuthConfig: "{",
			},
			wantErr: "parse request authorization:",
		},
		{
			name: "invalid URL for query API key",
			request: &domain.Request{
				URL: "https://example.test/%zz", AuthType: domain.AuthTypeAPIKey,
				AuthConfig: `{"in":"query","name":"key","value":"value"}`,
			},
			wantErr: "parse request URL:",
		},
		{
			name: "unsupported authorization type",
			request: &domain.Request{
				URL:        "https://example.test",
				AuthType:   "digest",
				AuthConfig: `{}`,
			},
			wantErr: `unsupported authorization type "digest"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := curl.ExportRequest(tc.request)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				require.Empty(t, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// The TUI header editor and both importers store headers as
// {"Name": ["value", ...]}. Export must accept that shape and emit one -H per
// value.
func TestExportRequest_ArrayValuedHeaders(t *testing.T) {
	t.Parallel()

	got, err := curl.ExportRequest(&domain.Request{
		Method:  "GET",
		URL:     "https://example.test",
		Headers: `{"Accept":["application/json"],"X-Tag":["a","b"],"X-Plain":"c"}`,
	})
	require.NoError(t, err)
	require.Equal(t,
		"curl -X 'GET' -H 'Accept: application/json' -H 'X-Plain: c' "+
			"-H 'X-Tag: a' -H 'X-Tag: b' 'https://example.test'",
		got,
	)
}

// A request imported from curl must export again.
func TestExportRequest_RoundTripsImportedHeaders(t *testing.T) {
	t.Parallel()

	imported, err := curl.NewImporter().Parse(
		strings.NewReader(`curl -H 'Accept: application/json' https://example.test`),
	)
	require.NoError(t, err)
	headers, err := json.Marshal(imported.Headers)
	require.NoError(t, err)

	got, err := curl.ExportRequest(&domain.Request{
		Method: imported.Method, URL: imported.URL, Headers: string(headers),
	})
	require.NoError(t, err)
	require.Equal(t, "curl -X 'GET' -H 'Accept: application/json' 'https://example.test'", got)
}

func TestExportRequest_BodyTypes(t *testing.T) {
	t.Parallel()

	form := &domain.Request{
		Method:   "POST",
		URL:      "https://example.test/token",
		Body:     "grant_type=client+credentials&token={{tok}}",
		BodyType: domain.BodyTypeFormURLEncoded,
	}
	got, err := curl.ExportRequest(form)
	require.NoError(t, err)
	assert.Contains(t, got, `-H 'Content-Type: application/x-www-form-urlencoded'`)
	assert.Contains(t, got, `--data-raw 'grant_type=client+credentials&token={{tok}}'`)

	none := &domain.Request{
		Method:   "POST",
		URL:      "https://example.test/",
		Body:     "stale",
		BodyType: domain.BodyTypeNone,
	}
	got, err = curl.ExportRequest(none)
	require.NoError(t, err)
	assert.NotContains(t, got, "--data-raw")
	assert.NotContains(t, got, "Content-Type")

	jsonBody := &domain.Request{
		Method:   "POST",
		URL:      "https://example.test/",
		Headers:  `{"Content-Type":["application/vnd.api+json"]}`,
		Body:     `{"a":1}`,
		BodyType: domain.BodyTypeJSON,
	}
	got, err = curl.ExportRequest(jsonBody)
	require.NoError(t, err)
	assert.Contains(t, got, `application/vnd.api+json`)
	assert.NotContains(t, got, "Content-Type: application/json")

	_, err = curl.ExportRequest(&domain.Request{URL: "https://example.test/", BodyType: "xml"})
	assert.ErrorContains(t, err, `unsupported body type "xml"`)
}

func TestExportRequest_MultipartParts(t *testing.T) {
	t.Parallel()

	body, err := domain.MarshalMultipartParts([]domain.MultipartPart{
		{Name: "title", Value: "@literal"},
		{Name: "upload", File: "/data/doc.txt", Filename: "doc.txt", ContentType: "text/plain"},
	})
	require.NoError(t, err)
	req := &domain.Request{
		Method:   "POST",
		URL:      "https://example.test/upload",
		Headers:  `{"Content-Type":["multipart/form-data; boundary=stale"],"X-Trace":["1"]}`,
		Body:     body,
		BodyType: domain.BodyTypeMultipart,
	}
	got, err := curl.ExportRequest(req)
	require.NoError(t, err)
	assert.Equal(t,
		`curl -X 'POST' -H 'X-Trace: 1' --form-string 'title=@literal' `+
			`-F 'upload=@/data/doc.txt;type=text/plain;filename=doc.txt' 'https://example.test/upload'`,
		got)

	_, err = curl.ExportRequest(
		&domain.Request{
			URL:      "https://example.test/",
			BodyType: domain.BodyTypeMultipart,
			Body:     "x",
		},
	)
	assert.ErrorContains(t, err, "export multipart body")
}
