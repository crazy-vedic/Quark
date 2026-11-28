//go:build e2e

package tui_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/store"
	"github.com/crazy-vedic/quark/internal/tui"
)

// echoedRequest is what the echo server saw on the wire.
type echoedRequest struct {
	Path string `json:"path"`
	Body string `json:"body"`
	Guid string `json:"guid"`
	Rand string `json:"rand"`
}

func newEchoServer(t *testing.T, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			hits.Add(1)
		}
		raw, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(echoedRequest{
			Path: r.URL.Path,
			Body: string(raw),
			Guid: r.Header.Get("X-Guid"),
			Rand: r.Header.Get("X-Rand"),
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// sendFromTUI saves req in a fresh collection, selects it and sends it with
// Alt+Enter. It returns the model after the response or error is handled.
func sendFromTUI(t *testing.T, st *store.Store, col *domain.Collection, req *domain.Request) tui.Model {
	t.Helper()
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	m := newE2EModel(t, st, exec.New(transport))
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	msg := runCmd(t, cmd)
	require.NotNil(t, msg)
	m = callUpdate(t, m, msg)
	return m
}

func TestE2E_DynamicVariablesAndEscapesAreSentFromTUI(t *testing.T) {
	srv := newEchoServer(t, nil)
	col := &domain.Collection{ID: uuid.NewString(), Name: "Dynamic"}
	st := setupStore(t, col)
	req := &domain.Request{
		ID:           uuid.NewString(),
		CollectionID: col.ID,
		Name:         "Mint",
		Method:       http.MethodPost,
		URL:          srv.URL + "/items/{{$guid}}/{{$guid}}",
		// \{{ is an escape: it is sent as a literal {{.
		Body:    `{"ts":"{{$timestamp}}","iso":"{{$isoTimestamp}}","raw":"\{{name}}"}`,
		Headers: `{"X-Guid":"{{$randomUUID}}","X-Rand":"{{$randomInt}}"}`,
	}
	seedRequests(t, st, col.ID, req)
	before := time.Now().Unix()

	m := sendFromTUI(t, st, col, req)

	require.NoError(t, m.Err())
	require.NotNil(t, m.Response())
	require.Equal(t, 200, m.Response().StatusCode)
	var echo echoedRequest
	require.NoError(t, json.Unmarshal(m.Response().Body, &echo))

	// Each {{$guid}} is evaluated on its own, so the two segments differ.
	parts := regexp.MustCompile(`^/items/([^/]+)/([^/]+)$`).FindStringSubmatch(echo.Path)
	require.Len(t, parts, 3, "path %q", echo.Path)
	first, err := uuid.Parse(parts[1])
	require.NoError(t, err)
	second, err := uuid.Parse(parts[2])
	require.NoError(t, err)
	assert.NotEqual(t, first, second, "two {{$guid}} in one request must differ")

	// The body keeps its JSON shape; \{{ became {{ and the rest is unchanged.
	var body struct {
		TS  string `json:"ts"`
		ISO string `json:"iso"`
		Raw string `json:"raw"`
	}
	require.NoError(t, json.Unmarshal([]byte(echo.Body), &body), "body %q", echo.Body)
	ts, err := strconv.ParseInt(body.TS, 10, 64)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, ts, before)
	assert.LessOrEqual(t, ts, time.Now().Unix())
	iso, err := time.Parse("2006-01-02T15:04:05.000Z07:00", body.ISO)
	require.NoError(t, err, "$isoTimestamp must be ISO 8601 with milliseconds")
	assert.WithinDuration(t, time.Now(), iso, time.Minute)
	assert.Equal(t, "{{name}}", body.Raw, "an escaped \\{{ is sent as a literal {{")

	// Headers take the same expansion as the URL and body.
	_, err = uuid.Parse(echo.Guid)
	require.NoError(t, err, "X-Guid must hold a UUID")
	n, err := strconv.Atoi(echo.Rand)
	require.NoError(t, err)
	assert.True(t, n >= 0 && n <= 1000, "$randomInt is in [0,1000], got %d", n)
}

func TestE2E_UnresolvedVariableWithoutEnvironmentIsReportedBeforeSending(t *testing.T) {
	var hits atomic.Int32
	srv := newEchoServer(t, &hits)
	col := &domain.Collection{ID: uuid.NewString(), Name: "Missing"}
	st := setupStore(t, col)
	req := &domain.Request{
		ID:           uuid.NewString(),
		CollectionID: col.ID,
		Name:         "Needs token",
		Method:       http.MethodGet,
		URL:          srv.URL + "/{{missing_var}}",
	}
	seedRequests(t, st, col.ID, req)

	m := sendFromTUI(t, st, col, req)

	require.Error(t, m.Err())
	assert.ErrorIs(t, m.Err(), exec.ErrUnresolvedVariable)
	assert.Contains(t, m.Err().Error(), "missing_var")
	assert.Nil(t, m.Response())
	assert.Equal(t, int32(0), hits.Load(), "nothing is sent when a variable is unresolved")
}

func TestE2E_UnresolvedVariableWithActiveEnvironmentNamesTheVariable(t *testing.T) {
	var hits atomic.Int32
	srv := newEchoServer(t, &hits)
	col := &domain.Collection{ID: uuid.NewString(), Name: "Partial"}
	st := setupStore(t, col)
	ctx := context.Background()
	env, err := st.CreateDefaultEnvironment(ctx, col.ID)
	require.NoError(t, err)
	env.SetVars(map[string]string{"host": "api.test"})
	require.NoError(t, st.SaveEnvironment(ctx, env))
	require.NoError(t, st.SetActiveEnvironment(ctx, col.ID, env.ID))
	req := &domain.Request{
		ID:           uuid.NewString(),
		CollectionID: col.ID,
		Name:         "Partial",
		Method:       http.MethodGet,
		URL:          srv.URL + "/{{host}}/{{missing_var}}",
	}
	seedRequests(t, st, col.ID, req)

	m := sendFromTUI(t, st, col, req)

	require.Error(t, m.Err())
	assert.ErrorIs(t, m.Err(), exec.ErrUnresolvedVariable)
	assert.Contains(t, m.Err().Error(), "missing_var")
	assert.NotContains(t, m.Err().Error(), "no environments active",
		"an active environment exists, so the message must not say there is none")
	assert.Equal(t, int32(0), hits.Load())
}

func TestE2E_UnresolvedVariableInAuthConfigFailsBeforeSending(t *testing.T) {
	var hits atomic.Int32
	srv := newEchoServer(t, &hits)
	col := &domain.Collection{ID: uuid.NewString(), Name: "Auth"}
	st := setupStore(t, col)
	req := &domain.Request{
		ID:           uuid.NewString(),
		CollectionID: col.ID,
		Name:         "Keyed",
		Method:       http.MethodGet,
		URL:          srv.URL + "/secure",
		AuthType:     domain.AuthTypeAPIKey,
		AuthConfig:   `{"in":"header","name":"X-Key","value":"{{api_token}}"}`,
	}
	seedRequests(t, st, col.ID, req)

	m := sendFromTUI(t, st, col, req)

	require.Error(t, m.Err())
	assert.ErrorIs(t, m.Err(), exec.ErrUnresolvedVariable)
	assert.Contains(t, m.Err().Error(), "api_token")
	assert.Equal(t, int32(0), hits.Load())
}
