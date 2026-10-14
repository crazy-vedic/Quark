// Package cli implements non-TUI subcommands. Thin wrappers — no business logic.
package cli

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

// CollectionStore is the minimum interface NewCollectionCmd requires.
type CollectionStore interface {
	store.CollectionLister
	store.CollectionWriter
}

// NewCollectionCmd returns the 'quark collection' subcommand tree.
func NewCollectionCmd(s CollectionStore) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "collection",
		Short: "Manage collections",
	}
	cmd.AddCommand(newCollectionListCmd(s))
	cmd.AddCommand(newCollectionCreateCmd(s))
	cmd.AddCommand(newCollectionRenameCmd(s))
	cmd.AddCommand(newCollectionDeleteCmd(s))
	return cmd
}

func newCollectionListCmd(s store.CollectionLister) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all collections",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cols, err := s.ListCollections(cmd.Context())
			if err != nil {
				return fmt.Errorf("collection list: %w", err)
			}
			if len(cols) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No collections found.")
				return nil
			}
			for _, c := range cols {
				name := c.Name
				if resolver, ok := s.(collectionFullPathResolver); ok {
					if fullPath, err := resolver.CollectionPath(cmd.Context(), c.ID); err == nil {
						name = fullPath
					}
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  %s  %s\n", c.ID, name)
			}
			return nil
		},
	}
}

func newCollectionCreateCmd(s store.CollectionWriter) *cobra.Command {
	return &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new collection",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := &domain.Collection{
				ID:   uuid.New().String(),
				Name: args[0],
			}
			if err := s.SaveCollection(cmd.Context(), c); err != nil {
				return fmt.Errorf("collection create: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created collection %q (%s)\n", c.Name, c.ID[:8])
			return nil
		},
	}
}

func newCollectionRenameCmd(s CollectionStore) *cobra.Command {
	return &cobra.Command{
		Use:   "rename <collection> <new-name>",
		Short: "Rename a collection",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			collection, err := resolveCollectionReference(cmd.Context(), s, args[0])
			if err != nil {
				return fmt.Errorf("collection rename: %w", err)
			}
			collection.Name = args[1]
			if err := s.SaveCollection(cmd.Context(), collection); err != nil {
				return fmt.Errorf("collection rename: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Renamed collection %q to %q\n", args[0], collection.Name)
			return nil
		},
	}
}

func newCollectionDeleteCmd(s CollectionStore) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <collection>",
		Short: "Delete a collection and its requests",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			collection, err := resolveCollectionReference(cmd.Context(), s, args[0])
			if err != nil {
				return fmt.Errorf("collection delete: %w", err)
			}
			if err := s.DeleteCollection(cmd.Context(), collection.ID); err != nil {
				return fmt.Errorf("collection delete: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Deleted collection %q\n", collection.Name)
			return nil
		},
	}
}
