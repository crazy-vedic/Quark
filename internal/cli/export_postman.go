package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/postman"
	"github.com/crazy-vedic/quark/internal/store"
)

// ExportPostmanStore is what 'export-postman' needs from the store.
type ExportPostmanStore interface {
	store.CollectionLister
	store.RequestReader
	ListCollectionEnvironments(
		ctx context.Context,
		collectionID string,
	) ([]*domain.Environment, error)
}

// NewExportPostmanCmd returns the 'quark export-postman' command, which prints
// a collection as a Postman Collection v2.1 JSON document.
func NewExportPostmanCmd(st ExportPostmanStore) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export-postman <collection>",
		Short: "Export a collection as a Postman Collection v2.1 JSON document",
		Long: "Print a collection, its folders and its requests as a Postman Collection v2.1\n" +
			"document. The default environment's variables become collection variables.\n" +
			"Auth is written as Postman auth blocks. Save the output to a file, or import it\n" +
			"again with 'quark import-postman'. Secrets are written as plain text.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			col, err := resolveCollectionReference(ctx, st, args[0])
			if err != nil {
				return fmt.Errorf("export-postman: %w", err)
			}
			root, err := loadExportFolder(ctx, st, col)
			if err != nil {
				return fmt.Errorf("export-postman: %w", err)
			}
			vars, err := defaultEnvironmentVars(ctx, st, col.ID)
			if err != nil {
				return fmt.Errorf("export-postman: %w", err)
			}
			data, err := postman.ExportCollection(col.Name, col.Description, root, vars)
			if err != nil {
				return fmt.Errorf("export-postman: %w", err)
			}
			if _, err := cmd.OutOrStdout().Write(data); err != nil {
				return fmt.Errorf("export-postman: write: %w", err)
			}
			return nil
		},
	}
	cmd.ValidArgsFunction = CompleteCollectionIDs(st.ListCollections)
	return cmd
}

// loadExportFolder reads col and its nested collections, in the order the
// store lists them, into a folder tree.
func loadExportFolder(
	ctx context.Context,
	st ExportPostmanStore,
	col *domain.Collection,
) (*postman.ExportFolder, error) {
	all, err := st.ListCollections(ctx)
	if err != nil {
		return nil, fmt.Errorf("list collections: %w", err)
	}
	children := map[string][]*domain.Collection{}
	for _, c := range all {
		children[c.ParentID] = append(children[c.ParentID], c)
	}
	return loadFolder(ctx, st, col, children, map[string]bool{})
}

func loadFolder(
	ctx context.Context,
	st ExportPostmanStore,
	col *domain.Collection,
	children map[string][]*domain.Collection,
	seen map[string]bool,
) (*postman.ExportFolder, error) {
	if seen[col.ID] {
		return nil, fmt.Errorf("collection %q: cycle in collection tree", col.Name)
	}
	seen[col.ID] = true
	reqs, err := st.ListRequests(ctx, col.ID)
	if err != nil {
		return nil, fmt.Errorf("list requests of %q: %w", col.Name, err)
	}
	folder := &postman.ExportFolder{Name: col.Name, Requests: reqs}
	for _, child := range children[col.ID] {
		sub, err := loadFolder(ctx, st, child, children, seen)
		if err != nil {
			return nil, err
		}
		folder.Folders = append(folder.Folders, sub)
	}
	return folder, nil
}

// defaultEnvironmentVars returns the variables of the collection's default
// environment, or none when it has no default environment.
func defaultEnvironmentVars(
	ctx context.Context,
	st ExportPostmanStore,
	collectionID string,
) (map[string]string, error) {
	envs, err := st.ListCollectionEnvironments(ctx, collectionID)
	if err != nil {
		return nil, fmt.Errorf("list environments: %w", err)
	}
	for _, env := range envs {
		if env.Name != cliDefaultEnvironmentName {
			continue
		}
		vars, err := env.DecodeVars()
		if err != nil {
			return nil, fmt.Errorf("environment %q: %w", env.Name, err)
		}
		return vars, nil
	}
	return nil, nil
}
