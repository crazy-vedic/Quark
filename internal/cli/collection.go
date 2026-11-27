// Package cli implements non-TUI subcommands. Thin wrappers — no business logic.
package cli

import (
	"context"
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
	cmd.AddCommand(newCollectionMoveCmd(s))
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
			fmt.Fprintf(cmd.OutOrStdout(), "Created collection %q (%s)\n", c.Name, shortID(c.ID))
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
			if err := s.RenameCollection(cmd.Context(), collection.ID, args[1]); err != nil {
				return fmt.Errorf("collection rename: %w", err)
			}
			fmt.Fprintf(
				cmd.OutOrStdout(),
				"Renamed collection %q to %q\n",
				args[0],
				args[1],
			)
			return nil
		},
	}
}

func newCollectionDeleteCmd(s CollectionStore) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <collection> --yes",
		Short: "Delete a collection and everything in it",
		Long: "Delete a collection with its nested collections, requests and environments. " +
			"Requires --yes.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return fmt.Errorf(
					"collection delete: refusing to delete %q without --yes; this also removes its nested collections, requests and environments",
					args[0],
				)
			}
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
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm the deletion")
	return cmd
}

// collectionMover is the store operation behind 'collection move'.
type collectionMover interface {
	MoveCollection(ctx context.Context, id, parentID string) error
}

func newCollectionMoveCmd(s CollectionStore) *cobra.Command {
	return &cobra.Command{
		Use:   "move <collection> <new-parent|/>",
		Short: "Move a collection under another collection",
		Long: "Move a collection, with everything in it, under a new parent collection. " +
			"Use / as the new parent to make it a root collection.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mover, ok := s.(collectionMover)
			if !ok {
				return fmt.Errorf("collection move: store cannot move collections")
			}
			ctx := cmd.Context()
			moving, err := resolveCollectionReference(ctx, s, args[0])
			if err != nil {
				return fmt.Errorf("collection move: %w", err)
			}
			parentID := ""
			if args[1] != "/" {
				parent, err := resolveCollectionReference(ctx, s, args[1])
				if err != nil {
					return fmt.Errorf("collection move: new parent: %w", err)
				}
				parentID = parent.ID
			}
			cols, err := s.ListCollections(ctx)
			if err != nil {
				return fmt.Errorf("collection move: %w", err)
			}
			if err := checkCollectionMove(cols, moving, parentID); err != nil {
				return fmt.Errorf("collection move: %w", err)
			}
			if err := mover.MoveCollection(ctx, moving.ID, parentID); err != nil {
				return fmt.Errorf("collection move: %w", err)
			}
			if parentID == "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Moved collection %q to the root\n", args[0])
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Moved collection %q under %q\n", args[0], args[1])
			}
			return nil
		},
	}
}

// checkCollectionMove rejects a move into the collection itself or one of its
// descendants, and a move that would give two siblings the same name. The
// store enforces both as well; checking here gives the reason.
func checkCollectionMove(
	cols []*domain.Collection,
	moving *domain.Collection,
	parentID string,
) error {
	byID := make(map[string]*domain.Collection, len(cols))
	for _, c := range cols {
		byID[c.ID] = c
	}
	where := "the root"
	if parentID != "" {
		parent, ok := byID[parentID]
		if !ok {
			return fmt.Errorf("collection %q not found", parentID)
		}
		where = fmt.Sprintf("%q", parent.Name)
		seen := make(map[string]bool)
		for id := parentID; id != "" && !seen[id]; {
			if id == moving.ID {
				return fmt.Errorf(
					"cannot move %q into itself or one of its descendants",
					moving.Name,
				)
			}
			seen[id] = true
			c, ok := byID[id]
			if !ok {
				break
			}
			id = c.ParentID
		}
	}
	for _, c := range cols {
		if c.ID != moving.ID && c.ParentID == parentID && c.Name == moving.Name {
			return fmt.Errorf("a collection named %q already exists in %s", moving.Name, where)
		}
	}
	return nil
}
