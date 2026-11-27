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

// quark test runs a collection in order, reports JUnit XML, and exits non-zero
// when a request fails.
func TestTestCommand_RunsCollectionAndReportsJUnit(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "runner.db"))
	require.NoError(t, err)
	defer st.Close()
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fail" {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	col := &domain.Collection{Name: "Smoke"}
	require.NoError(t, st.SaveCollection(ctx, col))
	if _, err := st.CreateDefaultEnvironment(ctx, col.ID); err != nil {
		require.ErrorIs(t, err, store.ErrDuplicate)
	}
	require.NoError(t, st.SaveRequest(ctx, &domain.Request{
		CollectionID: col.ID, Name: "health", Method: "GET", URL: srv.URL + "/health", SortOrder: 1,
		Assertions: domain.EncodeAssertions([]domain.Assertion{
			{Kind: domain.AssertJSONPath, Target: "$.ok", Op: "=", Value: "true"},
		}),
	}))
	require.NoError(t, st.SaveRequest(ctx, &domain.Request{
		CollectionID: col.ID, Name: "fail", Method: "GET", URL: srv.URL + "/fail", SortOrder: 2,
		Assertions: domain.EncodeAssertions([]domain.Assertion{
			{Kind: domain.AssertStatus, Op: "=", Value: "200"},
		}),
	}))

	executor := exec.New(http.DefaultTransport, exec.WithPostResponseHooks([]exec.PostResponseHook{
		exec.NewExtractionHook(st),
		exec.NewAssertionHook(),
	}))
	run := func(args ...string) (string, error) {
		cmd := quarkcli.NewTestCmd(st, executor)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(args)
		cmd.SetContext(ctx)
		err := cmd.Execute()
		return out.String(), err
	}

	out, err := run("Smoke", "--format", "junit")
	require.Error(t, err, "one request fails, so the command exits non-zero")
	assert.Contains(t, out, `<testsuite name="Smoke" tests="2" failures="1"`)
	assert.Contains(t, out, `<testcase name="Smoke/health" classname="Smoke"`)
	assert.Contains(t, out, `message="status = 200: status is 502"`)

	out, err = run("Smoke", "--format", "tap", "--bail")
	require.Error(t, err)
	assert.Contains(t, out, "TAP version 13\n1..2\nok 1 - Smoke/health\nnot ok 2 - Smoke/fail\n")
}
