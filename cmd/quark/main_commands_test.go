package main

import (
	"context"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/search"
	"github.com/crazy-vedic/quark/internal/store"
	"github.com/crazy-vedic/quark/internal/transport"
)

type fakeTeaRunner struct {
	model tea.Model
	err   error
	sent  int
}

type inertTeaModel struct{}

func (inertTeaModel) Init() tea.Cmd                         { return nil }
func (m inertTeaModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }
func (inertTeaModel) View() string                          { return "" }

func (p *fakeTeaRunner) Run() (tea.Model, error) { return p.model, p.err }
func (p *fakeTeaRunner) Send(tea.Msg)            { p.sent++ }

func testRuntime(t *testing.T) (*runtime, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.New(filepath.Join(dir, "quark.db"))
	require.NoError(t, err)
	cfg := config.Default(dir)
	tr, err := transport.New(cfg)
	require.NoError(t, err)
	rt := &runtime{st: st, cfg: cfg, executor: exec.New(http.DefaultTransport, exec.WithExecutionWriter(st)), transport: tr, importer: curl.NewImporter(), searcher: search.New(st)}
	t.Cleanup(func() { _ = st.Close() })
	return rt, st, dir
}

func executeCommand(t *testing.T, cmd interface {
	SetArgs([]string)
	Execute() error
}, args ...string) error {
	t.Helper()
	cmd.(interface{ SetContext(context.Context) }).SetContext(context.Background())
	cmd.SetArgs(args)
	return cmd.Execute()
}

