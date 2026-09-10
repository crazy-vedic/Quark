//go:build e2e

package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quarkcli "github.com/crazy-vedic/quark/internal/cli"
	"github.com/crazy-vedic/quark/internal/domain"
	quarkexec "github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/store"
)

type nestedEnvironmentFixture struct {
	root          *domain.Collection
	parent        *domain.Collection
	child         *domain.Collection
	request       *domain.Request
	global        *domain.Environment
	rootDefault   *domain.Environment
	rootActive    *domain.Environment
	parentDefault *domain.Environment
	parentActive  *domain.Environment
	childDefault  *domain.Environment
	childActive   *domain.Environment
}

func seedNestedEnvironmentFixture(
	t *testing.T,
	st *store.Store,
	requestURL string,
) nestedEnvironmentFixture {
	t.Helper()
	ctx := context.Background()

	fixture := nestedEnvironmentFixture{
		root:   &domain.Collection{ID: "root", Name: "Root"},
		parent: &domain.Collection{ID: "parent", Name: "Parent", ParentID: "root"},
		child:  &domain.Collection{ID: "child", Name: "Child", ParentID: "parent"},
	}
	for _, collection := range []*domain.Collection{fixture.root, fixture.parent, fixture.child} {
		require.NoError(t, st.SaveCollection(ctx, collection))
	}

	var err error
	fixture.global, err = st.GetGlobalEnvironment(ctx)
	require.NoError(t, err)
	fixture.rootDefault, err = st.GetEnvironmentByName(ctx, fixture.root.ID, "default")
	require.NoError(t, err)
	fixture.parentDefault, err = st.GetEnvironmentByName(ctx, fixture.parent.ID, "default")
	require.NoError(t, err)
	fixture.childDefault, err = st.GetEnvironmentByName(ctx, fixture.child.ID, "default")
	require.NoError(t, err)

	fixture.rootActive = saveNamedEnvironment(t, st, fixture.root.ID, "root-dev", "dev", nil)
	fixture.parentActive = saveNamedEnvironment(t, st, fixture.parent.ID, "parent-dev", "dev", nil)
	fixture.childActive = saveNamedEnvironment(t, st, fixture.child.ID, "child-dev", "dev", nil)
	_ = saveNamedEnvironment(t, st, fixture.root.ID, "root-prod", "prod", map[string]string{"shared": "wrong-root-selection"})
	_ = saveNamedEnvironment(t, st, fixture.parent.ID, "parent-prod", "prod", map[string]string{"shared": "wrong-parent-selection"})
	require.NoError(t, st.SetActiveEnvironment(ctx, fixture.root.ID, fixture.rootActive.ID))
	require.NoError(t, st.SetActiveEnvironment(ctx, fixture.parent.ID, fixture.parentActive.ID))
	require.NoError(t, st.SetActiveEnvironment(ctx, fixture.child.ID, fixture.childActive.ID))

	fixture.request = &domain.Request{
		ID:           "probe-request",
		CollectionID: fixture.child.ID,
		Name:         "Probe",
		Method:       http.MethodGet,
		URL:          requestURL,
	}
	require.NoError(t, st.SaveRequest(ctx, fixture.request))
	return fixture
}

func saveNamedEnvironment(
	t *testing.T,
	st *store.Store,
	collectionID, id, name string,
	vars map[string]string,
) *domain.Environment {
	t.Helper()
	environment := &domain.Environment{ID: id, CollectionID: collectionID, Name: name}
	environment.SetVars(vars)
	require.NoError(t, st.SaveEnvironment(context.Background(), environment))
	return environment
}

func saveEnvironmentVars(t *testing.T, st *store.Store, environment *domain.Environment, vars map[string]string) {
	t.Helper()
	environment.SetVars(vars)
	require.NoError(t, st.SaveEnvironment(context.Background(), environment))
}

type freshResponseRoundTripper struct {
	mu      sync.Mutex
	calls   int
	lastURL string
}

func (rt *freshResponseRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.mu.Lock()
	rt.calls++
	rt.lastURL = req.URL.String()
	rt.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
		Request:    req,
	}, nil
}

func (rt *freshResponseRoundTripper) snapshot() (int, string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.calls, rt.lastURL
}

func (rt *freshResponseRoundTripper) reset() {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.calls = 0
	rt.lastURL = ""
}

