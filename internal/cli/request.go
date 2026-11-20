package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/exec"
)

// NewRequestCmd returns the 'quark request' subcommand tree.
func NewRequestCmd(r SearchStore) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "request",
		Short: "Manage requests",
	}
	cmd.AddCommand(newRequestListCmd(r))
	cmd.AddCommand(newRequestExportCurlCmd(r))
	return cmd
}

func newRequestExportCurlCmd(r SearchStore) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export-curl <Collection/Request>",
		Short: "Print a saved request as a curl command",
		Long: "Print a saved request as a curl command, the same one the TUI copies.\n" +
			"{{variables}} are filled in from the collection's active environment and Global,\n" +
			"as 'quark run' does; a variable with no value is an error.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolver, ok := r.(requestPathResolver)
			if !ok {
				return fmt.Errorf("request export-curl: store cannot resolve request paths")
			}
			req, err := resolver.ResolveRequestPath(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("request export-curl: resolve request: %w", err)
			}
			envs, ok := r.(exec.EnvResolver)
			if !ok {
				return fmt.Errorf("request export-curl: store cannot resolve environments")
			}
			colEnv, globalEnv, err := exec.ResolveEnvVars(cmd.Context(), envs, req.CollectionID)
			if err != nil {
				return fmt.Errorf("request export-curl: resolve environments: %w", err)
			}
			prepared, err := exec.InterpolateRequest(req, colEnv, globalEnv)
			if err != nil {
				return fmt.Errorf("request export-curl: interpolate: %w", err)
			}
			command, err := curl.ExportRequest(prepared)
			if err != nil {
				return fmt.Errorf("request export-curl: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), command)
			return nil
		},
	}
	cmd.ValidArgsFunction = CompleteRequestPaths(r.ListCollections, r.ListRequests)
	return cmd
}

func newRequestListCmd(r SearchStore) *cobra.Command {
	var collectionID string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List requests in a collection",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if collectionID == "" {
				return fmt.Errorf("--collection is required")
			}
			col, err := resolveCollectionReference(cmd.Context(), r, collectionID)
			if err != nil {
				return fmt.Errorf("request list: resolve collection: %w", err)
			}
			reqs, err := r.ListRequests(cmd.Context(), col.ID)
			if err != nil {
				return fmt.Errorf("request list: %w", err)
			}
			if len(reqs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No requests found.")
				return nil
			}
			for _, req := range reqs {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s  %-6s  %s\n",
					shortID(req.ID), req.Method, req.Name)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&collectionID, "collection", "", "Collection ID, name, or nested path")
	_ = cmd.RegisterFlagCompletionFunc("collection", CompleteCollectionIDs(r.ListCollections))
	return cmd
}