func TestRunEntrypointAndLazyCommands(t *testing.T) {
	require.NoError(t, runWithArgs([]string{"--version"}))
	require.NoError(t, runWithArgs([]string{"--help"}))
	rt, st, dir := testRuntime(t)
	configDir = dir
	collection := &domain.Collection{ID: "main-col", Name: "Main"}
	require.NoError(t, st.SaveCollection(context.Background(), collection))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	req := &domain.Request{ID: "main-req", CollectionID: collection.ID, Name: "Get", Method: "GET", URL: server.URL, Headers: "{}", Body: ""}
	require.NoError(t, st.SaveRequest(context.Background(), req))
	rtf := func() (*runtime, error) { return rt, nil }
	for _, args := range [][]string{{"create", "Temp"}, {"rename", "Temp", "Temp2"}, {"list"}, {"delete", "Temp2"}} {
		require.NoError(t, executeCommand(t, lazyCollectionCmd(rtf), args...))
	}
	require.NoError(t, executeCommand(t, lazyRequestCmd(rtf), "list", "--collection", "Main"))
	require.NoError(t, executeCommand(t, lazySearchCmd(rtf), "Get"))
	require.NoError(t, executeCommand(t, lazyRunCmd(rtf), "Main/Get"))
	require.NoError(t, executeCommand(t, lazyScheduleCmd(rtf), "list"))
	require.NoError(t, executeCommand(t, lazyScheduleCmd(rtf), "run-due"))
	require.NoError(t, executeCommand(t, lazyScheduleCmd(rtf), "add", "Main/Get", "--at", "in 1h"))
	require.NoError(t, executeCommand(t, lazyEnvCmd(rtf), "list"))
	require.NoError(t, executeCommand(t, lazyEnvCmd(rtf), "create", "Main", "dev"))
	require.NoError(t, executeCommand(t, lazyEnvCmd(rtf), "set", "Main", "dev", "api", server.URL))
	require.NoError(t, executeCommand(t, lazyEnvCmd(rtf), "active", "Main", "dev"))
	require.NoError(t, executeCommand(t, lazyEnvCmd(rtf), "delete", "Main", "dev"))
	require.NoError(t, executeCommand(t, lazyKeybindingsCmd(), "list"))
	require.NoError(t, executeCommand(t, lazyKeybindingsCmd(), "set", "search", "ctrl+f"))
	require.NoError(t, executeCommand(t, lazyKeybindingsCmd(), "reset"))
	require.NoError(t, executeCommand(t, lazyClientCertCmd(), "list"))
	certCmd := lazyClientCertCmd()
	require.ErrorContains(t, executeCommand(t, certCmd, "set", "API.EXAMPLE.TEST", "--file", "client.pem", "--key-file", "key.pem", "--password-env", "CERT_PASSWORD"), "validate certificate")
	require.NoError(t, config.SaveClientCertificates(dir, []config.ClientCertificate{{Host: "api.example.test"}}))
	require.NoError(t, executeCommand(t, lazyClientCertCmd(), "list"))
	require.NoError(t, executeCommand(t, lazyClientCertCmd(), "delete", "api.example.test"))
	require.NoError(t, executeCommand(t, newThemeCmd(), "list"))
	require.NoError(t, executeCommand(t, newThemeCmd(), "set", "dark"))
	require.NoError(t, executeCommand(t, newThemeCmd(), "validate"))
	require.NoError(t, executeCommand(t, newThemeCmd(), "validate", "light"))

	require.NoError(t, executeCommand(t, lazyImportCmd(rtf), "curl", `curl -X GET https://example.test/health`, "--collection", collection.ID, "--name", "Health"))
	postman := filepath.Join(t.TempDir(), "collection.json")
	require.NoError(t, os.WriteFile(postman, []byte(`{"info":{"name":"Postman Import","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},"item":[]}`), 0o600))
	require.NoError(t, executeCommand(t, lazyImportPostmanCmd(rtf), postman))

	completionCmd := lazyRunCmd(rtf)
	completionCmd.SetContext(context.Background())
	completions, _ := completionCmd.ValidArgsFunction(completionCmd, nil, "Main/")
	require.NotNil(t, completions)
	scheduleCmd := lazyScheduleCmd(rtf).Commands()[0]
	scheduleCmd.SetContext(context.Background())
	completions, _ = scheduleCmd.ValidArgsFunction(scheduleCmd, nil, "Main/")
	require.NotNil(t, completions)
	searchCmd := lazySearchCmd(func() (*runtime, error) { return nil, errors.New("runtime") })
	searchCmd.SetContext(context.Background())
	err := searchCmd.RunE(searchCmd, []string{"x"})
	require.ErrorContains(t, err, "runtime")
	requestCmd := lazyRequestCmd(func() (*runtime, error) { return nil, errors.New("runtime") }).Commands()[0]
	requestCmd.SetContext(context.Background())
	completion, found := requestCmd.GetFlagCompletionFunc("collection")
	require.True(t, found)
	completions, directive := completion(requestCmd, nil, "")
	require.Nil(t, completions)
	require.NotZero(t, directive)
}

func TestDefaultRootCommandInitializesRuntimeAndLaunchesTUI(t *testing.T) {
	dir := t.TempDir()
	oldRunner := newTeaProgram
	newTeaProgram = func(model tea.Model, _ ...tea.ProgramOption) teaProgramRunner {
		return &fakeTeaRunner{model: model}
	}
	t.Cleanup(func() { newTeaProgram = oldRunner })
	oldDim, oldTiming := dimFlag, timingMode
	dimFlag, timingMode = "", "off"
	t.Cleanup(func() { dimFlag, timingMode = oldDim, oldTiming })
	require.NoError(t, runWithArgs([]string{"--config", dir}))
	_, err := os.Stat(filepath.Join(dir, "quark.db"))
	require.NoError(t, err)
	for _, args := range [][]string{
		{"collection", "list"},
		{"collection", "create", "Root Collection"},
		{"request", "list", "--collection", "Root Collection"},
		{"search", "absent"},
		{"schedule", "list"},
		{"env", "list"},
		{"cert", "list"},
		{"keybindings", "list"},
		{"theme", "list"},
	} {
		fullArgs := append([]string{"--config", dir}, args...)
		require.NoError(t, runWithArgs(fullArgs))
	}
	postman := filepath.Join(t.TempDir(), "collection.json")
	require.NoError(t, os.WriteFile(postman, []byte(`{"info":{"name":"Root Import","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},"item":[]}`), 0o600))
	require.NoError(t, runWithArgs([]string{"--config", dir, "import-postman", postman}))
	oldDebugFactory := debugLogFactory
	debugDir := filepath.Join(t.TempDir(), "debug")
	debugLogFactory = func() *os.File { return openDebugLogAt(debugDir) }
	require.NoError(t, runWithArgs([]string{"--config", dir, "--debug", "theme", "list"}))
	debugLogFactory = oldDebugFactory
	for _, flags := range [][]string{{"--dim", "invalid"}, {"--timing", "invalid"}, {"--theme", "missing-theme"}} {
		args := append([]string{"--config", t.TempDir()}, flags...)
		args = append(args, "tui")
		require.Error(t, runWithArgs(args))
	}
	configFile := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(configFile, []byte("file"), 0o600))
	require.ErrorContains(t, runWithArgs([]string{"--config", configFile, "collection", "list"}), "create config dir")
}

