package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/crazy-vedic/quark/internal/search"
	"github.com/crazy-vedic/quark/internal/store"
)

// SearchStore is the minimum interface NewSearchCmd requires.
type SearchStore interface {
	store.CollectionLister
	store.RequestReader
}

func NewSearchCmd(st SearchStore) *cobra.Command {
	return &cobra.Command{
		Use:   "search <query>",
		Short: "Search saved requests",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cols, err := st.ListCollections(ctx)
			if err != nil {
				return fmt.Errorf("search: list collections: %w", err)
			}
			ids := make([]string, 0, len(cols))
			names := make(map[string]string, len(cols))
			for _, col := range cols {
				ids = append(ids, col.ID)
				names[col.ID] = col.Name
			}

			result, err := search.New(st).SearchAll(ctx, args[0], ids)
			if err != nil {
				return fmt.Errorf("search: %w", err)
			}
			if len(result.Hits) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No requests found.")
				return nil
			}
			for _, hit := range result.Hits {
				req := hit.Request
				fmt.Fprintf(
					cmd.OutOrStdout(),
					"%.2f  %s/%s  %-6s  %s\n",
					hit.Score,
					names[req.CollectionID],
					req.Name,
					req.Method,
					req.URL,
				)
			}
			return nil
		},
	}
}
