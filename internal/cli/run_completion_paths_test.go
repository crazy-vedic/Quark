package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

type pathRunStore struct {
	*fakeRunStore
	resolve            *domain.Request
	resolveErr         error
	listCollectionsErr error
	listRequestsErr    error
}

func (s *pathRunStore) ResolveRequestPath(context.Context, string) (*domain.Request, error) {
	return s.resolve, s.resolveErr
}
func (s *pathRunStore) ListCollections(ctx context.Context) ([]*domain.Collection, error) {
	if s.listCollectionsErr != nil {
		return nil, s.listCollectionsErr
	}
	return s.fakeRunStore.ListCollections(ctx)
}
func (s *pathRunStore) ListRequests(ctx context.Context, id string) ([]*domain.Request, error) {
	if s.listRequestsErr != nil {
		return nil, s.listRequestsErr
	}
	return s.fakeRunStore.ListRequests(ctx, id)
}

type fallbackRunStore struct {
	*fakeRunStore
	collectionErr error
	requestsErr   error
}

func (s *fallbackRunStore) ListCollections(ctx context.Context) ([]*domain.Collection, error) {
	if s.collectionErr != nil {
		return nil, s.collectionErr
	}
	return s.fakeRunStore.ListCollections(ctx)
}
func (s *fallbackRunStore) ListRequests(ctx context.Context, id string) ([]*domain.Request, error) {
	if s.requestsErr != nil {
		return nil, s.requestsErr
	}
	return s.fakeRunStore.ListRequests(ctx, id)
}

func TestNewRunCmdResolverAndLookupErrors(t *testing.T) {
	base := &fakeRunStore{collections: []*domain.Collection{{ID: "c", Name: "C"}}, requests: map[string][]*domain.Request{"c": {{ID: "r", CollectionID: "c", Name: "R", Method: "GET", URL: "https://example.test"}}}}
	for _, tt := range []struct {
		name string
		st   RunStore
		arg  string
		want string
	}{
		{"bad format", base, "missing-slash", "must be 'Collection/Request Name'"},
		{"collection error", &fallbackRunStore{fakeRunStore: base, collectionErr: errors.New("list")}, "C/R", "list collections"},
		{"collection missing", &fakeRunStore{}, "C/R", "collection \"C\" not found"},
		{"request error", &fallbackRunStore{fakeRunStore: base, requestsErr: errors.New("list")}, "C/R", "list requests"},
		{"request missing", base, "C/missing", "request \"missing\" not found"},
		{"resolver error", &pathRunStore{fakeRunStore: base, resolveErr: errors.New("resolve")}, "C/R", "resolve request"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewRunCmd(tt.st, exec.New(nil))
			cmd.SetArgs([]string{tt.arg})
			err := cmd.Execute()
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestNewRunCmdResolverExecutesAndRejectsOverrides(t *testing.T) {
	global := &domain.Environment{ID: "global", Name: "Global", Data: "{}"}
	st := &pathRunStore{fakeRunStore: &fakeRunStore{collections: []*domain.Collection{{ID: "c", Name: "C"}}, globalEnv: global}, resolve: &domain.Request{ID: "r", CollectionID: "c", Method: "GET", URL: "https://example.test"}}
	cmd := NewRunCmd(st, exec.New(nil))
	cmd.SetArgs([]string{"C/R", "--var", "broken"})
	require.ErrorContains(t, cmd.Execute(), "expected key=value")
	roundTrip := &recordingRoundTripper{response: &http.Response{StatusCode: 200, Status: "200 OK", Header: http.Header{}, Body: io.NopCloser(strings.NewReader("ok"))}}
	cmd = NewRunCmd(st, exec.New(roundTrip))
	cmd.SetArgs([]string{"C/R"})
	require.NoError(t, cmd.Execute())
	require.Equal(t, "https://example.test", roundTrip.lastURL)
}

func TestRunOutputBranches(t *testing.T) {
	cmd := &cobra.Command{}
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	req := &domain.Request{Method: "GET", URL: "https://example.test", Headers: `{"Authorization":"secret"}`, AuthType: "bearer", AuthConfig: `{"token":"secret"}`, Body: "payload"}
	result := &exec.ExecuteResult{Status: "200 OK", Size: 0, Headers: http.Header{"X-Z": []string{"z"}, "X-A": []string{"a"}}}
	require.NoError(t, printRunResult(cmd, req, result, true))
	require.Contains(t, errOut.String(), "Request body")
	require.Contains(t, errOut.String(), "< X-A: a")
	for _, tc := range []struct {
		body, ct string
		terminal bool
		want     string
	}{
		{"", "text/plain", false, ""}, {"{\"ok\":true}", "application/json", false, "\"ok\": true"}, {string([]byte{0, 1}), "application/octet-stream", true, "binary response body omitted"}, {"bad", "application/json", false, "bad"},
	} {
		var b bytes.Buffer
		require.NoError(t, writeResponseBody(&b, []byte(tc.body), tc.ct, tc.terminal))
		require.Contains(t, b.String(), tc.want)
	}
	oldTerminalProbe := terminalProbe
	terminalProbe = func(io.Reader) bool { return true }
	t.Cleanup(func() { terminalProbe = oldTerminalProbe })
	textPath := filepath.Join(t.TempDir(), "large-response")
	require.NoError(t, os.WriteFile(textPath, []byte("response body"), 0o600))
	var terminalOut bytes.Buffer
	terminalCmd := &cobra.Command{}
	terminalCmd.SetOut(&terminalOut)
	large := &exec.ExecuteResult{Status: "200 OK", Size: (16 << 20) + 1, Headers: http.Header{"Content-Type": []string{"text/plain"}}, TempPath: textPath}
	require.NoError(t, printRunResult(terminalCmd, req, large, false))
	require.Contains(t, terminalOut.String(), "large response body")

	binaryPath := filepath.Join(t.TempDir(), "binary-response")
	require.NoError(t, os.WriteFile(binaryPath, []byte{0, 1, 2, 3}, 0o600))
	terminalOut.Reset()
	binaryResult := &exec.ExecuteResult{Status: "200 OK", Size: 4, Headers: http.Header{"Content-Type": []string{"application/octet-stream"}}, TempPath: binaryPath}
	require.NoError(t, printRunResult(terminalCmd, req, binaryResult, false))
	require.Contains(t, terminalOut.String(), "binary response body omitted")
	require.True(t, binaryResponse([]byte("file"), "application/pdf"))
	require.True(t, binaryResponse([]byte{0}, ""))
	for _, tc := range []struct {
		body, contentType, want string
	}{
        {`{"file":true}`, "application/json", "file"},
		{"plain file", "text/plain", "plain file"},
	} {
		path := filepath.Join(t.TempDir(), "response")
		require.NoError(t, os.WriteFile(path, []byte(tc.body), 0o600))
		var b bytes.Buffer
		fileCmd := &cobra.Command{}
		fileCmd.SetOut(&b)
		result := &exec.ExecuteResult{Status: "200 OK", Size: int64(len(tc.body)), Headers: http.Header{"Content-Type": []string{tc.contentType}}, TempPath: path}
		require.NoError(t, printRunResult(fileCmd, req, result, false))
		require.Contains(t, b.String(), tc.want)
	}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "keep"), []byte("x"), 0o600))
	bad := &exec.ExecuteResult{Status: "500", Size: 3, TempPath: dir}
	err := printRunResult(cmd, req, bad, false)
	require.ErrorContains(t, err, "read response body")
	require.ErrorContains(t, err, "cleanup response body")
	missing := &exec.ExecuteResult{Status: "200", Size: 1, TempPath: filepath.Join(t.TempDir(), "missing")}
	require.ErrorContains(t, printRunResult(cmd, req, missing, false), "read response body")
	_ = safeText(&bytes.Buffer{}, "plain")
}

