package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	osExec "os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"software.sslmate.com/src/go-pkcs12"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

const cliE2EHelperEnv = "QUARK_CLI_E2E_HELPER"

// TestMain turns the package's own test executable into a real Quark CLI
// process for the tests below. This exercises run(), Cobra's root command,
// lazy runtime construction, SQLite persistence, and transport configuration
// without maintaining a second test-only command tree.
func TestMain(m *testing.M) {
	if os.Getenv(cliE2EHelperEnv) == "1" {
		separator := -1
		for index, arg := range os.Args {
			if arg == "--" {
				separator = index
				break
			}
		}
		if separator < 0 {
			fmt.Fprintln(os.Stderr, "quark e2e helper: missing argument separator")
			os.Exit(2)
		}
		os.Args = append([]string{"quark"}, os.Args[separator+1:]...)
		if err := run(); err != nil {
			fmt.Fprintf(os.Stderr, "quark: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestCLIImportCurlEndToEndPersistsIAMRequestAndMTLS(t *testing.T) {
	configDir := t.TempDir()
	collectionID := seedCLICollection(t, configDir, "Imported APIs")
	certificatePath := writeCLITestPKCS12(t, "Password1")

	command := fmt.Sprintf(`curl 'https://access.dev.wealthcareadmin.com/access/connect/token' \
  --header 'Content-Type: application/x-www-form-urlencoded' \
  --header 'X-Trace: first' \
  --header 'X-Trace: second' \
  --cert-type P12 \
  --cert '%s:Password1' \
  --data-urlencode 'user_id=3n1kl3DpdbiW3OANNDq6PA==' \
  --data-urlencode 'grant_type=Cert' \
  --data-urlencode 'client_id=dapr_cron_job' \
  --data-urlencode 'scope=mbi_api offline_access bensoft_api openid profile'`, certificatePath)

	output, err := runQuarkCLI(t,
		"--config", configDir,
		"import", "curl", command,
		"--collection", collectionID,
		"--name", "IAM token",
	)
	require.NoError(t, err, output)
	require.Contains(t, output, "Method:   POST")
	require.Contains(t, output, "URL:      https://access.dev.wealthcareadmin.com/access/connect/token")
	require.Contains(t, output, "mTLS:      P12 certificate "+certificatePath)
	require.Contains(t, output, `Saved as "IAM token"`)
	require.NotContains(t, output, "Password1")
	require.NotContains(t, output, "3n1kl3DpdbiW3OANNDq6PA")

	requests := loadCLIRequests(t, configDir, collectionID)
	require.Len(t, requests, 1)
	request := requests[0]
	require.Equal(t, "IAM token", request.Name)
	require.Equal(t, http.MethodPost, request.Method)
	require.Equal(t, "https://access.dev.wealthcareadmin.com/access/connect/token", request.URL)
	require.Equal(t,
		"user_id=3n1kl3DpdbiW3OANNDq6PA%3D%3D&grant_type=Cert&client_id=dapr_cron_job&scope=mbi_api%20offline_access%20bensoft_api%20openid%20profile",
		request.Body,
	)
	var headers http.Header
	require.NoError(t, json.Unmarshal([]byte(request.Headers), &headers))
	require.Equal(t, "application/x-www-form-urlencoded", headers.Get("Content-Type"))
	require.Equal(t, []string{"first", "second"}, headers.Values("X-Trace"))

	cfg, err := config.Load(configDir)
	require.NoError(t, err)
	require.Equal(t, []config.ClientCertificate{{
		Host:     "access.dev.wealthcareadmin.com",
		File:     certificatePath,
		Type:     "P12",
		Password: "Password1",
	}}, cfg.HTTP.ClientCertificates)
}

func TestCLIImportCurlEndToEndExecutesPersistedRequest(t *testing.T) {
	type receivedRequest struct {
		method  string
		headers []string
		body    string
		err     error
	}
	received := make(chan receivedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		body, readErr := io.ReadAll(r.Body)
		received <- receivedRequest{
			method:  r.Method,
			headers: r.Header.Values("X-Tag"),
			body:    string(body),
			err:     readErr,
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"executed":true}`))
	}))
	defer server.Close()

	configDir := t.TempDir()
	collectionID := seedCLICollection(t, configDir, "CLI E2E")
	command := fmt.Sprintf(
		`curl -H 'X-Tag: one' -H 'X-Tag: two' --data-urlencode 'q=two words' '%s/echo'`,
		server.URL,
	)

	output, err := runQuarkCLI(t,
		"--config", configDir,
		"import", "curl", command,
		"--collection", collectionID,
		"--name", "Imported request",
	)
	require.NoError(t, err, output)

	output, err = runQuarkCLI(t, "--config", configDir, "run", "CLI E2E/Imported request")
	require.NoError(t, err, output)
	require.Contains(t, output, "Status: 200 OK")
	require.Contains(t, output, `{"executed":true}`)

	select {
	case got := <-received:
		require.NoError(t, got.err)
		require.Equal(t, http.MethodPost, got.method)
		require.Equal(t, []string{"one", "two"}, got.headers)
		require.Equal(t, "q=two%20words", got.body)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the imported request to reach the HTTP server")
	}
}

func TestCLIImportCurlEndToEndRejectsUnsafeCommandsWithoutWrites(t *testing.T) {
	tests := map[string]string{
		"command substitution": `curl "https://example.com/$(whoami)"`,
		"pipeline":             `curl https://example.com | powershell -Command Get-ChildItem`,
		"file upload":          `curl --data-binary @secrets.txt https://example.com`,
	}
	for name, command := range tests {
		t.Run(name, func(t *testing.T) {
			configDir := t.TempDir()
			collectionID := seedCLICollection(t, configDir, "Rejected")
			output, err := runQuarkCLI(t,
				"--config", configDir,
				"import", "curl", command,
				"--collection", collectionID,
				"--name", "must not exist",
			)
			require.Error(t, err, output)
			require.Contains(t, output, "import curl: parse")
			require.Empty(t, loadCLIRequests(t, configDir, collectionID))
		})
	}
}

func TestCLIImportCurlEndToEndRollsBackWhenCertificateCannotLoad(t *testing.T) {
	configDir := t.TempDir()
	collectionID := seedCLICollection(t, configDir, "Rollback")
	missingCertificate := filepath.Join(configDir, "missing client.p12")
	command := fmt.Sprintf(
		`curl --cert-type P12 --cert '%s:literal-password' https://example.com`,
		missingCertificate,
	)

	output, err := runQuarkCLI(t,
		"--config", configDir,
		"import", "curl", command,
		"--collection", collectionID,
		"--name", "must not exist",
	)
	require.Error(t, err, output)
	require.Contains(t, output, "save mTLS configuration")
	require.NotContains(t, output, "literal-password")
	require.Empty(t, loadCLIRequests(t, configDir, collectionID))

	cfg, loadErr := config.Load(configDir)
	require.NoError(t, loadErr)
	require.Empty(t, cfg.HTTP.ClientCertificates)
}

func runQuarkCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	processArgs := append([]string{"-test.run=^$", "--"}, args...)
	cmd := osExec.Command(os.Args[0], processArgs...)
	cmd.Env = append(os.Environ(), cliE2EHelperEnv+"=1")
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func seedCLICollection(t *testing.T, configDir, name string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(configDir, 0o700))
	st, err := store.New(filepath.Join(configDir, "quark.db"))
	require.NoError(t, err)
	collection := &domain.Collection{Name: name}
	require.NoError(t, st.SaveCollection(context.Background(), collection))
	require.NoError(t, st.Close())
	return collection.ID
}

func loadCLIRequests(t *testing.T, configDir, collectionID string) []*domain.Request {
	t.Helper()
	st, err := store.New(filepath.Join(configDir, "quark.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	requests, err := st.ListRequests(context.Background(), collectionID)
	require.NoError(t, err)
	return requests
}

func writeCLITestPKCS12(t *testing.T, password string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(7),
		Subject:      pkix.Name{CommonName: "quark-cli-e2e"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	encoded, err := pkcs12.Encode(rand.Reader, key, &x509.Certificate{Raw: der}, nil, password)
	require.NoError(t, err)
	dir := filepath.Join(t.TempDir(), "certificates with spaces")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	path := filepath.Join(dir, "dev-auth 1.p12")
	require.NoError(t, os.WriteFile(path, encoded, 0o600))
	return path
}
