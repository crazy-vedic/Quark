package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

// CurlCertificateSaver persists host-scoped TLS metadata discovered during a
// cURL import. It is optional for dry runs but required when saving a request
// that contains certificate options.
type CurlCertificateSaver func(context.Context, *curl.CertificateSpec, string) error

// NewImportCmd returns the 'quark import' subcommand tree.
func NewImportCmd(w store.RequestWriter, im *curl.Importer, certificateSavers ...CurlCertificateSaver) *cobra.Command {
	var certificateSaver CurlCertificateSaver
	if len(certificateSavers) > 0 {
		certificateSaver = certificateSavers[0]
	}
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import requests from external formats",
	}
	cmd.AddCommand(newImportCurlCmd(w, im, certificateSaver))
	return cmd
}

func newImportCurlCmd(w store.RequestWriter, im *curl.Importer, certificateSavers ...CurlCertificateSaver) *cobra.Command {
	var certificateSaver CurlCertificateSaver
	if len(certificateSavers) > 0 {
		certificateSaver = certificateSavers[0]
	}
	var collectionID, name string
	cmd := &cobra.Command{
		Use:   "curl <quoted-curl-command>",
		Short: "Import a curl command as a request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			curlCmd := args[0]
			result, err := im.Parse(strings.NewReader(curlCmd))
			if err != nil {
				return fmt.Errorf("import curl: parse: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Method:   %s\nURL:      %s\nSecurity: %s\n",
				result.Method, result.URL, result.Security)
			for _, w := range result.Warnings {
				fmt.Fprintf(cmd.OutOrStdout(), "Warning:  %s\n", w)
			}
			if result.Certificate != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "mTLS:      %s certificate %s\n", result.Certificate.Type, result.Certificate.File)
			}

			if collectionID == "" || name == "" {
				fmt.Fprintln(cmd.OutOrStdout(), "\n(Dry run: use --collection and --name to save)")
				return nil
			}
			if w == nil {
				return fmt.Errorf("import curl: request storage is unavailable")
			}
			headersJSON, err := json.Marshal(result.Headers)
			if err != nil {
				return fmt.Errorf("import curl: encode headers: %w", err)
			}
			req := &domain.Request{
				ID:           uuid.New().String(),
				CollectionID: collectionID,
				Name:         name,
				Method:       result.Method,
				URL:          result.URL,
				Headers:      string(headersJSON),
				Body:         result.Body,
			}
			if err := w.SaveRequest(cmd.Context(), req); err != nil {
				return fmt.Errorf("import curl: save: %w", err)
			}
			if result.Certificate != nil {
				if certificateSaver == nil {
					_ = w.DeleteRequest(cmd.Context(), req.ID)
					return fmt.Errorf("import curl: saving mTLS options is unavailable in this command context")
				}
				if err := certificateSaver(cmd.Context(), result.Certificate, result.URL); err != nil {
					_ = w.DeleteRequest(cmd.Context(), req.ID)
					return fmt.Errorf("import curl: save mTLS configuration: %w", err)
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Saved as %q (%s)\n", name, req.ID[:8])
			return nil
		},
	}
	cmd.Flags().StringVar(&collectionID, "collection", "", "Target collection ID")
	cmd.Flags().StringVar(&name, "name", "", "Request name")
	return cmd
}
