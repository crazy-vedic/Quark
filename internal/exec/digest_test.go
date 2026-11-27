package exec

import (
	"context"
	"crypto/md5" //nolint:gosec // test server mirrors RFC 7616 MD5.
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

// RFC 7616 §3.9.1 example values.
const (
	rfcRealm  = "http-auth@example.org"
	rfcNonce  = "7ypf/xlj9XXwfDPEoM4URrv/xwf94BcCAzFZH4GiTo0v"
	rfcOpaque = "FQhe/qaU925kfnzjCev0ciny7QMkPqMAFRtzCUYo5tdS"
	rfcCnonce = "f2/wE4q74E6zIJEtWaHKaf5wv/H5QzzpXusqGemxURZJ"
)

func TestDigestAuthorization_RFC7616Vectors(t *testing.T) {
	cases := []struct {
		algorithm string
		response  string
	}{
		{"MD5", "8ca523f5e9506fed4657c9700eebdbec"},
		{"SHA-256", "753927fa0e85d155564e2e272a28d1802ca10daf4496794697cf8db5856cb6c1"},
	}
	for _, tc := range cases {
		t.Run(tc.algorithm, func(t *testing.T) {
			ch := digestChallenge{
				realm: rfcRealm, nonce: rfcNonce, opaque: rfcOpaque,
				algorithm: tc.algorithm, qop: []string{"auth", "auth-int"},
			}
			got, err := digestAuthorization(
				ch, "Mufasa", "Circle of Life", "GET", "/dir/index.html", rfcCnonce,
			)
			require.NoError(t, err)
			fields := parseAuthParams(strings.TrimPrefix(got, "Digest "))
			assert.Equal(t, tc.response, fields["response"])
			assert.Equal(t, "Mufasa", fields["username"])
			assert.Equal(t, "auth", fields["qop"])
			assert.Equal(t, "00000001", fields["nc"])
			assert.Equal(t, rfcOpaque, fields["opaque"])
			assert.Equal(t, tc.algorithm, fields["algorithm"])
			assert.Equal(t, "/dir/index.html", fields["uri"])
		})
	}
}

func TestDigestAuthorization_NoQopAndSessAndUserhash(t *testing.T) {
	// RFC 2069 form: no qop, so no nc/cnonce in the header.
	got, err := digestAuthorization(
		digestChallenge{realm: "r", nonce: "n", algorithm: "MD5"},
		"u", "p", "GET", "/", "c",
	)
	require.NoError(t, err)
	fields := parseAuthParams(strings.TrimPrefix(got, "Digest "))
	ha1 := hexHash(md5.New, "u:r:p")
	ha2 := hexHash(md5.New, "GET:/")
	assert.Equal(t, hexHash(md5.New, ha1+":n:"+ha2), fields["response"])
	assert.NotContains(t, got, "cnonce")

	got, err = digestAuthorization(
		digestChallenge{
			realm: "r", nonce: "n", algorithm: "SHA-256-SESS",
			qop: []string{"auth"}, userhash: true,
		},
		"u", "p", "GET", "/", "c",
	)
	require.NoError(t, err)
	fields = parseAuthParams(strings.TrimPrefix(got, "Digest "))
	ha1 = hexHash(sha256.New, hexHash(sha256.New, "u:r:p")+":n:c")
	ha2 = hexHash(sha256.New, "GET:/")
	assert.Equal(t, hexHash(sha256.New, ha1+":n:00000001:c:auth:"+ha2), fields["response"])
	assert.Equal(t, hexHash(sha256.New, "u:r"), fields["username"])
	assert.Contains(t, got, "userhash=true")
}

func TestDigestAuthorization_Unsupported(t *testing.T) {
	_, err := digestAuthorization(
		digestChallenge{nonce: "n", algorithm: "SHA-512-256"}, "u", "p", "GET", "/", "c",
	)
	require.ErrorIs(t, err, errDigestUnsupported)
	_, err = digestAuthorization(
		digestChallenge{nonce: "n", algorithm: "MD5", qop: []string{"auth-int"}},
		"u", "p", "GET", "/", "c",
	)
	require.ErrorIs(t, err, errDigestUnsupported)
}

func TestPickDigestChallenge(t *testing.T) {
	ch, ok := pickDigestChallenge([]string{
		`Basic realm="x"`,
		`Digest realm="a", nonce="n1", qop="auth", algorithm=MD5, ` +
			`Digest realm="a", nonce="n2", qop="auth", algorithm=SHA-256`,
	})
	require.True(t, ok)
	assert.Equal(t, "SHA-256", ch.algorithm)
	assert.Equal(t, "n2", ch.nonce)
	assert.Equal(t, []string{"auth"}, ch.qop)

	ch, ok = pickDigestChallenge([]string{`Digest realm="a \"q\"", nonce="n"`})
	require.True(t, ok)
	assert.Equal(t, "MD5", ch.algorithm, "algorithm defaults to MD5")
	assert.Equal(t, `a "q"`, ch.realm)

	_, ok = pickDigestChallenge([]string{`Basic realm="x"`})
	assert.False(t, ok)
	_, ok = pickDigestChallenge([]string{`Digest realm="x"`})
	assert.False(t, ok, "a challenge without a nonce cannot be answered")
	_, ok = pickDigestChallenge([]string{`Digest nonce="n", algorithm=SHA-512-256`})
	assert.False(t, ok)
}

func hexHash(newHash func() hash.Hash, s string) string {
	h := newHash()
	_, _ = io.WriteString(h, s)
	return hex.EncodeToString(h.Sum(nil))
}

// digestServer answers with a SHA-256 Digest challenge until a request
// carries a valid Authorization header for user/pass.
func digestServer(t *testing.T, hits *atomic.Int32, wantBody string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		body, _ := io.ReadAll(r.Body)
		assert.Equal(t, wantBody, string(body), "body is sent on every attempt")
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Digest ") {
			w.Header().Set("WWW-Authenticate",
				`Digest realm="quark", nonce="abc", qop="auth", algorithm=SHA-256, opaque="op"`)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, "challenge")
			return
		}
		f := parseAuthParams(strings.TrimPrefix(auth, "Digest "))
		ha1 := hexHash(sha256.New, "user:quark:pass")
		ha2 := hexHash(sha256.New, r.Method+":"+r.URL.RequestURI())
		want := hexHash(sha256.New,
			ha1+":abc:"+f["nc"]+":"+f["cnonce"]+":auth:"+ha2)
		if f["response"] != want || f["uri"] != r.URL.RequestURI() || f["opaque"] != "op" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
}