// This is the resolver's exhaustive precedence matrix, but driven through the
// real SQLite store and CLI execution boundary. Each bit controls whether the
// shared key exists in one of the seven ordered layers.
func TestE2E_RunNestedHierarchy_All128PrecedenceCombinations(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "precedence.db"), store.WithCacheSize(100))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	fixture := seedNestedEnvironmentFixture(t, st, "https://example.test/{{shared}}")
	layers := []struct {
		name string
		env  *domain.Environment
	}{
		{"global", fixture.global},
		{"root-default", fixture.rootDefault},
		{"root-active", fixture.rootActive},
		{"parent-default", fixture.parentDefault},
		{"parent-active", fixture.parentActive},
		{"child-default", fixture.childDefault},
		{"child-active", fixture.childActive},
	}
	transport := &freshResponseRoundTripper{}
	executor := quarkexec.New(transport)

	for mask := 0; mask < 1<<len(layers); mask++ {
		t.Run(fmt.Sprintf("presence_%07b", mask), func(t *testing.T) {
			expected := ""
			for i, layer := range layers {
				vars := map[string]string{}
				if mask&(1<<i) != 0 {
					vars["shared"] = layer.name
					expected = layer.name
				}
				saveEnvironmentVars(t, st, layer.env, vars)
			}

			transport.reset()
			cmd := quarkcli.NewRunCmd(st, executor)
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			cmd.SetContext(context.Background())
			cmd.SetArgs([]string{"Root/Parent/Child/Probe"})
			err := cmd.Execute()
			calls, lastURL := transport.snapshot()

			if expected == "" {
				require.Error(t, err)
				assert.ErrorIs(t, err, quarkexec.ErrUnresolvedVariable)
				assert.Zero(t, calls, "unresolved interpolation must prevent dispatch")
				return
			}
			require.NoError(t, err, output.String())
			assert.Equal(t, 1, calls)
			assert.Equal(t, "https://example.test/"+expected, lastURL)
		})
	}
}

type observedRequest struct {
	method  string
	path    string
	query   string
	body    string
	headers http.Header
}

