// Package transport builds Quark's HTTP transport, including optional mTLS.
package transport

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

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
	// variants holds per-request transports, keyed by route and settings, so
	// requests with the same proxy/TLS/timeout share a connection pool.
	variants map[variantKey]*http.Transport
}

// variantKey identifies one shared per-request transport.
type variantKey struct {
	route         string // certificate host, or "" for the base transport
	proxy         string
	insecure      bool
	headerTimeout time.Duration
}

// New creates a transport manager and loads all configured certificates.
func New(cfg config.Config) (*Manager, error) {
	// Start from net/http's defaults so HTTP(S)_PROXY/NO_PROXY, the dial and
	// TLS handshake timeouts, idle-connection limits and HTTP/2 all apply.
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("transport: http.DefaultTransport is %T", http.DefaultTransport)
	}
	base = base.Clone()
	base.ResponseHeaderTimeout = cfg.Timeout()
	m := &Manager{base: base}
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
		// Client-certificate hosts stay on HTTP/1.1, as before: the TLS config
		// allows renegotiation, which HTTP/2 forbids.
		clone.ForceAttemptHTTP2 = false
		clone.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
		transports[host] = clone
	}
	m.mu.Lock()
	previous := m.transports
	previousVariants := m.variants
	m.transports = transports
	m.variants = make(map[variantKey]*http.Transport)
	m.mu.Unlock()
	// Requests in flight on the old routes finish normally; only idle sockets close.
	for _, rt := range previous {
		if rt != nil {
			rt.CloseIdleConnections()
		}
	}
	for _, rt := range previousVariants {
		rt.CloseIdleConnections()
	}
	return nil
}

// Variant returns a RoundTripper for one request whose proxy, TLS verification
// or response-header timeout differ from the defaults. Each host keeps its
// configured client certificate, so only the overridden settings change.
func (m *Manager) Variant(
	proxy *url.URL,
	skipTLSVerify bool,
	responseHeaderTimeout time.Duration,
) http.RoundTripper {
	return variantRoundTripper{
		m:             m,
		proxy:         proxy,
		insecure:      skipTLSVerify,
		headerTimeout: responseHeaderTimeout,
	}
}

type variantRoundTripper struct {
	m             *Manager
	proxy         *url.URL
	insecure      bool
	headerTimeout time.Duration
}

// RoundTrip implements http.RoundTripper.
func (v variantRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	host := ""
	if req.URL != nil {
		host = strings.ToLower(req.URL.Hostname())
	}
	return v.m.variantTransport(host, v).RoundTrip(req)
}

// variantTransport returns the shared transport for a host and settings,
// creating it on first use from the host's route (or the base transport).
func (m *Manager) variantTransport(host string, v variantRoundTripper) *http.Transport {
	key := variantKey{insecure: v.insecure, headerTimeout: v.headerTimeout}
	if v.proxy != nil {
		key.proxy = v.proxy.String()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	source := m.base
	if routed := m.transports[host]; routed != nil {
		source = routed
		key.route = host
	}
	if shared, ok := m.variants[key]; ok {
		return shared
	}
	clone := source.Clone()
	if v.proxy != nil {
		clone.Proxy = http.ProxyURL(v.proxy)
	}
	if v.insecure {
		// Explicitly requested for one request (curl -k); the client
		// certificate and CA settings of the host route are kept.
		cfg := &tls.Config{MinVersion: tls.VersionTLS12}
		if clone.TLSClientConfig != nil {
			cfg = clone.TLSClientConfig.Clone()
		}
		cfg.InsecureSkipVerify = true //nolint:gosec // requested per request by the user
		clone.TLSClientConfig = cfg
	}
	clone.ResponseHeaderTimeout = v.headerTimeout
	if m.variants == nil {
		m.variants = make(map[variantKey]*http.Transport)
	}
	m.variants[key] = clone
	return clone
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
			return tls.Certificate{}, fmt.Errorf(
				"password environment variable %q is empty",
				item.PasswordEnv,
			)
		}
	}
	privateKey, certificate, chain, err := pkcs12.DecodeChain(data, password)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("decode PKCS#12: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw})
	for _, ca := range chain {
		certPEM = append(
			certPEM,
			pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Raw})...)
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
