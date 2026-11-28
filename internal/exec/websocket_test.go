package exec_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
)

// wsServer accepts WebSocket upgrades and hands each connection to handler.
func wsServer(
	t *testing.T,
	handler func(context.Context, *websocket.Conn, *http.Request),
) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.CloseNow() }()
		handler(r.Context(), c, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func wsURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func echoHandler(ctx context.Context, c *websocket.Conn, r *http.Request) {
	_ = c.Write(ctx, websocket.MessageText, []byte("token="+r.Header.Get("X-Token")))
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		_ = c.Write(ctx, typ, append([]byte("echo: "), data...))
	}
}

func TestIsWebSocketURL(t *testing.T) {
	assert.True(t, exec.IsWebSocketURL("ws://example.test/socket"))
	assert.True(t, exec.IsWebSocketURL("wss://example.test/socket"))
	assert.True(t, exec.IsWebSocketURL("WSS://example.test/socket"))
	assert.False(t, exec.IsWebSocketURL("https://example.test/"))
	assert.False(t, exec.IsWebSocketURL("example.test/socket"))
	assert.False(t, exec.IsWebSocketURL("{{base}}/socket"))
	assert.False(t, exec.IsWebSocketURL("://bad"))
}

func TestOpenWebSocket_HeadersSendAndReceive(t *testing.T) {
	srv := wsServer(t, echoHandler)
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	e := exec.New(transport, exec.WithTimeout(5*time.Second))
	session, err := e.OpenWebSocket(context.Background(), &domain.Request{
		Method:  "GET",
		URL:     wsURL(srv),
		Headers: `{"X-Token":"abc"}`,
	})
	require.NoError(t, err)
	assert.Equal(t, "101 Switching Protocols", session.Status())
	assert.NotEmpty(t, session.Headers().Get("Sec-Websocket-Accept"))

	msg, err := session.Receive()
	require.NoError(t, err)
	assert.Equal(t, "token=abc", string(msg.Data))
	assert.False(t, msg.Binary)
	assert.False(t, msg.At.IsZero())

	require.NoError(t, session.SendText(context.Background(), "ping"))
	msg, err = session.Receive()
	require.NoError(t, err)
	assert.Equal(t, "echo: ping", string(msg.Data))

	require.NoError(t, session.Close(1000, "bye"))
	_, err = session.Receive()
	require.ErrorIs(t, err, exec.ErrWebSocketClosed)
	require.NoError(t, session.Close(1000, "bye"), "a second Close is a no-op")
}

func TestOpenWebSocket_PeerCloseReportsCodeAndReason(t *testing.T) {
	srv := wsServer(t, func(ctx context.Context, c *websocket.Conn, _ *http.Request) {
		_ = c.Write(ctx, websocket.MessageText, []byte("last"))
		_ = c.Close(4000, "done")
	})
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	session, err := exec.New(transport).OpenWebSocket(context.Background(),
		&domain.Request{Method: "GET", URL: wsURL(srv)})
	require.NoError(t, err)
	defer func() { _ = session.Close(1000, "") }()

	msg, err := session.Receive()
	require.NoError(t, err)
	assert.Equal(t, "last", string(msg.Data))

	_, err = session.Receive()
	require.ErrorIs(t, err, exec.ErrWebSocketClosed)
	assert.Contains(t, err.Error(), "4000")
	assert.Contains(t, err.Error(), "done")
}

func TestOpenWebSocket_BinaryMessageIsFlagged(t *testing.T) {
	srv := wsServer(t, func(ctx context.Context, c *websocket.Conn, _ *http.Request) {
		_ = c.Write(ctx, websocket.MessageBinary, []byte{0x00, 0xff})
		_, _, _ = c.Read(ctx) // returns when the client closes
	})
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	session, err := exec.New(transport).OpenWebSocket(context.Background(),
		&domain.Request{Method: "GET", URL: wsURL(srv)})
	require.NoError(t, err)
	defer func() { _ = session.Close(1000, "") }()

	msg, err := session.Receive()
	require.NoError(t, err)
	assert.True(t, msg.Binary)
	assert.Equal(t, []byte{0x00, 0xff}, msg.Data)
}

func TestOpenWebSocket_LocalCloseUnblocksReceive(t *testing.T) {
	srv := wsServer(t, func(ctx context.Context, c *websocket.Conn, _ *http.Request) {
		for {
			if _, _, err := c.Read(ctx); err != nil {
				return
			}
		}
	})
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	session, err := exec.New(transport).OpenWebSocket(context.Background(),
		&domain.Request{Method: "GET", URL: wsURL(srv)})
	require.NoError(t, err)

	received := make(chan error, 1)
	go func() {
		_, err := session.Receive()
		received <- err
	}()
	time.Sleep(20 * time.Millisecond)
	require.NoError(t, session.Close(1000, "leaving"))

	select {
	case err := <-received:
		require.ErrorIs(t, err, exec.ErrWebSocketClosed)
	case <-time.After(5 * time.Second):
		t.Fatal("Receive stayed blocked after Close")
	}
}

