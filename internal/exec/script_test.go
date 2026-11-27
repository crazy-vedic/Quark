package exec

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

func newScriptStore(t *testing.T) (*store.Store, *domain.Collection) {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "scripts.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	col := &domain.Collection{Name: "api"}
	require.NoError(t, st.SaveCollection(ctx, col))
	if _, err := st.CreateDefaultEnvironment(ctx, col.ID); err != nil {
		require.ErrorIs(t, err, store.ErrDuplicate)
	}
	return st, col
}

func envValue(t *testing.T, st *store.Store, colID, name string) (string, bool) {
	t.Helper()
	env, err := st.GetEnvironmentByName(context.Background(), colID, "default")
	require.NoError(t, err)
	vars, err := env.DecodeVars()
	require.NoError(t, err)
	v, ok := vars[name]
	return v, ok
}

func TestPreRequestScriptChangesTheRequestAndEnvironment(t *testing.T) {
	st, col := newScriptStore(t)
	hook := NewScriptHook(st)
	req := &domain.Request{
		CollectionID: col.ID, Method: "GET", URL: "https://old.example.test/a",
		Headers: `{"X-Keep":"1"}`, Body: "old",
		PreScript: `
			req.method = 'POST';
			req.url = req.url + '/b';
			req.body = env.get('payload') + '!';
			req.headers['X-Added'] = 'yes';
			delete req.headers['X-Keep'];
			env.set('stamp', 'set-by-pre');
		`,
	}
	require.NoError(
		t,
		st.SaveEnvironment(
			context.Background(),
			envWithVars(t, st, col.ID, map[string]string{"payload": "hi"}),
		),
	)

	got, err := hook.BeforeRequest(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "POST", got.Method)
	assert.Equal(t, "https://old.example.test/a/b", got.URL)
	assert.Equal(t, "hi!", got.Body)
	assert.JSONEq(t, `{"X-Added":"yes"}`, got.Headers)
	assert.Equal(t, "GET", req.Method, "the stored request is not modified")

	v, ok := envValue(t, st, col.ID, "stamp")
	require.True(t, ok)
	assert.Equal(t, "set-by-pre", v)
}

func TestPreRequestScriptErrorAbortsTheRequest(t *testing.T) {
	st, col := newScriptStore(t)
	_, err := NewScriptHook(st).BeforeRequest(context.Background(), &domain.Request{
		CollectionID: col.ID, Method: "GET", URL: "http://x", PreScript: "throw new Error('boom')",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pre-request script")
	assert.Contains(t, err.Error(), "boom")
}

func TestPreRequestScriptWithoutChangesKeepsHeadersAsStored(t *testing.T) {
	st, col := newScriptStore(t)
	raw := `{"Authorization":["a","b"]}`
	req := &domain.Request{
		CollectionID: col.ID, Method: "GET", URL: "http://x", Headers: raw,
		PreScript: "console.log('seen', req.method)",
	}
	got, err := NewScriptHook(st).BeforeRequest(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, raw, got.Headers, "repeated headers survive a script that does not touch them")
}

func TestPostResponseScriptAssertsReadsAndLogs(t *testing.T) {
	st, col := newScriptStore(t)
	hook := NewScriptHook(st)
	res := &ExecuteResult{
		StatusCode: 201,
		Headers:    http.Header{"Content-Type": {"application/json"}},
		Body:       []byte(`{"id":42,"name":"Ada"}`),
		Duration:   250 * time.Millisecond,
	}
	req := &domain.Request{
		CollectionID: col.ID,
		PostScript: `
			assert(res.status === 201, 'created');
			assert(res.json().name === 'Bob', 'name matches');
			assert(res.headers['content-type'].indexOf('json') >= 0, 'json type');
			env.set('userId', String(res.json().id));
			console.log('status', res.status, 'time', res.time);
		`,
	}
	log := NewScriptLog()
	ctx := WithScriptLog(context.Background(), log)
	require.NoError(t, hook.AfterResponse(ctx, req, res))

	require.Len(t, res.ScriptChecks, 3)
	assert.True(t, res.ScriptChecks[0].Passed)
	assert.Equal(t, "assert: created", res.ScriptChecks[0].Rule)
	assert.False(t, res.ScriptChecks[1].Passed)
	assert.Equal(t, "condition was false", res.ScriptChecks[1].Reason)
	assert.True(t, res.ScriptChecks[2].Passed)
	assert.Equal(t, []string{"status 201 time 250"}, log.Lines())

	v, ok := envValue(t, st, col.ID, "userId")
	require.True(t, ok)
	assert.Equal(t, "42", v)
}

func TestPostResponseScriptExceptionIsAFailedResult(t *testing.T) {
	st, col := newScriptStore(t)
	res := &ExecuteResult{StatusCode: 200, Body: []byte("not json")}
	req := &domain.Request{CollectionID: col.ID, PostScript: "res.json()"}
	require.NoError(t, NewScriptHook(st).AfterResponse(context.Background(), req, res))
	require.Len(t, res.ScriptChecks, 1)
	assert.False(t, res.ScriptChecks[0].Passed)
	assert.Equal(t, "post-response script", res.ScriptChecks[0].Rule)
}

func TestScriptIsInterruptedAfterTheTimeout(t *testing.T) {
	st, col := newScriptStore(t)
	hook := NewScriptHook(st)
	hook.timeout = 50 * time.Millisecond
	res := &ExecuteResult{StatusCode: 200}
	req := &domain.Request{CollectionID: col.ID, PostScript: "while (true) {}"}
	start := time.Now()
	require.NoError(t, hook.AfterResponse(context.Background(), req, res))
	assert.Less(t, time.Since(start), 5*time.Second)
	require.Len(t, res.ScriptChecks, 1)
	assert.Contains(t, res.ScriptChecks[0].Reason, "timed out")
}

func TestScriptsRunThroughExecutor(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Token")
		_, _ = w.Write([]byte(`{"token":"t-1"}`))
	}))
	defer srv.Close()
	st, col := newScriptStore(t)

	ex := New(http.DefaultTransport, WithPreRequestHooks([]PreRequestHook{NewScriptHook(st)}),
		WithPostResponseHooks([]PostResponseHook{NewScriptHook(st)}))
	first := &domain.Request{
		CollectionID: col.ID, Method: "GET", URL: srv.URL,
		PostScript: "env.set('token', res.json().token); assert(res.status === 200, 'ok');",
	}
	out, err := ex.Execute(context.Background(), first)
	require.NoError(t, err)
	require.Len(t, out.ScriptChecks, 1)
	assert.True(t, out.ScriptChecks[0].Passed)

	second := &domain.Request{
		CollectionID: col.ID, Method: "GET", URL: srv.URL,
		PreScript: "req.headers['X-Token'] = env.get('token');",
	}
	_, err = ex.Execute(context.Background(), second)
	require.NoError(t, err)
	assert.Equal(t, "t-1", gotHeader)
}

func envWithVars(
	t *testing.T,
	st *store.Store,
	colID string,
	vars map[string]string,
) *domain.Environment {
	t.Helper()
	env, err := st.GetEnvironmentByName(context.Background(), colID, "default")
	require.NoError(t, err)
	env.SetVars(vars)
	return env
}