func TestLazyCommandRuntimeErrorsAndEnvironmentCompletions(t *testing.T) {
	runtimeFailure := func() (*runtime, error) { return nil, errors.New("runtime unavailable") }
	call := func(cmd *cobra.Command, args ...string) {
		t.Helper()
		require.NotNil(t, cmd.RunE, cmd.Name())
		cmd.SetContext(context.Background())
		require.ErrorContains(t, cmd.RunE(cmd, args), "runtime unavailable")
	}
	for _, cmd := range []*cobra.Command{
		lazyCollectionCmd(runtimeFailure).Commands()[0],
		lazyRequestCmd(runtimeFailure).Commands()[0],
		lazyRunCmd(runtimeFailure),
		lazySearchCmd(runtimeFailure),
		lazyScheduleCmd(runtimeFailure).Commands()[0],
		lazyScheduleCmd(runtimeFailure).Commands()[1],
		lazyScheduleCmd(runtimeFailure).Commands()[2],
		lazyImportCmd(runtimeFailure).Commands()[0],
		lazyImportPostmanCmd(runtimeFailure),
	} {
		call(cmd, "example")
	}
	for _, cmd := range lazyEnvCmd(runtimeFailure).Commands() {
		call(cmd, "example")
	}

	rt, _, _ := testRuntime(t)
	for _, cmd := range lazyEnvCmd(func() (*runtime, error) { return rt, nil }).Commands() {
		if cmd.ValidArgsFunction == nil {
			continue
		}
		cmd.SetContext(context.Background())
		_, _ = cmd.ValidArgsFunction(cmd, nil, "")
		if len(cmd.Commands()) > 0 {
			for _, child := range cmd.Commands() {
				if child.ValidArgsFunction != nil {
					_, _ = child.ValidArgsFunction(child, nil, "")
				}
			}
		}
	}
}

func TestImportAndCertificateHelpers(t *testing.T) {
	rt, st, dir := testRuntime(t)
	configDir = dir
	imp := tuiPostmanImporter{st: st}
	_, err := imp.ImportPostman(context.Background(), filepath.Join(t.TempDir(), "missing.json"))
	require.Error(t, err)
	require.ErrorContains(t, saveRuntimeImportedCertificate(rt, &curl.CertificateSpec{Type: "DER"}, "https://example.test"), "unsupported certificate type")
	require.ErrorContains(t, saveRuntimeImportedCertificate(rt, &curl.CertificateSpec{}, "not-a-url"), "hostname")
	certServer := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer certServer.Close()
	caPath := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certServer.Certificate().Raw}), 0o600))
	require.NoError(t, saveRuntimeImportedCertificate(rt, &curl.CertificateSpec{CAFile: caPath}, "https://Example.Test/path"))
	require.NoError(t, saveRuntimeImportedCertificate(rt, &curl.CertificateSpec{CAFile: caPath}, "https://example.test/other"))
	require.Len(t, rt.cfg.HTTP.ClientCertificates, 1)
	postman := filepath.Join(t.TempDir(), "collection.json")
	require.NoError(t, os.WriteFile(postman, []byte(`{"info":{"name":"Import","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},"item":[]}`), 0o600))
	_, err = imp.ImportPostman(context.Background(), postman)
	require.NoError(t, err)
}

