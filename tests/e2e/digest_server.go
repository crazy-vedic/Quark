//go:build e2e

package e2e

import (
	"crypto/md5" //nolint:gosec // RFC 7616 Digest auth still defines MD5; the server must compute it.
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// DigestRequest is one request received by a DigestServer.
type DigestRequest struct {
	Method string
	URI    string
	Body   string
	// Authorized is true when the request carried a Digest Authorization
	// header whose response matched the one this test server computes.
	Authorized bool
	// Params are the Authorization header's parameters, unquoted.
	Params map[string]string
}

// DigestServer is an httptest server that answers unauthenticated requests
// with the given WWW-Authenticate values and checks every Digest
// Authorization header against an RFC 7616 response computed here,
// independently of the production code.
type DigestServer struct {
	*httptest.Server

	mu       sync.Mutex
	requests []DigestRequest
}

// NewDigestServer starts a server that sends challenges (one WWW-Authenticate
// header per entry) with 401 to requests lacking an Authorization header.
// Digest responses are checked against username and password. The server is
// closed when the test ends.
func NewDigestServer(t testing.TB, username, password string, challenges ...string) *DigestServer {
	t.Helper()
	ds := &DigestServer{}
	ds.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		rec := DigestRequest{Method: r.Method, URI: r.URL.RequestURI(), Body: string(raw)}

		auth := r.Header.Get("Authorization")
		if auth == "" {
			ds.record(rec)
			for _, c := range challenges {
				w.Header().Add("WWW-Authenticate", c)
			}
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, "login required")
			return
		}

		rec.Params = parseDigestParams(auth)
		rec.Authorized = digestResponseMatches(rec.Params, username, password, r.Method, rec.URI)
		ds.record(rec)
		if !rec.Authorized {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, "bad digest")
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "authorized")
	}))
	t.Cleanup(ds.Close)
	return ds
}

func (ds *DigestServer) record(rec DigestRequest) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.requests = append(ds.requests, rec)
}

// Requests returns a copy of every request received, in order.
func (ds *DigestServer) Requests() []DigestRequest {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	return append([]DigestRequest(nil), ds.requests...)
}

var (
	digestParamPattern  = regexp.MustCompile(`([A-Za-z]+)=(?:"((?:[^"\\]|\\.)*)"|([^,\s]+))`)
	digestEscapePattern = regexp.MustCompile(`\\(.)`)
)

// parseDigestParams reads `Digest name="value", name=value` into a map.
// Quoted values are unescaped; bare tokens are taken as written.
func parseDigestParams(header string) map[string]string {
	out := make(map[string]string)
	for _, m := range digestParamPattern.FindAllStringSubmatch(header, -1) {
		value := m[2]
		if value == "" {
			value = m[3]
		}
		out[strings.ToLower(m[1])] = digestEscapePattern.ReplaceAllString(value, "$1")
	}
	return out
}

func digestHashFor(algorithm string) func() hash.Hash {
	if strings.HasPrefix(algorithm, "SHA-256") {
		return sha256.New
	}
	return md5.New
}

// digestResponseMatches recomputes the RFC 7616 response for the parameters
// the client sent and checks it, plus the username (hashed when userhash=true).
func digestResponseMatches(p map[string]string, username, password, method, uri string) bool {
	if p["uri"] != uri {
		return false
	}
	algorithm := strings.ToUpper(p["algorithm"])
	newHash := digestHashFor(algorithm)
	h := func(parts ...string) string {
		sum := newHash()
		_, _ = io.WriteString(sum, strings.Join(parts, ":"))
		return hex.EncodeToString(sum.Sum(nil))
	}

	wantUser := username
	if strings.EqualFold(p["userhash"], "true") {
		wantUser = h(username, p["realm"])
	}
	if p["username"] != wantUser {
		return false
	}

	ha1 := h(username, p["realm"], password)
	if strings.HasSuffix(algorithm, "-SESS") {
		ha1 = h(ha1, p["nonce"], p["cnonce"])
	}
	ha2 := h(method, uri)
	var want string
	if qop := p["qop"]; qop != "" {
		want = h(ha1, p["nonce"], p["nc"], p["cnonce"], qop, ha2)
	} else {
		want = h(ha1, p["nonce"], ha2)
	}
	return p["response"] == want
}
