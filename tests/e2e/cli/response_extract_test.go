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

// A token extracted from one `quark run` is sent by the next one.
func TestRunCommand_ExtractsIntoEnvironmentForTheNextRequest(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "extract.db"))
	require.NoError(t, err)
	defer st.Close()
	ctx := context.Background()

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"session":{"token":"tok-42"}}`))
			return
		}
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	col := &domain.Collection{Name: "API"}
	require.NoError(t, st.SaveCollection(ctx, col))
	if _, err := st.CreateDefaultEnvironment(ctx, col.ID); err != nil {
		require.ErrorIs(t, err, store.ErrDuplicate)
	}
	require.NoError(t, st.SaveRequest(ctx, &domain.Request{
		CollectionID: col.ID, Name: "login", Method: "POST", URL: srv.URL + "/login",
		Extractions: domain.EncodeExtractions([]domain.Extraction{
			{Variable: "token", Source: domain.ExtractFromBody, Path: "$.session.token"},
		}),
	}))
	require.NoError(t, st.SaveRequest(ctx, &domain.Request{
		CollectionID: col.ID, Name: "me", Method: "GET", URL: srv.URL + "/me",
		Headers: `{"Authorization":"Bearer {{token}}"}`,
	}))

	executor := exec.New(http.DefaultTransport,
		exec.WithPostResponseHooks([]exec.PostResponseHook{exec.NewExtractionHook(st)}))
	run := func(path string) string {
		cmd := quarkcli.NewRunCmd(st, executor)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{path})
		cmd.SetContext(ctx)
		require.NoError(t, cmd.Execute())
		return out.String()
	}

	assert.Contains(t, run("API/login"), "Extracted: token -> default")
	run("API/me")
	assert.Equal(t, "Bearer tok-42", gotAuth)
}
