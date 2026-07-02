package cli

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

// NewImportCmd returns the 'quark import' subcommand tree.
func NewImportCmd(w store.RequestWriter, im *curl.Importer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import requests from external formats",
	}
	cmd.AddCommand(newImportCurlCmd(w, im))
	return cmd
}

func newImportCurlCmd(w store.RequestWriter, im *curl.Importer) *cobra.Command {
	var collectionID, name string
	cmd := &cobra.Command{
		Use:   "curl <curl-command>",
		Short: "Import a curl command as a request",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			curlCmd := strings.Join(args, " ")
			result, err := im.Parse(strings.NewReader(curlCmd))
			if err != nil {
				return fmt.Errorf("import curl: parse: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Method:   %s\nURL:      %s\nSecurity: %s\n",
				result.Method, result.URL, result.Security)
			for _, w := range result.Warnings {
				fmt.Fprintf(cmd.OutOrStdout(), "Warning:  %s\n", w)
			}

			if collectionID == "" || name == "" {
				fmt.Fprintln(cmd.OutOrStdout(), "\n(Dry run: use --collection and --name to save)")
				return nil
			}

			req := &domain.Request{
				ID:           uuid.New().String(),
				CollectionID: collectionID,
				Name:         name,
				Method:       result.Method,
				URL:          result.URL,
			}
			if err := w.SaveRequest(cmd.Context(), req); err != nil {
				return fmt.Errorf("import curl: save: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Saved as %q (%s)\n", name, req.ID[:8])
			return nil
		},
	}
	cmd.Flags().StringVar(&collectionID, "collection", "", "Target collection ID")
	cmd.Flags().StringVar(&name, "name", "", "Request name")
	return cmd
}
