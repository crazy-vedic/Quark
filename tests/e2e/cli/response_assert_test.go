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

// quark run exits non-zero when an assertion fails and reports each result.
func TestRunCommand_AssertionsDecideExitStatus(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "assert.db"))
	require.NoError(t, err)
	defer st.Close()
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":7}`))
	}))
	defer srv.Close()

	col := &domain.Collection{Name: "API"}
	require.NoError(t, st.SaveCollection(ctx, col))
	require.NoError(t, st.SaveRequest(ctx, &domain.Request{
		CollectionID: col.ID, Name: "ok", Method: "GET", URL: srv.URL + "/item",
		Assertions: domain.EncodeAssertions([]domain.Assertion{
			{Kind: domain.AssertStatus, Op: "=", Value: "200"},
			{Kind: domain.AssertJSONPath, Target: "$.id", Op: "=", Value: "7"},
		}),
	}))
	require.NoError(t, st.SaveRequest(ctx, &domain.Request{
		CollectionID: col.ID, Name: "bad", Method: "GET", URL: srv.URL + "/item",
		Assertions: domain.EncodeAssertions([]domain.Assertion{
			{Kind: domain.AssertStatus, Op: "=", Value: "404"},
		}),
	}))

	executor := exec.New(http.DefaultTransport,
		exec.WithPostResponseHooks([]exec.PostResponseHook{exec.NewAssertionHook()}))
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

	out, err := run("API/ok")
	require.NoError(t, err)
	assert.Contains(t, out, "Assert PASS: status = 200")
	assert.Contains(t, out, "Assert PASS: $.id = 7")

	out, err = run("API/bad")
	require.Error(t, err)
	assert.Contains(t, out, "Assert FAIL: status = 404 (status is 200)")
}