func TestE2E_CLIBinary_NestedInheritanceActiveVariantsAndReopen(t *testing.T) {
	requests := make(chan observedRequest, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		requests <- observedRequest{
			method: r.Method, path: r.URL.Path, query: r.URL.RawQuery,
			body: string(body), headers: r.Header.Clone(),
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)

	home := t.TempDir()
	dbPath := createQuarkDBPath(t, home)
	st, err := store.New(dbPath, store.WithCacheSize(100))
	require.NoError(t, err)
	fixture := seedNestedEnvironmentFixture(t, st, srv.URL+
		"/execute/{{shared}}?g={{g}}&rd={{rd}}&ra={{ra|rd}}&pd={{pd}}&pa={{pa|pd}}&cd={{cd}}&ca={{ca|cd}}&empty={{empty}}")

	saveEnvironmentVars(t, st, fixture.global, map[string]string{"g": "global", "shared": "global", "empty": "global-nonempty"})
	saveEnvironmentVars(t, st, fixture.rootDefault, map[string]string{"rd": "root-default", "shared": "root-default"})
	saveEnvironmentVars(t, st, fixture.rootActive, map[string]string{"ra": "root-dev", "shared": "root-dev"})
	saveEnvironmentVars(t, st, fixture.parentDefault, map[string]string{"pd": "parent-default", "shared": "parent-default"})
	saveEnvironmentVars(t, st, fixture.parentActive, map[string]string{"pa": "parent-dev", "shared": "parent-dev"})
	_ = saveNamedEnvironment(t, st, fixture.parent.ID, "parent-Dev", "Dev", map[string]string{"pa": "wrong-case", "shared": "wrong-case"})
	saveEnvironmentVars(t, st, fixture.childDefault, map[string]string{"cd": "child-default", "shared": "child-default"})
	saveEnvironmentVars(t, st, fixture.childActive, map[string]string{
		"ca": "child-dev", "shared": "child-dev", "empty": "", "token": "child-token",
	})
	fixture.request.Method = http.MethodPost
	fixture.request.Headers = `{"X-Parent":"{{pa|pd}}","X-Root":"{{ra|rd}}","X-Shared":"{{shared}}"}`
	fixture.request.AuthType = domain.AuthTypeBearer
	fixture.request.AuthConfig = `{"token":"{{token|shared}}"}`
	fixture.request.Body = `{"shared":"{{shared}}","global":"{{g}}"}`
	require.NoError(t, st.SaveRequest(context.Background(), fixture.request))
	require.NoError(t, st.Close())

	runAndObserve := func(reference string) observedRequest {
		t.Helper()
		stdout, stderr, code := runQuarkWithHome(t, home, "run", reference)
		require.Equal(t, 0, code, "stdout=%s stderr=%s", stdout, stderr)
		select {
		case got := <-requests:
			return got
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for HTTP request")
			return observedRequest{}
		}
	}

	assertDevChain := func(t *testing.T, got observedRequest, rootActive string) {
		t.Helper()
		assert.Equal(t, http.MethodPost, got.method)
		assert.Equal(t, "/execute/child-dev", got.path)
		assert.Contains(t, got.query, "g=global")
		assert.Contains(t, got.query, "rd=root-default")
		assert.Contains(t, got.query, "ra="+rootActive)
		assert.Contains(t, got.query, "pd=parent-default")
		assert.Contains(t, got.query, "pa=parent-dev")
		assert.Contains(t, got.query, "cd=child-default")
		assert.Contains(t, got.query, "ca=child-dev")
		assert.Contains(t, got.query, "empty=")
		assert.Equal(t, rootActive, got.headers.Get("X-Root"))
		assert.Equal(t, "parent-dev", got.headers.Get("X-Parent"))
		assert.Equal(t, "child-dev", got.headers.Get("X-Shared"))
		assert.Equal(t, "Bearer child-token", got.headers.Get("Authorization"))
		assert.JSONEq(t, `{"shared":"child-dev","global":"global"}`, got.body)
	}

	// Exact full path exercises structured nested lookup.
	assertDevChain(t, runAndObserve("Root/Parent/Child/Probe"), "root-dev")
	// The shortest unique suffix exercises legacy shorthand lookup.
	assertDevChain(t, runAndObserve("Child/Probe"), "root-dev")

	// A missing ancestor named environment is skipped; its default still applies.
	st, err = store.New(dbPath, store.WithCacheSize(100))
	require.NoError(t, err)
	require.NoError(t, st.DeleteEnvironment(context.Background(), fixture.rootActive.ID))
	require.NoError(t, st.Close())
	assertDevChain(t, runAndObserve("Child/Probe"), "root-default")

	// Selecting child default changes only the child's layer; the parent keeps
	// its independently persisted active environment.
	st, err = store.New(dbPath, store.WithCacheSize(100))
	require.NoError(t, err)
	require.NoError(t, st.SetActiveEnvironment(context.Background(), fixture.child.ID, fixture.childDefault.ID))
	require.NoError(t, st.Close())
	got := runAndObserve("Child/Probe")
	assert.Equal(t, "/execute/child-default", got.path)
	assert.Contains(t, got.query, "ra=root-default")
	assert.Contains(t, got.query, "pa=parent-dev")
	assert.Contains(t, got.query, "ca=child-default")
	assert.Contains(t, got.query, "empty=global-nonempty")
	assert.Equal(t, "Bearer child-default", got.headers.Get("Authorization"))

	// A child-only active name is valid; absent ancestor matches are skipped.
	st, err = store.New(dbPath, store.WithCacheSize(100))
	require.NoError(t, err)
	staging := saveNamedEnvironment(t, st, fixture.child.ID, "child-staging", "staging", map[string]string{
		"ca": "child-staging", "shared": "child-staging", "token": "staging-token",
	})
	require.NoError(t, st.SetActiveEnvironment(context.Background(), fixture.child.ID, staging.ID))
	require.NoError(t, st.Close())
	got = runAndObserve("Child/Probe")
	assert.Equal(t, "/execute/child-staging", got.path)
	assert.Contains(t, got.query, "ra=root-default")
	assert.Contains(t, got.query, "pa=parent-dev")
	assert.Contains(t, got.query, "ca=child-staging")
	assert.Equal(t, "Bearer staging-token", got.headers.Get("Authorization"))

	// Reopening the database retains the child-owned active selection.
	st, err = store.New(dbPath, store.WithCacheSize(100))
	require.NoError(t, err)
	activeID, err := st.GetActiveEnvironment(context.Background(), fixture.child.ID)
	require.NoError(t, err)
	assert.Equal(t, staging.ID, activeID)
	require.NoError(t, st.Close())
}

func TestE2E_CLIBinary_ResolutionFailuresPreventDispatch(t *testing.T) {
	corruptions := []struct {
		name      string
		wantError string
		corrupt   func(*testing.T, *store.Store, nestedEnvironmentFixture)
	}{
		{
			name: "parent-owned active selected by child", wantError: "invalid environment ownership",
			corrupt: func(t *testing.T, st *store.Store, f nestedEnvironmentFixture) {
				_, err := st.DB().ExecContext(context.Background(),
					`UPDATE collection_active_env SET env_id = ? WHERE collection_id = ?`,
					f.parentActive.ID, f.child.ID)
				require.NoError(t, err)
			},
		},
		{
			name: "malformed child default", wantError: "decode environment",
			corrupt: func(t *testing.T, st *store.Store, f nestedEnvironmentFixture) {
				_, err := st.DB().ExecContext(context.Background(),
					`UPDATE environments SET data = '["not-an-object"]' WHERE id = ?`, f.childDefault.ID)
				require.NoError(t, err)
			},
		},
		{
			name: "broken parent", wantError: "missing-parent",
			corrupt: func(t *testing.T, st *store.Store, f nestedEnvironmentFixture) {
				_, err := st.DB().ExecContext(context.Background(), `PRAGMA foreign_keys = OFF`)
				require.NoError(t, err)
				_, err = st.DB().ExecContext(context.Background(),
					`UPDATE collections SET parent_id = 'missing-parent' WHERE id = ?`, f.parent.ID)
				require.NoError(t, err)
			},
		},
		{
			name: "collection cycle", wantError: "cycle",
			corrupt: func(t *testing.T, st *store.Store, f nestedEnvironmentFixture) {
				_, err := st.DB().ExecContext(context.Background(),
					`UPDATE collections SET parent_id = ? WHERE id = ?`, f.child.ID, f.root.ID)
				require.NoError(t, err)
			},
		},
	}

	for _, tc := range corruptions {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				hits.Add(1)
				w.WriteHeader(http.StatusNoContent)
			}))
			t.Cleanup(srv.Close)

			home := t.TempDir()
			dbPath := createQuarkDBPath(t, home)
			st, err := store.New(dbPath, store.WithCacheSize(100))
			require.NoError(t, err)
			fixture := seedNestedEnvironmentFixture(t, st, srv.URL+"/must-not-dispatch")
			tc.corrupt(t, st, fixture)
			require.NoError(t, st.Close())

			stdout, stderr, code := runQuarkWithHome(t, home, "run", "Child/Probe")
			assert.NotEqual(t, 0, code, "stdout=%s stderr=%s", stdout, stderr)
			assert.Contains(t, strings.ToLower(stderr), strings.ToLower(tc.wantError))
			assert.Zero(t, hits.Load())
		})
	}
}