func TestExecute_DigestHandshake(t *testing.T) {
	var hits atomic.Int32
	srv := digestServer(t, &hits, `{"a":1}`)
	defer srv.Close()

	e := New(srv.Client().Transport)
	res, err := e.Execute(context.Background(), &domain.Request{
		Method:   http.MethodPost,
		URL:      srv.URL + "/dir/index.html?x=1",
		Body:     `{"a":1}`,
		AuthType: domain.AuthTypeDigest,
		AuthConfig: domain.MustAuthConfigJSON(
			domain.AuthConfig{Username: "user", Password: "pass"},
		),
	})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "ok", string(res.Body))
	assert.Equal(t, int32(2), hits.Load())
}

func TestExecute_DigestWrongPasswordShowsServerResponse(t *testing.T) {
	var hits atomic.Int32
	srv := digestServer(t, &hits, "")
	defer srv.Close()

	e := New(srv.Client().Transport)
	res, err := e.Execute(context.Background(), &domain.Request{
		Method:     http.MethodGet,
		URL:        srv.URL,
		AuthType:   domain.AuthTypeDigest,
		AuthConfig: domain.MustAuthConfigJSON(domain.AuthConfig{Username: "user", Password: "bad"}),
	})
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, res.StatusCode)
	assert.Equal(t, int32(2), hits.Load(), "retried exactly once")
}

func TestExecute_Digest401WithoutChallengeIsReturned(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("WWW-Authenticate", `Basic realm="x"`)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "nope")
	}))
	defer srv.Close()

	e := New(srv.Client().Transport)
	res, err := e.Execute(context.Background(), &domain.Request{
		Method:     http.MethodGet,
		URL:        srv.URL,
		AuthType:   domain.AuthTypeDigest,
		AuthConfig: domain.MustAuthConfigJSON(domain.AuthConfig{Username: "user"}),
	})
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
	assert.Equal(t, "nope", string(res.Body))
	assert.Equal(t, int32(1), hits.Load())
}

func TestExecute_DigestUnsupportedChallengeIsReturned(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Digest realm="x", nonce="n", qop="auth-int"`)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "challenge")
	}))
	defer srv.Close()

	e := New(srv.Client().Transport)
	res, err := e.Execute(context.Background(), &domain.Request{
		Method:     http.MethodGet,
		URL:        srv.URL,
		AuthType:   domain.AuthTypeDigest,
		AuthConfig: domain.MustAuthConfigJSON(domain.AuthConfig{Username: "user"}),
	})
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
	assert.Equal(t, "challenge", string(res.Body))
}

func TestExecute_DigestRequiresCredentials(t *testing.T) {
	e := New(http.DefaultTransport)
	_, err := e.Execute(context.Background(), &domain.Request{
		Method:   http.MethodGet,
		URL:      "http://127.0.0.1:1",
		AuthType: domain.AuthTypeDigest,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "digest username or password is required")
}
