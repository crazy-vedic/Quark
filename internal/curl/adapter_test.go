package curl_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/domain"
)

const iamMTLSCommand = `curl 'https://access.dev.wealthcareadmin.com/access/connect/token' \
  --header 'Content-Type: application/x-www-form-urlencoded' \
  --cert-type P12 \
  --cert '/mnt/c/Users/vedic.varma/Downloads/dev-auth 1.p12:Password1' \
  --data-urlencode 'user_id=3n1kl3DpdbiW3OANNDq6PA==' \
  --data-urlencode 'grant_type=Cert' \
  --data-urlencode 'client_id=dapr_cron_job' \
  --data-urlencode 'scope=mbi_api offline_access bensoft_api openid profile'`

func TestImporter_ExactIAMMTLSCommand(t *testing.T) {
	result, err := curl.NewImporter().Parse(strings.NewReader(iamMTLSCommand))
	require.NoError(t, err)
	assert.Equal(t, http.MethodPost, result.Method)
	assert.Equal(t, "https://access.dev.wealthcareadmin.com/access/connect/token", result.URL)
	assert.Equal(t, "application/x-www-form-urlencoded", result.Headers.Get("Content-Type"))
	assert.Equal(
		t,
		"user_id=3n1kl3DpdbiW3OANNDq6PA%3D%3D&grant_type=Cert&client_id=dapr_cron_job&scope=mbi_api%20offline_access%20bensoft_api%20openid%20profile",
		result.Body,
	)
	require.NotNil(t, result.Certificate)
	assert.Equal(t, "/mnt/c/Users/vedic.varma/Downloads/dev-auth 1.p12", result.Certificate.File)
	assert.Equal(t, "P12", result.Certificate.Type)
	assert.Equal(t, "Password1", result.Certificate.Password)
}

func TestImporter_RepeatedHeadersArePreserved(t *testing.T) {
	result, err := curl.NewImporter().Parse(strings.NewReader(
		`curl -H 'X-Tag: one' --header='X-Tag: two' https://example.com`,
	))
	require.NoError(t, err)
	assert.Equal(t, []string{"one", "two"}, result.Headers.Values("X-Tag"))
}

func TestImporter_ReadsBodyFile(t *testing.T) {
	dir := t.TempDir()
	bodyPath := filepath.Join(dir, "payload.json")
	require.NoError(t, os.WriteFile(bodyPath, []byte(`{"from":"file"}`), 0o600))

	result, err := curl.NewImporter().Parse(strings.NewReader(
		`curl --data-binary @` + bodyPath + ` https://api.example.com`,
	))
	require.NoError(t, err)
	assert.Equal(t, `{"from":"file"}`, result.Body)
	assert.Equal(t, curl.Dangerous, result.Security)
}

func TestImporter_RejectsMissingOrOversizedBodyFiles(t *testing.T) {
	_, err := curl.NewImporter().Parse(strings.NewReader(
		`curl --data @/definitely/missing/quark-body.json https://api.example.com`,
	))
	assert.ErrorContains(t, err, "read body file")

	dir := t.TempDir()
	bodyPath := filepath.Join(dir, "large.bin")
	require.NoError(t, os.WriteFile(bodyPath, make([]byte, 10<<20+1), 0o600))
	_, err = curl.NewImporter().Parse(strings.NewReader(
		`curl --data @` + bodyPath + ` https://api.example.com`,
	))
	assert.ErrorContains(t, err, "exceeds")
}

func TestImporter_DataRawKeepsAtPrefixLiteral(t *testing.T) {
	result, err := curl.NewImporter().Parse(strings.NewReader(
		`curl --data-raw @payload.json https://api.example.com`,
	))
	require.NoError(t, err)
	assert.Equal(t, "@payload.json", result.Body)
}

func TestImporter_PEMKeyAndCA(t *testing.T) {
	result, err := curl.NewImporter().Parse(strings.NewReader(
		`curl --cert client.pem --key private.key --cacert root.pem https://example.com`,
	))
	require.NoError(t, err)
	require.NotNil(t, result.Certificate)
	assert.Equal(t, &curl.CertificateSpec{
		File: "client.pem", Type: "PEM", KeyFile: "private.key", CAFile: "root.pem",
	}, result.Certificate)
}

