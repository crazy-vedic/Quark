package cli

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/domain"
)

type importRequestWriter struct{ saved *domain.Request }

func (w *importRequestWriter) SaveRequest(_ context.Context, request *domain.Request) error {
	w.saved = request
	return nil
}

func (w *importRequestWriter) DeleteRequest(context.Context, string) error { return nil }

type failingImportRequestWriter struct{ deleted string }

func (w *failingImportRequestWriter) SaveRequest(context.Context, *domain.Request) error {
	return errors.New("save failed")
}
func (w *failingImportRequestWriter) DeleteRequest(_ context.Context, id string) error {
	w.deleted = id
	return nil
}

func TestImportCurlPersistsHeadersAndBody(t *testing.T) {
	writer := &importRequestWriter{}
	cmd := newImportCurlCmd(writer, curl.NewImporter())
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{
		`curl -H 'X-Tag: one' -H 'X-Tag: two' --data-urlencode 'q=two words' https://example.com`,
		"--collection", "collection-1", "--name", "Imported",
	})

	require.NoError(t, cmd.Execute())
	require.NotNil(t, writer.saved)
	require.Equal(t, "q=two%20words", writer.saved.Body)
	require.JSONEq(t, `{"Content-Type":["application/x-www-form-urlencoded"],"X-Tag":["one","two"]}`, writer.saved.Headers)
}

func TestImportCurlPersistsCertificateThroughConfiguredSaver(t *testing.T) {
	writer := &importRequestWriter{}
	var savedSpec *curl.CertificateSpec
	var savedURL string
	cmd := newImportCurlCmd(writer, curl.NewImporter(), func(_ context.Context, spec *curl.CertificateSpec, rawURL string) error {
		copy := *spec
		savedSpec = &copy
		savedURL = rawURL
		return nil
	})
	cmd.SetArgs([]string{
		`curl --cert-type P12 --cert 'client.p12:literal' https://example.com`,
		"--collection", "collection-1", "--name", "Imported",
	})

	require.NoError(t, cmd.Execute())
	require.NotNil(t, writer.saved)
	require.Equal(t, "https://example.com", savedURL)
	require.Equal(t, &curl.CertificateSpec{File: "client.p12", Type: "P12", Password: "literal"}, savedSpec)
}

func TestImportCurlSavesRequestBeforeCertificate(t *testing.T) {
	writer := &failingImportRequestWriter{}
	saverCalled := false
	cmd := newImportCurlCmd(writer, curl.NewImporter(), func(context.Context, *curl.CertificateSpec, string) error {
		saverCalled = true
		return nil
	})
	cmd.SetArgs([]string{`curl --cert client.pem https://example.com`, "--collection", "c", "--name", "n"})
	require.Error(t, cmd.Execute())
	require.False(t, saverCalled, "certificate must not be persisted when request save fails")
}
