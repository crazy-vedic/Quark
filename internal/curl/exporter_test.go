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

func TestExportRequest_WritesRequestOptionsAndRoundTrips(t *testing.T) {
	follow := true
	req := &domain.Request{
		Method:  "GET",
		URL:     "https://api.example.com/items",
		Headers: "{}",
		Options: domain.RequestOptions{
			TimeoutMS:       2500,
			FollowRedirects: &follow,
			MaxRedirects:    intPtrForExport(4),
			Proxy:           "http://proxy.internal:3128",
			SkipTLSVerify:   true,
		},
	}
	command, err := curl.ExportRequest(req)
	require.NoError(t, err)
	require.Contains(t, command, "-k")
	require.Contains(t, command, "-L")
	require.Contains(t, command, "--max-redirs 4")
	require.Contains(t, command, "--max-time 2.5")
	require.Contains(t, command, "-x 'http://proxy.internal:3128'")

	parsed, err := curl.NewImporter().Parse(strings.NewReader(command))
	require.NoError(t, err)
	assert.Equal(t, req.Options.TimeoutMS, parsed.Options.TimeoutMS)
	assert.True(t, parsed.Options.SkipTLSVerify)
	require.NotNil(t, parsed.Options.FollowRedirects)
	assert.True(t, *parsed.Options.FollowRedirects)
	require.NotNil(t, parsed.Options.MaxRedirects)
	assert.Equal(t, 4, *parsed.Options.MaxRedirects)
	assert.Equal(t, req.Options.Proxy, parsed.Options.Proxy)
}

func TestExportRequest_NoRedirectsStaysNoRedirects(t *testing.T) {
	off := false
	req := &domain.Request{
		Method:  "GET",
		URL:     "https://api.example.com/items",
		Headers: "{}",
		Options: domain.RequestOptions{FollowRedirects: &off},
	}
	command, err := curl.ExportRequest(req)
	require.NoError(t, err)

	parsed, err := curl.NewImporter().Parse(strings.NewReader(command))
	require.NoError(t, err)
	require.NotNil(t, parsed.Options.MaxRedirects)
	assert.Zero(t, *parsed.Options.MaxRedirects,
		"the exported form must follow no redirects, as the saved request did")
}

func TestExportRequest_DefaultRequestHasNoOptionFlags(t *testing.T) {
	command, err := curl.ExportRequest(&domain.Request{
		Method:  "GET",
		URL:     "https://api.example.com",
		Headers: "{}",
	})
	require.NoError(t, err)
	for _, flag := range []string{" -k", " -L", "--max-redirs", "--max-time", " -x "} {
		require.NotContains(t, command, flag)
	}
}

func intPtrForExport(v int) *int { return &v }