func TestE2E_CLIBinary_ScheduledNestedInheritanceAndFailurePersistence(t *testing.T) {
	var hits atomic.Int32
	requests := make(chan observedRequest, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		requests <- observedRequest{path: r.URL.Path, headers: r.Header.Clone()}
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)

	home := t.TempDir()
	dbPath := createQuarkDBPath(t, home)
	st, err := store.New(dbPath, store.WithCacheSize(100))
	require.NoError(t, err)
	fixture := seedNestedEnvironmentFixture(t, st, srv.URL+"/scheduled/{{shared}}")
	saveEnvironmentVars(t, st, fixture.global, map[string]string{"shared": "global"})
	saveEnvironmentVars(t, st, fixture.rootDefault, map[string]string{"shared": "root-default"})
	saveEnvironmentVars(t, st, fixture.rootActive, map[string]string{"shared": "root-dev"})
	saveEnvironmentVars(t, st, fixture.parentDefault, map[string]string{"shared": "parent-default"})
	saveEnvironmentVars(t, st, fixture.parentActive, map[string]string{"shared": "parent-dev"})
	saveEnvironmentVars(t, st, fixture.childDefault, map[string]string{"shared": "child-default"})
	saveEnvironmentVars(t, st, fixture.childActive, map[string]string{"shared": "child-dev"})
	require.NoError(t, st.SaveScheduledRun(context.Background(), &domain.ScheduledRun{
		ID: "scheduled-success", RequestID: fixture.request.ID,
		RunAt: time.Now().Add(-time.Minute), Status: domain.ScheduledRunPending,
	}))
	require.NoError(t, st.Close())

	stdout, stderr, code := runQuarkWithHome(t, home, "schedule", "run-due")
	require.Equal(t, 0, code, "stdout=%s stderr=%s", stdout, stderr)
	select {
	case got := <-requests:
		assert.Equal(t, "/scheduled/child-dev", got.path)
	case <-time.After(3 * time.Second):
		t.Fatal("scheduled request was not dispatched")
	}

	st, err = store.New(dbPath, store.WithCacheSize(100))
	require.NoError(t, err)
	run, err := st.GetScheduledRun(context.Background(), "scheduled-success")
	require.NoError(t, err)
	assert.Equal(t, domain.ScheduledRunCompleted, run.Status)
	assert.Empty(t, run.LastError)

	// Break an invariant and verify a later due run is marked failed without a
	// second network request.
	_, err = st.DB().ExecContext(context.Background(),
		`UPDATE environments SET data = '{"broken":42}' WHERE id = ?`, fixture.parentDefault.ID)
	require.NoError(t, err)
	require.NoError(t, st.SaveScheduledRun(context.Background(), &domain.ScheduledRun{
		ID: "scheduled-failure", RequestID: fixture.request.ID,
		RunAt: time.Now().Add(-time.Minute), Status: domain.ScheduledRunPending,
	}))
	require.NoError(t, st.Close())

	stdout, stderr, code = runQuarkWithHome(t, home, "schedule", "run-due")
	require.Equal(t, 0, code, "individual scheduled failures are persisted: stdout=%s stderr=%s", stdout, stderr)
	assert.Contains(t, stdout, "failed")
	assert.Equal(t, int32(1), hits.Load(), "resolution failure must prevent network dispatch")

	st, err = store.New(dbPath, store.WithCacheSize(100))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })
	run, err = st.GetScheduledRun(context.Background(), "scheduled-failure")
	require.NoError(t, err)
	assert.Equal(t, domain.ScheduledRunFailed, run.Status)
	assert.Contains(t, run.LastError, "resolve environments")
	assert.Contains(t, run.LastError, "invalid environment data")
}

