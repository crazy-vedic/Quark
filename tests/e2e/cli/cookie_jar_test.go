//go:build e2e

package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

// A redirect sets a session cookie and sends the client to a page that needs
// it. With http.cookie_jar enabled the cookie is kept for the redirected
// request; without it, the page sees no cookie.
func TestCLI_CookieJarKeepsRedirectCookieOnlyWhenEnabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			//nolint:gosec // G124: test cookie over plain HTTP; Secure would stop it being sent.
			http.SetCookie(w, &http.Cookie{
				Name:     "sid",
				Value:    "abc",
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
			http.Redirect(w, r, "/check", http.StatusFound)
		case "/check":
			if c, err := r.Cookie("sid"); err == nil && c.Value == "abc" {
				_, _ = w.Write([]byte("signed in"))
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("anonymous"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	tests := []struct {
		name      string
		config    string
		wantCode  int
		wantOut   string
		wantNoOut string
	}{
		{
			name:     "cookie jar enabled in config",
			config:   "[http]\ncookie_jar = true\n",
			wantCode: 0,
			wantOut:  "signed in",
		},
		{
			name:      "cookie jar off by default",
			config:    "",
			wantCode:  0,
			wantOut:   "anonymous",
			wantNoOut: "signed in",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home, colID := setupHomeWithCollection(t)
			if tc.config != "" {
				configPath := filepath.Join(home, ".quark", "config.toml")
				require.NoError(t, os.WriteFile(configPath, []byte(tc.config), 0o600))
			}
			seedRequest(t, home, colID, &domain.Request{
				Name:   "Flow",
				Method: http.MethodGet,
				URL:    srv.URL + "/start",
			})

			out, errOut, code := runQuarkWithHome(t, home, "run", "API/Flow")
			assert.Equal(t, tc.wantCode, code, "run: %s %s", out, errOut)
			assert.Contains(t, out, tc.wantOut)
			if tc.wantNoOut != "" {
				assert.NotContains(t, out, tc.wantNoOut)
			}
		})
	}
}
