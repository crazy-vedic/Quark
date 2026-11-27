package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/har"
	"github.com/crazy-vedic/quark/internal/store"
)

// ExportHARStore is what 'export-har' needs from the store.
type ExportHARStore interface {
	store.CollectionLister
	store.RequestReader
	ResolveRequestPath(ctx context.Context, reference string) (*domain.Request, error)
	store.ExecutionReader
}

// NewExportHARCmd returns the 'quark export-har' command, which prints the
// execution history of one request as a HAR 1.2 document. version is written
// to the document's creator field.
func NewExportHARCmd(st ExportHARStore, version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export-har <Collection/Request>",
		Short: "Print a request's execution history as a HAR 1.2 document",
		Long: "Print the execution history of a saved request as a HAR 1.2 document, oldest\n" +
			"entry first. Each entry shows what was sent, including the auth header or query\n" +
			"parameter, and what came back. Credentials are written as plain text.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			req, err := st.ResolveRequestPath(ctx, args[0])
			if err != nil {
				return fmt.Errorf("export-har: resolve request: %w", err)
			}
			execs, err := st.ListExecutionsByRequest(ctx, req.ID)
			if err != nil {
				return fmt.Errorf("export-har: list executions: %w", err)
			}
			if len(execs) == 0 {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"No execution history for %q; send the request first.\n", args[0])
			}
			log, err := har.FromExecutions(execs, version)
			if err != nil {
				return fmt.Errorf("export-har: %w", err)
			}
			data, err := har.Encode(log)
			if err != nil {
				return fmt.Errorf("export-har: %w", err)
			}
			if _, err := cmd.OutOrStdout().Write(data); err != nil {
				return fmt.Errorf("export-har: write: %w", err)
			}
			return nil
		},
	}
	cmd.ValidArgsFunction = CompleteRequestPaths(st.ListCollections, st.ListRequests)
	return cmd
}