func TestImporter_MultipartRepeatedFields(t *testing.T) {
	result, err := curl.NewImporter().Parse(strings.NewReader(
		`curl -F tag=one --form 'tag=two words' -F active=true https://example.com`,
	))
	require.NoError(t, err)
	assert.Equal(t, domain.BodyTypeMultipart, result.BodyType)
	assert.Empty(
		t,
		result.Headers.Get("Content-Type"),
		"the send sets the Content-Type and boundary",
	)
	parts, err := domain.ParseMultipartParts(result.Body)
	require.NoError(t, err)
	assert.Equal(t, []domain.MultipartPart{
		{Name: "tag", Value: "one"},
		{Name: "tag", Value: "two words"},
		{Name: "active", Value: "true"},
	}, parts)
}

func TestImporter_MultipartFileFields(t *testing.T) {
	result, err := curl.NewImporter().Parse(strings.NewReader(
		`curl -F 'doc=@/tmp/report.pdf;type=application/pdf;filename=q1.pdf' -F note=hi https://example.com`,
	))
	require.NoError(t, err)
	parts, err := domain.ParseMultipartParts(result.Body)
	require.NoError(t, err)
	assert.Equal(t, []domain.MultipartPart{
		{Name: "doc", File: "/tmp/report.pdf", Filename: "q1.pdf", ContentType: "application/pdf"},
		{Name: "note", Value: "hi"},
	}, parts)

	_, err = curl.NewImporter().
		Parse(strings.NewReader(`curl -F 'doc=<notes.txt' https://example.com`))
	assert.ErrorContains(t, err, "read from a file")
	_, err = curl.NewImporter().
		Parse(strings.NewReader(`curl -F 'doc=@a.txt;encoder=base64' https://example.com`))
	assert.ErrorContains(t, err, `"encoder" is not imported`)
}

func TestImporter_JSONGetAndPlatformContinuations(t *testing.T) {
	tests := []struct {
		name    string
		command string
		check   func(*testing.T, *curl.ImportResult)
	}{
		{
			"json",
			`curl --json '{"ok":true}' https://example.com`,
			func(t *testing.T, result *curl.ImportResult) {
				assert.Equal(t, "application/json", result.Headers.Get("Content-Type"))
				assert.Equal(t, "application/json", result.Headers.Get("Accept"))
			},
		},
		{
			"get data",
			`curl -G --data-urlencode 'q=two words' https://example.com/search`,
			func(t *testing.T, result *curl.ImportResult) {
				assert.Equal(t, http.MethodGet, result.Method)
				assert.Equal(t, "https://example.com/search?q=two%20words", result.URL)
				assert.Empty(t, result.Body)
			},
		},
		{
			"PowerShell",
			"curl https://example.com `\r\n --header 'X-Shell: powershell'",
			func(t *testing.T, result *curl.ImportResult) {
				assert.Equal(t, "powershell", result.Headers.Get("X-Shell"))
			},
		},
		{
			"cmd",
			"curl https://example.com ^\r\n --header \"X-Shell: cmd\"",
			func(t *testing.T, result *curl.ImportResult) {
				assert.Equal(t, "cmd", result.Headers.Get("X-Shell"))
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := curl.NewImporter().Parse(strings.NewReader(test.command))
			require.NoError(t, err)
			test.check(t, result)
		})
	}
}

func TestImporter_RejectsUnsafeInputs(t *testing.T) {
	inputs := []string{
		`curl "https://example.com/$TOKEN"`,
		`curl https://example.com/$(whoami)`,
		`curl https://example.com | powershell`,
		`curl https://example.com > output.txt`,
		`TOKEN=x curl https://example.com`,
		`curl -d @secrets.txt https://example.com`,
		`curl --data-binary @- https://example.com`,
		`curl --data-urlencode field@secrets.txt https://example.com`,
		`curl -H @headers.txt https://example.com`,
		`curl --cookie @cookies.txt https://example.com`,
		`curl --config secrets.curlrc https://example.com`,
		`curl https://example.com https://second.example.com`,
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			_, err := curl.NewImporter().Parse(strings.NewReader(input))
			assert.Error(t, err)
		})
	}
}

func BenchmarkImporter_IAMMTLSCommand(b *testing.B) {
	importer := curl.NewImporter()
	for b.Loop() {
		result, err := importer.Parse(strings.NewReader(iamMTLSCommand))
		if err != nil || result.URL == "" {
			b.Fatal(err)
		}
	}
}
