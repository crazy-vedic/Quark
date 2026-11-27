//go:build e2e

package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quarkcli "github.com/crazy-vedic/quark/internal/cli"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/store"
)

// A pre-request script sets a header from the environment, a post-response
// script records a value and a console line, and quark run reports both.
func TestRunCommand_RunsPreAndPostScripts(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "scripts.db"))
	require.NoError(t, err)
	defer st.Close()
	ctx := context.Background()

	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_, _ = w.Write([]byte(`{"token":"abc"}`))
			return
		}
		gotHeader = r.Header.Get("X-Token")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	col := &domain.Collection{Name: "API"}
	require.NoError(t, st.SaveCollection(ctx, col))
	if _, err := st.CreateDefaultEnvironment(ctx, col.ID); err != nil {
		require.ErrorIs(t, err, store.ErrDuplicate)
	}
	require.NoError(t, st.SaveRequest(ctx, &domain.Request{
		CollectionID: col.ID, Name: "token", Method: "GET", URL: srv.URL + "/token",
		PostScript: "env.set('token', res.json().token); console.log('got token'); assert(res.status === 200, 'ok');",
	}))
	require.NoError(t, st.SaveRequest(ctx, &domain.Request{
		CollectionID: col.ID, Name: "use", Method: "GET", URL: srv.URL + "/use",
		PreScript: "req.headers['X-Token'] = env.get('token');",
	}))

	executor := exec.New(http.DefaultTransport,
		exec.WithPreRequestHooks([]exec.PreRequestHook{exec.NewScriptHook(st)}),
		exec.WithPostResponseHooks([]exec.PostResponseHook{
			exec.NewExtractionHook(st), exec.NewScriptHook(st),
		}))
	run := func(path string) (string, error) {
		cmd := quarkcli.NewRunCmd(st, executor)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{path})
		cmd.SetContext(ctx)
		err := cmd.Execute()
		return out.String(), err
	}

	out, err := run("API/token")
	require.NoError(t, err)
	assert.Contains(t, out, "Console: got token")
	assert.Contains(t, out, "Script PASS: assert: ok")

	_, err = run("API/use")
	require.NoError(t, err)
	assert.Equal(t, "abc", gotHeader)
}
