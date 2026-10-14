package cli

import (
	"bytes"
	"errors"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
)

type certificateReloadRecorder struct {
	configs []config.Config
	err     error
}

func (r *certificateReloadRecorder) reload(cfg config.Config) error {
	r.configs = append(r.configs, cfg)
	return r.err
}

func executeCertificateCommand(
	t *testing.T,
	configDir string,
	reload func(config.Config) error,
	args ...string,
) (string, error) {
	t.Helper()
	cmd := NewClientCertificatesCmd(configDir, reload)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestClientCertificateSetListAndDelete(t *testing.T) {
	dir := t.TempDir()
	recorder := &certificateReloadRecorder{}
	out, err := executeCertificateCommand(
		t,
		dir,
		recorder.reload,
		"set",
		"API.Example",
		"--file",
		"client.p12",
		"--password-env",
		"P12_PASSWORD",
		"--ca-file",
		"root.pem",
	)
	require.NoError(t, err)
	require.Contains(t, out, "Saved client certificate for api.example")
	require.Len(t, recorder.configs, 1)
	require.Equal(t, "P12", recorder.configs[0].HTTP.ClientCertificates[0].Type)
	require.Equal(t, "root.pem", recorder.configs[0].HTTP.ClientCertificates[0].CAFile)
	require.Equal(t, "P12_PASSWORD", recorder.configs[0].HTTP.ClientCertificates[0].PasswordEnv)

	out, err = executeCertificateCommand(t, dir, nil, "list")
	require.NoError(t, err)
	require.Contains(t, out, "api.example\tP12\tclient.p12")
	require.Contains(t, out, "password-env=P12_PASSWORD")
	require.NotContains(t, out, "P12_PASSWORD=")

	_, err = executeCertificateCommand(
		t,
		dir,
		recorder.reload,
		"set",
		"api.example",
		"--file",
		"client.pem",
		"--key-file",
		"client.key",
	)
	require.NoError(t, err)
	out, err = executeCertificateCommand(t, dir, nil, "list")
	require.NoError(t, err)
	require.Contains(t, out, "api.example\tPEM\tclient.pem\tkey=client.key")
	require.NotContains(t, out, "root.pem")

	out, err = executeCertificateCommand(t, dir, recorder.reload, "delete", "API.EXAMPLE")
	require.NoError(t, err)
	require.Contains(t, out, "Deleted client certificate for api.example")
	out, err = executeCertificateCommand(t, dir, nil, "list")
	require.NoError(t, err)
	require.Contains(t, out, "No client certificates configured.")
}

func TestClientCertificateRejectsInvalidInputAndFailedReload(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{
		{"set", "api.example"},
		{"set", "api.example", "--file", "client.pem", "--type", "DER"},
		{"set", "api.example", "--key-file", "client.key"},
	} {
		_, err := executeCertificateCommand(t, dir, nil, args...)
		require.Error(t, err)
	}

	recorder := &certificateReloadRecorder{err: errors.New("invalid certificate")}
	_, err := executeCertificateCommand(
		t,
		dir,
		recorder.reload,
		"set",
		"api.example",
		"--file",
		"client.pem",
	)
	require.ErrorContains(t, err, "validate certificate")
	cfg, loadErr := config.Load(dir)
	require.NoError(t, loadErr)
	require.Empty(t, cfg.HTTP.ClientCertificates)

	_, err = executeCertificateCommand(t, dir, nil, "delete", "missing.example")
	require.ErrorContains(t, err, "no client certificate configured")

	// The command remains constructible from a nil-reload CLI context.
	require.IsType(t, &cobra.Command{}, NewClientCertificatesCmd(dir, nil))
}
