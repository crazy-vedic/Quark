package transport

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"software.sslmate.com/src/go-pkcs12"

	"github.com/crazy-vedic/quark/internal/config"
)

func TestPlatformPathConvertsWSLAndGitBashPathsOnWindows(t *testing.T) {
	if os.PathSeparator != '\\' {
		t.Skip("Windows path conversion")
	}
	require.Equal(t, `C:\Users\me\client.p12`, platformPath(`/mnt/c/Users/me/client.p12`))
	require.Equal(t, `D:\certs\root.pem`, platformPath(`/d/certs/root.pem`))
}

func TestNewLoadsPKCS12ChainAndRoutesByHost(t *testing.T) {
	p12Path := writeTestPKCS12(t, "secret")
	cfg := config.Default(t.TempDir())
	cfg.HTTP.ClientCertificates = []config.ClientCertificate{{
		Host: "ACCESS.DEV.EXAMPLE.COM", File: p12Path, Password: "secret",
	}}

	m, err := New(cfg)
	require.NoError(t, err)
	require.Len(t, m.transports, 1)
	require.NotNil(t, m.transports["access.dev.example.com"].TLSClientConfig)
	require.Len(t, m.transports["access.dev.example.com"].TLSClientConfig.Certificates, 1)
	require.Equal(t, tls.RenegotiateOnceAsClient,
		m.transports["access.dev.example.com"].TLSClientConfig.Renegotiation)
}

func TestNewReadsPasswordFromEnvironment(t *testing.T) {
	p12Path := writeTestPKCS12(t, "from-env")
	cfg := config.Default(t.TempDir())
	cfg.HTTP.ClientCertificates = []config.ClientCertificate{{
		Host: "api.example.com", File: p12Path, PasswordEnv: "QUARK_TEST_CERT_PASSWORD",
	}}

	_, err := New(cfg)
	require.ErrorContains(t, err, "password environment variable")
	t.Setenv("QUARK_TEST_CERT_PASSWORD", "from-env")
	_, err = New(cfg)
	require.NoError(t, err)
}

func TestNewRejectsBadPassword(t *testing.T) {
	p12Path := writeTestPKCS12(t, "correct")
	cfg := config.Default(t.TempDir())
	cfg.HTTP.ClientCertificates = []config.ClientCertificate{{
		Host: "api.example.com", File: p12Path, Password: "wrong",
	}}

	_, err := New(cfg)
	require.ErrorContains(t, err, "decode PKCS#12")
}

func TestNewLoadsPEMKeyAndCustomCA(t *testing.T) {
	certPath, keyPath := writeTestPEM(t)
	cfg := config.Default(t.TempDir())
	cfg.HTTP.ClientCertificates = []config.ClientCertificate{{
		Host: "pem.example.com", File: certPath, Type: "PEM", KeyFile: keyPath, CAFile: certPath,
	}}

	m, err := New(cfg)
	require.NoError(t, err)
	tlsConfig := m.transports["pem.example.com"].TLSClientConfig
	require.Len(t, tlsConfig.Certificates, 1)
	require.NotNil(t, tlsConfig.RootCAs)
}

func TestNewRejectsUnsupportedCertificateType(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.HTTP.ClientCertificates = []config.ClientCertificate{{
		Host: "api.example.com", File: "client.der", Type: "DER",
	}}

	_, err := New(cfg)
	require.ErrorContains(t, err, "unsupported certificate type")
}

func writeTestPKCS12(t *testing.T, password string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "quark-test-client"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	encoded, err := pkcs12.Encode(rand.Reader, key, &x509.Certificate{Raw: der}, nil, password)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "client.p12")
	require.NoError(t, os.WriteFile(path, encoded, 0o600))
	return path
}

func writeTestPEM(t *testing.T) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: "quark-test-pem"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	dir := t.TempDir()
	certPath := filepath.Join(dir, "client.pem")
	keyPath := filepath.Join(dir, "client-key.pem")
	require.NoError(t, os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	require.NoError(t, os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600))
	return certPath, keyPath
}

func TestReloadRejectsDuplicateHosts(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.HTTP.ClientCertificates = []config.ClientCertificate{
		{Host: "api.example.com"},
		{Host: "API.EXAMPLE.COM"},
	}

	_, err := New(cfg)
	require.ErrorContains(t, err, "duplicate mTLS certificate host")
}