func TestLaunchTUIValidationAndRunnerResults(t *testing.T) {
	_, st, dir := testRuntime(t)
	cfg := config.Default(dir)
	originalDim, originalTiming, originalDebug := dimFlag, timingMode, debugMode
	t.Cleanup(func() { dimFlag, timingMode, debugMode = originalDim, originalTiming, originalDebug })
	newTeaProgramOld := newTeaProgram
	t.Cleanup(func() { newTeaProgram = newTeaProgramOld })
	ctx := context.Background()
	dimFlag = "bad"
	require.Error(t, launchTUI(ctx, st, nil, nil, nil, nil, cfg, nil, dir))
	dimFlag = ""
	timingMode = "bad"
	require.Error(t, launchTUI(ctx, st, nil, nil, nil, nil, cfg, nil, dir))
	timingMode = "off"
	debugMode = false
	newTeaProgram = func(model tea.Model, _ ...tea.ProgramOption) teaProgramRunner { return &fakeTeaRunner{model: model} }
	require.NoError(t, launchTUI(ctx, st, nil, nil, nil, nil, cfg, nil, dir))
	debugMode = true
	timingMode = "flat"
	require.NoError(t, launchTUI(ctx, st, nil, nil, nil, nil, cfg, nil, dir))
	debugMode = false
	newTeaProgram = func(model tea.Model, _ ...tea.ProgramOption) teaProgramRunner {
		return &fakeTeaRunner{model: model, err: tea.ErrProgramKilled}
	}
	require.NoError(t, launchTUI(ctx, st, nil, nil, nil, nil, cfg, nil, dir))
	newTeaProgram = func(model tea.Model, _ ...tea.ProgramOption) teaProgramRunner {
		return &fakeTeaRunner{model: model, err: errors.New("terminal")}
	}
	require.ErrorContains(t, launchTUI(ctx, st, nil, nil, nil, nil, cfg, nil, dir), "tui: terminal")
}

func TestDebugLogAndConfigDirHelpers(t *testing.T) {
	dir := t.TempDir()
	previousLogDir := debugLogDirectory
	debugLogDirectory = dir
	t.Cleanup(func() { debugLogDirectory = previousLogDir })
	viaDefault := openDebugLog()
	require.NotNil(t, viaDefault)
	require.NoError(t, viaDefault.Close())
	f := openDebugLogAt(dir)
	require.NotNil(t, f)
	require.NoError(t, f.Close())
	second := openDebugLogAt(dir)
	require.NotNil(t, second)
	require.NoError(t, second.Close())
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(entries), 2)
	previousArgs := os.Args
	os.Args = []string{"quark", "--version"}
	require.NoError(t, run())
	os.Args = previousArgs
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	path, err := defaultConfigDir()
	if err == nil {
		require.Contains(t, path, ".quark")
	}
	require.NotNil(t, newTeaProgram(inertTeaModel{}))
}

func TestDebugModeRunsPersistentPreRun(t *testing.T) {
	dir := t.TempDir()
	opened := false
	previousFactory := debugLogFactory
	debugLogFactory = func() *os.File {
		opened = true
		return openDebugLogAt(filepath.Join(dir, "debug"))
	}
	t.Cleanup(func() { debugLogFactory = previousFactory })
	require.NoError(t, runWithArgs([]string{"--debug", "--config", dir, "collection", "list"}))
	require.True(t, opened)
}
