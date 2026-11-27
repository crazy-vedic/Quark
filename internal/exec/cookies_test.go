package exec_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/exec"
)

// cookieEchoServer sets sid=abc on /set and echoes the Cookie header it
// received in an X-Seen-Cookie response header on every other path.
func cookieEchoServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/set" {
			// Plain-HTTP test server: Secure/HttpOnly would stop the jar sending it.
			cookie := &http.Cookie{ //nolint:gosec // test fixture
				Name:  "sid",
				Value: "abc",
				Path:  "/",
			}
			http.SetCookie(w, cookie)
			return
		}
		w.Header().Set("X-Seen-Cookie", r.Header.Get("Cookie"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSessionJar_CarriesCookiesBetweenRequestsUntilCleared(t *testing.T) {
	srv := cookieEchoServer(t)
	e := exec.New(http.DefaultTransport, exec.WithCookieJar(exec.NewSessionJar()))
	ctx := context.Background()

	_, err := e.Execute(ctx, newTestRequest("GET", srv.URL+"/set", ""))
	require.NoError(t, err)

	res, err := e.Execute(ctx, newTestRequest("GET", srv.URL+"/check", ""))
	require.NoError(t, err)
	assert.Equal(t, "sid=abc", http.Header(res.Headers).Get("X-Seen-Cookie"))

	assert.True(t, e.ClearSessionCookies(), "a session jar is configured, so clearing applies")

	res, err = e.Execute(ctx, newTestRequest("GET", srv.URL+"/check", ""))
	require.NoError(t, err)
	assert.Empty(t, http.Header(res.Headers).Get("X-Seen-Cookie"),
		"cleared jar must not send old cookies")
}

func TestWithoutJar_NoCookiesAreStoredOrSent(t *testing.T) {
	srv := cookieEchoServer(t)
	e := exec.New(http.DefaultTransport)

	_, err := e.Execute(context.Background(), newTestRequest("GET", srv.URL+"/set", ""))
	require.NoError(t, err)

	res, err := e.Execute(context.Background(), newTestRequest("GET", srv.URL+"/check", ""))
	require.NoError(t, err)
	assert.Empty(t, http.Header(res.Headers).Get("X-Seen-Cookie"))
	assert.False(t, e.ClearSessionCookies(), "no jar configured: nothing to clear")
}

func TestSessionJar_ClearEmptiesJar(t *testing.T) {
	jar := exec.NewSessionJar()
	u, err := url.Parse("http://example.test/")
	require.NoError(t, err)
	jar.SetCookies(u, []*http.Cookie{{Name: "a", Value: "1"}}) //nolint:gosec // test fixture
	require.Len(t, jar.Cookies(u), 1)

	jar.Clear()
	assert.Empty(t, jar.Cookies(u))
}

func TestResponseCookies_ParsesSetCookieAttributes(t *testing.T) {
	headers := http.Header{}
	headers.Add("Set-Cookie", "sid=abc; Path=/app; HttpOnly; Secure; SameSite=Lax")
	headers.Add("Set-Cookie", "theme=dark")

	cookies := exec.ResponseCookies(headers)
	require.Len(t, cookies, 2)
	assert.Equal(t, "sid", cookies[0].Name)
	assert.Equal(t, "abc", cookies[0].Value)
	assert.Equal(t, "/app", cookies[0].Path)
	assert.True(t, cookies[0].HttpOnly)
	assert.True(t, cookies[0].Secure)
	assert.Equal(t, http.SameSiteLaxMode, cookies[0].SameSite)
	assert.Equal(t, "theme", cookies[1].Name)

	assert.Empty(t, exec.ResponseCookies(http.Header{}))
}