func TestE2E_OptionalExecutorResolver_ResolvesOnceAndBlocksOnFailure(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "executor-resolver.db"), store.WithCacheSize(100))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })
	fixture := seedNestedEnvironmentFixture(t, st, "https://example.test/{{shared}}")
	saveEnvironmentVars(t, st, fixture.childActive, map[string]string{"shared": "resolved"})

	transport := &freshResponseRoundTripper{}
	var resolutions atomic.Int32
	resolver := func(collectionID string) (map[string]string, map[string]string, error) {
		resolutions.Add(1)
		return quarkexec.ResolveEnvVars(context.Background(), st, collectionID)
	}
	executor := quarkexec.New(transport, quarkexec.WithVariableResolver(resolver))
	_, err = executor.Execute(context.Background(), fixture.request)
	require.NoError(t, err)
	assert.Equal(t, int32(1), resolutions.Load())
	calls, lastURL := transport.snapshot()
	assert.Equal(t, 1, calls)
	assert.Equal(t, "https://example.test/resolved", lastURL)

	transport.reset()
	require.NoError(t, st.DeleteEnvironment(context.Background(), fixture.parentDefault.ID))
	_, err = executor.Execute(context.Background(), fixture.request)
	require.Error(t, err)
	assert.ErrorIs(t, err, quarkexec.ErrMissingDefaultEnvironment)
	assert.Equal(t, int32(2), resolutions.Load(), "one additional attempt must resolve exactly once")
	calls, _ = transport.snapshot()
	assert.Zero(t, calls)
}

func createQuarkDBPath(t *testing.T, home string) string {
	t.Helper()
	quarkDir := filepath.Join(home, ".quark")
	require.NoError(t, os.MkdirAll(quarkDir, 0o700))
	return filepath.Join(quarkDir, "quark.db")
}
