package exec

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
)

// SessionJar is an in-memory http.CookieJar that can be emptied as a whole.
// It is never written to disk: cookies set by responses are session state,
// and keeping them only for the life of the process avoids replaying them in
// later runs without the user asking for that.
type SessionJar struct {
	mu  sync.RWMutex
	jar *cookiejar.Jar
}

// NewSessionJar returns an empty session jar.
func NewSessionJar() *SessionJar {
	return &SessionJar{jar: newCookieJar()}
}

func newCookieJar() *cookiejar.Jar {
	// cookiejar.New only fails when Options are invalid; nil Options cannot be.
	jar, _ := cookiejar.New(nil)
	return jar
}

// SetCookies implements http.CookieJar.
func (s *SessionJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	s.jar.SetCookies(u, cookies)
}

// Cookies implements http.CookieJar.
func (s *SessionJar) Cookies(u *url.URL) []*http.Cookie {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.jar.Cookies(u)
}

// Clear discards every cookie in the jar.
func (s *SessionJar) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jar = newCookieJar()
}

// ResponseCookies returns the cookies a response asks the client to store,
// parsed from its Set-Cookie headers. Headers without Set-Cookie yield nil.
func ResponseCookies(headers http.Header) []*http.Cookie {
	return (&http.Response{Header: headers}).Cookies()
}