func TestCompletionPureBranches(t *testing.T) {
	t.Setenv("FISH_VERSION", "3.6")
	sh, err := detectShell()
	require.NoError(t, err)
	require.Equal(t, shellFish, sh)
	t.Setenv("FISH_VERSION", "")
	t.Setenv("ZSH_VERSION", "5.9")
	sh, err = detectShell()
	require.NoError(t, err)
	require.Equal(t, shellZsh, sh)
	t.Setenv("ZSH_VERSION", "")
	t.Setenv("SHELL", "")
	_, err = detectShell()
	require.Error(t, err)
	t.Setenv("SHELL", `C:\bin\unknown`)
	_, err = detectShell()
	require.ErrorContains(t, err, "unsupported shell")
	root := testCompletionRoot(t)
	for _, shell := range []string{shellBash, shellZsh, shellFish, shellPowerShell} {
		for _, noDesc := range []bool{false, true} {
			var out bytes.Buffer
			err := writeCompletionScript(root, shell, &out, noDesc)
			require.NoError(t, err)
			require.NotEmpty(t, out.String())
		}
		_, err := completionPaths(shell, t.TempDir(), "quark")
		require.NoError(t, err)
	}
	_, err = completionPaths("unknown", t.TempDir(), "quark")
	require.Error(t, err)
	err = writeCompletionScript(root, "unknown", io.Discard, false)
	require.Error(t, err)
	require.Contains(t, injectBeforeEndMarker("head\n"+completionMarkerEnd, "extra"), "extra\n"+completionMarkerEnd)
	require.Contains(t, injectBeforeEndMarker("head", "extra"), "head\nextra")
	_ = powershellProfilePath(t.TempDir())
	clearZshCompdump(t.TempDir())
	_, err = appendOrUpdateBlock(filepath.Join(t.TempDir(), "profile"), "marker", "block")
	require.NoError(t, err)
	_ = os.ErrNotExist
}
