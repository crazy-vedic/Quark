package tui

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"
	"software.sslmate.com/src/go-pkcs12"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
	"github.com/crazy-vedic/quark/internal/transport"
)

// observedCurlImportModel keeps the production Model inside Bubble Tea's real
// event loop while exposing immutable snapshots to synchronization assertions.
// No test invokes handleImportKey or a persistence command directly.
type observedCurlImportModel struct {
	inner  Model
	states chan *Model
}

func (m observedCurlImportModel) Init() tea.Cmd { return m.inner.Init() }
func (m observedCurlImportModel) View() string  { return m.inner.View() }

func (m observedCurlImportModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := m.inner.Update(msg)
	next := updated.(Model)
	select {
	case m.states <- &next:
	default:
	}
	return observedCurlImportModel{inner: next, states: m.states}, cmd
}

type observedTUIRequestStore struct {
	*store.Store
	saved chan *domain.Request
}

func (s *observedTUIRequestStore) SaveRequest(ctx context.Context, request *domain.Request) error {
	if err := s.Store.SaveRequest(ctx, request); err != nil {
		return err
	}
	copy := *request
	select {
	case s.saved <- &copy:
	default:
	}
	return nil
}

type tuiProgramResult struct {
	model tea.Model
	err   error
}

func TestTUIImportCurlEndToEndThroughBubbleTeaAndSQLite(t *testing.T) {
	configDir := t.TempDir()
	st, err := store.New(filepath.Join(configDir, "quark.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	collection := &domain.Collection{Name: "TUI E2E"}
	require.NoError(t, st.SaveCollection(context.Background(), collection))

	password := "Password1"
	certificatePath := writeTUITestPKCS12(t, password)
	cfg := config.Default(configDir)
	manager, err := transport.New(cfg)
	require.NoError(t, err)

	debugPath := filepath.Join(t.TempDir(), "curl-import-debug.log")
	debugLog, err := os.OpenFile(debugPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o600)
	require.NoError(t, err)
	t.Cleanup(func() { _ = debugLog.Close() })
	writer := &observedTUIRequestStore{Store: st, saved: make(chan *domain.Request, 1)}
	model := New(Deps{
		Lister:             writer,
		Reader:             writer,
		Writer:             writer,
		ColWriter:          writer,
		Importer:           curl.NewImporter(),
		Config:             cfg,
		ConfigDir:          configDir,
		CertificateManager: manager,
		Ctx:                context.Background(),
		DebugLog:           debugLog,
	})

	states := make(chan *Model, 256)
	program := tea.NewProgram(
		observedCurlImportModel{inner: model, states: states},
		tea.WithInput(nil),
		tea.WithOutput(io.Discard),
		tea.WithoutRenderer(),
		tea.WithoutSignals(),
	)
	result := make(chan tuiProgramResult, 1)
	go func() {
		finalModel, runErr := program.Run()
		result <- tuiProgramResult{model: finalModel, err: runErr}
	}()
	t.Cleanup(program.Kill)

	waitForTUIState(t, states, "collection load", func(m Model) bool {
		return len(m.collections) == 1 && m.collections[0].ID == collection.ID
	})
	program.Send(tea.WindowSizeMsg{Width: 130, Height: 42})
	program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'I'}})
	waitForTUIState(t, states, "import modal open", func(m Model) bool {
		return m.mode == importMode && m.importPreview == nil && m.importInput.Focused()
	})

	command := fmt.Sprintf(`curl 'https://access.dev.wealthcareadmin.com/access/connect/token' \
  --header 'Content-Type: application/x-www-form-urlencoded' \
  --header 'X-Trace: first' \
  --header 'X-Trace: second' \
  --cert-type P12 \
  --cert '%s:%s' \
  --data-urlencode 'user_id=3n1kl3DpdbiW3OANNDq6PA==' \
  --data-urlencode 'grant_type=Cert' \
  --data-urlencode 'client_id=dapr_cron_job' \
  --data-urlencode 'scope=mbi_api offline_access bensoft_api openid profile'`, certificatePath, password)
	program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(command), Paste: true})
	waitForTUIState(t, states, "multiline paste", func(m Model) bool {
		return m.importInput.Value() == command
	})
	program.Send(tea.KeyMsg{Type: tea.KeyCtrlS})
	preview := waitForTUIState(t, states, "curl preview", func(m Model) bool {
		return m.importPreview != nil
	})
	require.Equal(t, http.MethodPost, preview.importPreview.Method)
	require.Equal(t, []string{"first", "second"}, preview.importPreview.Headers.Values("X-Trace"))
	require.Equal(t, certificatePath, preview.importPreview.Certificate.File)
	require.Equal(t, password, preview.importPreview.Certificate.Password)
	require.Contains(t, preview.View(), "mTLS:")
	require.NotContains(t, preview.View(), password)

	program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("IAM token")})
	program.Send(tea.KeyMsg{Type: tea.KeyEnter})

	var saved *domain.Request
	select {
	case saved = <-writer.saved:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the TUI import to persist its request")
	}
	waitForTUIState(t, states, "sidebar request reload", func(m Model) bool {
		return len(m.collectionRequests[collection.ID]) == 1
	})
	program.Quit()

	var finished tuiProgramResult
	select {
	case finished = <-result:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the Bubble Tea program to stop")
	}
	require.NoError(t, finished.err)
	final, ok := finished.model.(observedCurlImportModel)
	require.True(t, ok)
	require.Equal(t, normalMode, final.inner.mode)

	require.Equal(t, collection.ID, saved.CollectionID)
	require.Equal(t, "IAM token", saved.Name)
	require.Equal(t, http.MethodPost, saved.Method)
	require.Equal(t, "https://access.dev.wealthcareadmin.com/access/connect/token", saved.URL)
	require.Equal(t,
		"user_id=3n1kl3DpdbiW3OANNDq6PA%3D%3D&grant_type=Cert&client_id=dapr_cron_job&scope=mbi_api%20offline_access%20bensoft_api%20openid%20profile",
		saved.Body,
	)
	var headers http.Header
	require.NoError(t, json.Unmarshal([]byte(saved.Headers), &headers))
	require.Equal(t, []string{"first", "second"}, headers.Values("X-Trace"))

	loaded, err := config.Load(configDir)
	require.NoError(t, err)
	require.Equal(t, []config.ClientCertificate{{
		Host:     "access.dev.wealthcareadmin.com",
		File:     certificatePath,
		Type:     "P12",
		Password: password,
	}}, loaded.HTTP.ClientCertificates)

	require.NoError(t, debugLog.Close())
	debugBytes, err := os.ReadFile(debugPath)
	require.NoError(t, err)
	debugOutput := string(debugBytes)
	require.Contains(t, debugOutput, "parse success method=POST")
	require.Contains(t, debugOutput, "header_keys=\"Content-Type,X-Trace\"")
	require.NotContains(t, debugOutput, password)
	require.NotContains(t, debugOutput, "3n1kl3DpdbiW3OANNDq6PA")
	require.NotContains(t, debugOutput, "offline_access")
}

