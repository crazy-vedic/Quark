package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewRequestCmd returns the 'quark request' subcommand tree.
func NewRequestCmd(r SearchStore) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "request",
		Short: "Manage requests",
	}
	cmd.AddCommand(newRequestListCmd(r))
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
			reqs, err := r.ListRequests(cmd.Context(), collectionID)
			if err != nil {
				return fmt.Errorf("request list: %w", err)
			}
			if len(reqs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No requests found.")
				return nil
			}
			for _, req := range reqs {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s  %-6s  %s\n",
					req.ID[:8], req.Method, req.Name)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&collectionID, "collection", "", "Collection ID")
	_ = cmd.RegisterFlagCompletionFunc("collection", CompleteCollectionIDs(r.ListCollections))
	return cmd
}
