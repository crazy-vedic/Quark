// Package transport builds Quark's HTTP transport, including optional mTLS.
package transport

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"software.sslmate.com/src/go-pkcs12"

	"github.com/crazy-vedic/quark/internal/config"
)

// Manager is a concurrency-safe host router for Quark's HTTP requests.
// Certificates are loaded when the manager is created or reloaded, not per
// request. Hosts are matched case-insensitively and without their port.
type Manager struct {
	mu         sync.RWMutex
	base       *http.Transport
	transports map[string]*http.Transport
}

// New creates a transport manager and loads all configured certificates.
func New(cfg config.Config) (*Manager, error) {
	m := &Manager{base: &http.Transport{ResponseHeaderTimeout: cfg.Timeout()}}
	if err := m.Reload(cfg); err != nil {
		return nil, err
	}
	return m, nil
}

// Reload replaces the configured certificate routes atomically.
func (m *Manager) Reload(cfg config.Config) error {
	transports := make(map[string]*http.Transport, len(cfg.HTTP.ClientCertificates))
	for _, item := range cfg.HTTP.ClientCertificates {
		host := strings.ToLower(strings.TrimSpace(item.Host))
		if host == "" {
			return fmt.Errorf("mTLS certificate host is required")
		}
		if _, exists := transports[host]; exists {
			return fmt.Errorf("duplicate mTLS certificate host %q", host)
		}
		// Reserve the host before doing filesystem/PKCS#12 work so duplicate
		// configuration is reported deterministically.
		transports[host] = nil
	}
	for _, item := range cfg.HTTP.ClientCertificates {
		host := strings.ToLower(strings.TrimSpace(item.Host))
		tlsConfig, err := loadTLSConfig(item)
		if err != nil {
			return fmt.Errorf("load TLS configuration for %q: %w", host, err)
		}
		clone := m.base.Clone()
		clone.TLSClientConfig = tlsConfig
		transports[host] = clone
	}
	m.mu.Lock()
	m.transports = transports
	m.mu.Unlock()
	return nil
}

// RoundTrip implements http.RoundTripper.
func (m *Manager) RoundTrip(req *http.Request) (*http.Response, error) {
	host := ""
	if req.URL != nil {
		host = strings.ToLower(req.URL.Hostname())
	}
	m.mu.RLock()
	rt := http.RoundTripper(m.base)
	if configured, ok := m.transports[host]; ok {
		rt = configured
	}
	m.mu.RUnlock()
	return rt.RoundTrip(req)
}

func loadTLSConfig(item config.ClientCertificate) (*tls.Config, error) {
	item.File = platformPath(item.File)
	item.KeyFile = platformPath(item.KeyFile)
	item.CAFile = platformPath(item.CAFile)
	tlsConfig := &tls.Config{}
	if item.File != "" {
		certType := strings.ToUpper(strings.TrimSpace(item.Type))
		if certType == "" {
			lower := strings.ToLower(item.File)
			if strings.HasSuffix(lower, ".p12") || strings.HasSuffix(lower, ".pfx") {
				certType = "P12"
			} else {
				certType = "PEM"
			}
		}
		var (
			cert tls.Certificate
			err  error
		)
		switch certType {
		case "P12":
			cert, err = loadPKCS12(item)
		case "PEM":
			cert, err = loadPEM(item)
		default:
			return nil, fmt.Errorf("unsupported certificate type %q", certType)
		}
		if err != nil {
			return nil, err
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
		// Preserve compatibility with the existing IAM endpoint, which requests
		// legacy TLS renegotiation after the client-certificate handshake.
		tlsConfig.Renegotiation = tls.RenegotiateOnceAsClient
	} else if item.KeyFile != "" {
		return nil, fmt.Errorf("certificate file is required when key_file is set")
	}
	if item.CAFile != "" {
		pool, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("load system CA pool: %w", err)
		}
		caPEM, err := os.ReadFile(item.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read CA file: %w", err)
		}
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("CA file does not contain a valid PEM certificate")
		}
		tlsConfig.RootCAs = pool
	}
	if len(tlsConfig.Certificates) == 0 && tlsConfig.RootCAs == nil {
		return nil, fmt.Errorf("certificate file or CA file is required")
	}
	return tlsConfig, nil
}

func platformPath(value string) string {
	if runtime.GOOS != "windows" || value == "" {
		return value
	}
	normalized := strings.ReplaceAll(value, "\\", "/")
	var drive string
	var rest string
	switch {
	case len(normalized) >= 7 && strings.HasPrefix(normalized, "/mnt/") && normalized[6] == '/':
		drive = normalized[5:6]
		rest = normalized[7:]
	case len(normalized) >= 4 && normalized[0] == '/' && normalized[2] == '/':
		drive = normalized[1:2]
		rest = normalized[3:]
	default:
		return value
	}
	if (drive[0] < 'a' || drive[0] > 'z') && (drive[0] < 'A' || drive[0] > 'Z') {
		return value
	}
	return strings.ToUpper(drive) + ":\\" + filepath.FromSlash(rest)
}

func loadPEM(item config.ClientCertificate) (tls.Certificate, error) {
	if item.Password != "" || item.PasswordEnv != "" {
		return tls.Certificate{}, fmt.Errorf("encrypted PEM private keys are not supported")
	}
	keyFile := item.KeyFile
	if keyFile == "" {
		keyFile = item.File
	}
	cert, err := tls.LoadX509KeyPair(item.File, keyFile)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("load PEM key pair: %w", err)
	}
	return cert, nil
}

func loadPKCS12(item config.ClientCertificate) (tls.Certificate, error) {
	if item.File == "" {
		return tls.Certificate{}, fmt.Errorf("certificate file is required")
	}
	data, err := os.ReadFile(item.File)
	if err != nil {
		return tls.Certificate{}, err
	}
	password := item.Password
	if item.PasswordEnv != "" {
		password = os.Getenv(item.PasswordEnv)
		if password == "" {
			return tls.Certificate{}, fmt.Errorf("password environment variable %q is empty", item.PasswordEnv)
		}
	}
	privateKey, certificate, chain, err := pkcs12.DecodeChain(data, password)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("decode PKCS#12: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw})
	for _, ca := range chain {
		certPEM = append(certPEM, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Raw})...)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("marshal private key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("build TLS key pair: %w", err)
	}
	return cert, nil
}