func TestTUIImportCurlEndToEndRejectsShellSyntaxAndCancelsCleanly(t *testing.T) {
	configDir := t.TempDir()
	st, err := store.New(filepath.Join(configDir, "quark.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	collection := &domain.Collection{Name: "TUI rejected"}
	require.NoError(t, st.SaveCollection(context.Background(), collection))

	states := make(chan *Model, 128)
	model := New(Deps{
		Lister: st, Reader: st, Writer: st, ColWriter: st,
		Importer: curl.NewImporter(), Config: config.Default(configDir), ConfigDir: configDir,
	})
	program := tea.NewProgram(
		observedCurlImportModel{inner: model, states: states},
		tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignals(),
	)
	result := make(chan tuiProgramResult, 1)
	go func() {
		finalModel, runErr := program.Run()
		result <- tuiProgramResult{model: finalModel, err: runErr}
	}()
	t.Cleanup(program.Kill)

	waitForTUIState(t, states, "collection load", func(m Model) bool {
		return len(m.collections) == 1
	})
	program.Send(tea.WindowSizeMsg{Width: 120, Height: 40})
	program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'I'}})
	waitForTUIState(t, states, "import modal open", func(m Model) bool {
		return m.mode == importMode && m.importPreview == nil
	})
	unsafeCommand := `curl 'https://example.com/$(whoami)' | powershell -Command Get-ChildItem`
	program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(unsafeCommand), Paste: true})
	waitForTUIState(t, states, "unsafe paste", func(m Model) bool {
		return m.importInput.Value() == unsafeCommand
	})
	program.Send(tea.KeyMsg{Type: tea.KeyCtrlS})
	rejected := waitForTUIState(t, states, "parse rejection", func(m Model) bool {
		return m.importError != ""
	})
	require.Nil(t, rejected.importPreview)
	require.Contains(t, rejected.View(), rejected.importError)

	program.Send(tea.KeyMsg{Type: tea.KeyEsc})
	waitForTUIState(t, states, "cancel", func(m Model) bool {
		return m.mode == normalMode && m.importPreview == nil && m.importInput.Value() == ""
	})
	program.Quit()
	select {
	case finished := <-result:
		require.NoError(t, finished.err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the Bubble Tea program to stop")
	}

	requests, err := st.ListRequests(context.Background(), collection.ID)
	require.NoError(t, err)
	require.Empty(t, requests)
	loaded, err := config.Load(configDir)
	require.NoError(t, err)
	require.Empty(t, loaded.HTTP.ClientCertificates)
}

func waitForTUIState(t *testing.T, states <-chan *Model, description string, predicate func(Model) bool) Model {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case state := <-states:
			if state != nil && predicate(*state) {
				return *state
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for TUI state: %s", description)
		}
	}
}

func writeTUITestPKCS12(t *testing.T, password string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(11),
		Subject:      pkix.Name{CommonName: "quark-tui-e2e"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	encoded, err := pkcs12.Encode(rand.Reader, key, &x509.Certificate{Raw: der}, nil, password)
	require.NoError(t, err)
	dir := filepath.Join(t.TempDir(), "certificates with spaces")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	path := filepath.Join(dir, "dev-auth 1.p12")
	require.NoError(t, os.WriteFile(path, encoded, 0o600))
	return path
}