func TestOpenWebSocket_SendAfterCloseFails(t *testing.T) {
	srv := wsServer(t, echoHandler)
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	session, err := exec.New(transport).OpenWebSocket(context.Background(),
		&domain.Request{Method: "GET", URL: wsURL(srv)})
	require.NoError(t, err)
	require.NoError(t, session.Close(1000, ""))
	err = session.SendText(context.Background(), "late")
	require.ErrorIs(t, err, exec.ErrWebSocketClosed)
}

func TestOpenWebSocket_RejectsNonWebSocketScheme(t *testing.T) {
	e := exec.New(&http.Transport{})
	_, err := e.OpenWebSocket(context.Background(),
		&domain.Request{Method: "GET", URL: "http://example.test/socket"})
	require.ErrorIs(t, err, exec.ErrInvalidURL)
}

func TestOpenWebSocket_RejectsBody(t *testing.T) {
	e := exec.New(&http.Transport{})
	_, err := e.OpenWebSocket(context.Background(),
		&domain.Request{Method: "GET", URL: "ws://example.test/socket", Body: "{}"})
	require.ErrorContains(t, err, "request body")
}

func TestOpenWebSocket_InterpolatesVariables(t *testing.T) {
	srv := wsServer(t, echoHandler)
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()
	host := strings.TrimPrefix(srv.URL, "http://")

	e := exec.New(transport, exec.WithVariableResolver(
		func(string) (map[string]string, map[string]string, error) {
			return map[string]string{"host": host}, nil, nil
		}))
	session, err := e.OpenWebSocket(context.Background(), &domain.Request{
		Method:       "GET",
		URL:          "ws://{{host}}/socket",
		CollectionID: "col-1",
	})
	require.NoError(t, err)
	defer func() { _ = session.Close(1000, "") }()
	msg, err := session.Receive()
	require.NoError(t, err)
	assert.Equal(t, "token=", string(msg.Data))
}

func TestOpenWebSocket_HandshakeRejectionIsNotAClose(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	_, err := exec.New(transport).OpenWebSocket(context.Background(),
		&domain.Request{Method: "GET", URL: wsURL(srv)})
	require.Error(t, err)
	assert.False(t, errors.Is(err, exec.ErrWebSocketClosed))
	assert.False(t, errors.Is(err, exec.ErrTimeout))
}

func TestOpenWebSocket_HandshakeTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	_, err := exec.New(transport, exec.WithTimeout(50*time.Millisecond)).OpenWebSocket(
		context.Background(), &domain.Request{Method: "GET", URL: wsURL(srv)})
	require.ErrorIs(t, err, exec.ErrTimeout)
}

func TestOpenWebSocket_RecordsHandshakeInHistory(t *testing.T) {
	srv := wsServer(t, echoHandler)
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()
	writer := &recordingExecutionWriter{}

	session, err := exec.New(transport, exec.WithExecutionWriter(writer)).OpenWebSocket(
		context.Background(),
		&domain.Request{ID: "r1", Method: "GET", URL: wsURL(srv)})
	require.NoError(t, err)
	defer func() { _ = session.Close(1000, "") }()

	require.Len(t, writer.executions, 1)
	assert.Equal(t, http.StatusSwitchingProtocols, writer.executions[0].StatusCode)
	assert.Equal(t, "r1", writer.executions[0].RequestID)
}

// selfSignedTLS returns a certificate that is valid as both server and client
// leaf, and trusts itself when placed in a pool.
func selfSignedTLS(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "quark websocket test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
			x509.ExtKeyUsageClientAuth,
		},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	leaf, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, pool
}

func TestOpenWebSocket_ClientCertificateOverWSS(t *testing.T) {
	cert, pool := selfSignedTLS(t)
	handler := func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.CloseNow() }()
		echoHandler(r.Context(), c, r)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(handler))
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
	}
	srv.StartTLS()
	defer srv.Close()
	url := "wss" + strings.TrimPrefix(srv.URL, "https")

	withCert := &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs:      pool,
		Certificates: []tls.Certificate{cert},
	}}
	defer withCert.CloseIdleConnections()
	session, err := exec.New(withCert).OpenWebSocket(context.Background(),
		&domain.Request{Method: "GET", URL: url, Headers: `{"X-Token":"mtls"}`})
	require.NoError(t, err)
	msg, err := session.Receive()
	require.NoError(t, err)
	assert.Equal(t, "token=mtls", string(msg.Data))
	require.NoError(t, session.Close(1000, ""))

	withoutCert := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}
	defer withoutCert.CloseIdleConnections()
	_, err = exec.New(withoutCert).OpenWebSocket(context.Background(),
		&domain.Request{Method: "GET", URL: url})
	require.Error(t, err, "the server requires a client certificate")
}
