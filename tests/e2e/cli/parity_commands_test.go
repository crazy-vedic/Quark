//go:build e2e

package cli

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

func TestCLI_CollectionRenameAndDelete(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	out, stderr, code := runQuarkWithHome(t, home, "collection", "rename", colID, "Renamed API")
	require.Equal(t, 0, code, "%s", stderr)
	assert.Contains(t, out, `Renamed collection`)
	assert.Contains(t, out, `"Renamed API"`)

	out, stderr, code = runQuarkWithHome(t, home, "collection", "list")
	require.Equal(t, 0, code, "%s", stderr)
	assert.Contains(t, out, "Renamed API")

	out, stderr, code = runQuarkWithHome(t, home, "collection", "delete", "Renamed API")
	require.Equal(t, 0, code, "%s", stderr)
	assert.Contains(t, out, `Deleted collection "Renamed API"`)
	out, stderr, code = runQuarkWithHome(t, home, "collection", "list")
	require.Equal(t, 0, code, "%s", stderr)
	assert.Contains(t, out, "No collections found")
}

func TestCLI_RunPrintsBodyAndVerboseDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Reply", "present")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"created":true}`))
	}))
	defer server.Close()

	home, colID := setupHomeWithCollection(t)
	dbPath := filepath.Join(home, ".quark", "quark.db")
	st, err := store.New(dbPath, store.WithCacheSize(100))
	require.NoError(t, err)
	require.NoError(t, st.SaveRequest(context.Background(), &domain.Request{
		ID: uuid.NewString(), CollectionID: colID, Name: "Create", Method: http.MethodPost,
		URL: server.URL, Headers: `{"Content-Type":["application/json"]}`, Body: `{"id":1}`,
	}))
	require.NoError(t, st.Close())

	stdout, stderr, code := runQuarkWithHome(t, home, "run", "API/Create", "--verbose")
	require.Equal(t, 0, code, "stdout: %s\nstderr: %s", stdout, stderr)
	assert.Contains(t, stdout, "Status: 201 Created")
	assert.Contains(t, stdout, `"created": true`)
	assert.Contains(t, stderr, "Request: POST "+server.URL)
	assert.Contains(t, stderr, "Request headers:")
	assert.Contains(t, stderr, `Request body (8 bytes):`)
	assert.Contains(t, stderr, "< X-Reply: present")
}

func TestCLI_RunHandlesEmptyAndStreamedResponseBodies(t *testing.T) {
	const largeBodyBytes = (10 << 20) + 128
	largeBody := strings.Repeat("x", largeBodyBytes)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		if r.URL.Path == "/empty" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = w.Write([]byte(largeBody))
	}))
	defer server.Close()

	home, colID := setupHomeWithCollection(t)
	st, err := store.New(filepath.Join(home, ".quark", "quark.db"), store.WithCacheSize(100))
	require.NoError(t, err)
	for _, req := range []*domain.Request{
		{ID: uuid.NewString(), CollectionID: colID, Name: "Empty", Method: http.MethodGet, URL: server.URL + "/empty"},
		{ID: uuid.NewString(), CollectionID: colID, Name: "Large", Method: http.MethodGet, URL: server.URL + "/large"},
	} {
		require.NoError(t, st.SaveRequest(context.Background(), req))
	}
	require.NoError(t, st.Close())

	stdout, stderr, code := runQuarkWithHome(t, home, "run", "API/Empty")
	require.Equal(t, 0, code, "%s", stderr)
	assert.Contains(t, stdout, "Status: 204 No Content")
	assert.NotContains(t, stdout, "{}")

	stdout, stderr, code = runQuarkWithHome(t, home, "run", "API/Large")
	require.Equal(t, 0, code, "%s", stderr)
	assert.Contains(t, stdout, "Status: 200 OK")
	assert.GreaterOrEqual(t, len(stdout), largeBodyBytes)
	assert.True(t, strings.HasSuffix(stdout, largeBody))
}

func TestCLI_ClientCertificatesSetListAndDelete(t *testing.T) {
	home, _ := setupHomeWithCollection(t)
	configDir := filepath.Join(home, ".quark")
	caPath := filepath.Join(home, "root.pem")
	require.NoError(t, os.WriteFile(caPath, testCertificatePEM(t), 0600))

	out, stderr, code := runQuarkWithHome(t, home, "cert", "set", "API.Example", "--ca-file", caPath)
	require.Equal(t, 0, code, "%s", stderr)
	assert.Contains(t, out, "Saved client certificate for api.example")

	out, stderr, code = runQuarkWithHome(t, home, "cert", "list")
	require.Equal(t, 0, code, "%s", stderr)
	assert.Contains(t, out, "api.example")
	assert.Contains(t, out, "ca="+caPath)

	cfg, err := config.Load(configDir)
	require.NoError(t, err)
	require.Len(t, cfg.HTTP.ClientCertificates, 1)
	assert.Equal(t, "api.example", cfg.HTTP.ClientCertificates[0].Host)

	out, stderr, code = runQuarkWithHome(t, home, "cert", "delete", "API.EXAMPLE")
	require.Equal(t, 0, code, "%s", stderr)
	assert.Contains(t, out, "Deleted client certificate for api.example")
	out, stderr, code = runQuarkWithHome(t, home, "cert", "list")
	require.Equal(t, 0, code, "%s", stderr)
	assert.Contains(t, out, "No client certificates configured")
}

func testCertificatePEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	require.NoError(t, err)
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "quark e2e root"},
		NotBefore:    now.Add(-time.Minute), NotAfter: now.Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
