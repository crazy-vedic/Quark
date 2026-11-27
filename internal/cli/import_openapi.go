package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/openapi"
	"github.com/crazy-vedic/quark/internal/store"
)

// ImportOpenAPIStore is what 'import-openapi' needs from the store.
type ImportOpenAPIStore interface {
	store.CollectionLister
	BeginTransaction(ctx context.Context) (store.TransactionalWriter, error)
}

// NewImportOpenAPICmd returns the 'quark import-openapi' command.
func NewImportOpenAPICmd(st ImportOpenAPIStore) *cobra.Command {
	var collectionName, onDuplicate string
	cmd := &cobra.Command{
		Use:   "import-openapi <file>",
		Short: "Import an OpenAPI 3.x document (JSON or YAML) as a collection",
		Long: "Import an OpenAPI 3.x document as a collection. Each tag becomes a folder, the\n" +
			"first server becomes the baseUrl variable of the default environment, and path\n" +
			"parameters become {{name}} placeholders. JSON request bodies are generated from\n" +
			"examples or schemas. Constructs that cannot be mapped are listed as warnings.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			action, err := parseOpenAPIDuplicate(onDuplicate)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(args[0]) //nolint:gosec // The user chose the file to import.
			if err != nil {
				return fmt.Errorf("import-openapi: %w", err)
			}
			res, err := openapi.Convert(data)
			if err != nil {
				return fmt.Errorf("import-openapi: %s: %w", args[0], err)
			}
			root := res.Collection
			if collectionName != "" {
				root.Name = collectionName
			}
			outcome, err := saveOpenAPICollection(cmd.Context(), st, root, action)
			if err != nil {
				return fmt.Errorf("import-openapi: %w", err)
			}
			printOpenAPIResult(cmd, outcome, res.Warnings)
			return nil
		},
	}
	cmd.Flags().StringVarP(&collectionName, "collection-name", "n", "",
		"Override the collection name (default: the document's info.title)")
	cmd.Flags().StringVar(&onDuplicate, "on-duplicate", "duplicate",
		"Action when a collection of that name exists: duplicate, replace or skip")
	cmd.ValidArgsFunction = func(
		*cobra.Command, []string, string,
	) ([]cobra.Completion, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveFilterFileExt
	}
	return cmd
}

// openAPIDuplicate says what happens when a root collection has the same name.
type openAPIDuplicate string

const (
	openAPIDuplicateCopy    openAPIDuplicate = "duplicate"
	openAPIDuplicateReplace openAPIDuplicate = "replace"
	openAPIDuplicateSkip    openAPIDuplicate = "skip"
)

func parseOpenAPIDuplicate(s string) (openAPIDuplicate, error) {
	switch a := openAPIDuplicate(s); a {
	case openAPIDuplicateCopy, openAPIDuplicateReplace, openAPIDuplicateSkip:
		return a, nil
	}
	return "", fmt.Errorf("invalid --on-duplicate %q: must be duplicate, replace, or skip", s)
}

// openAPIOutcome summarises one import.
type openAPIOutcome struct {
	name     string
	action   openAPIDuplicate // empty when no collection of that name existed
	skipped  bool
	requests int
	folders  int
}

// saveOpenAPICollection writes root and its folders in one transaction.
func saveOpenAPICollection(
	ctx context.Context,
	st ImportOpenAPIStore,
	root *openapi.Collection,
	action openAPIDuplicate,
) (openAPIOutcome, error) {
	out := openAPIOutcome{
		name:     root.Name,
		requests: root.CountRequests(),
		folders:  countFolders(root),
	}
	// Read before the transaction opens: the store uses one SQLite connection.
	all, err := st.ListCollections(ctx)
	if err != nil {
		return out, fmt.Errorf("list collections: %w", err)
	}
	var existingID string
	used := map[string]bool{}
	for _, col := range all {
		if col.ParentID != "" {
			continue
		}
		used[col.Name] = true
		if col.Name == root.Name {
			existingID = col.ID
		}
	}
	if existingID != "" {
		out.action = action
		switch action {
		case openAPIDuplicateSkip:
			out.skipped = true
			return out, nil
		case openAPIDuplicateCopy:
			out.name = uniqueOpenAPIName(root.Name, used)
			root.Name = out.name
		}
	}

	tx, err := st.BeginTransaction(ctx)
	if err != nil {
		return out, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if existingID != "" && action == openAPIDuplicateReplace {
		if err := tx.DeleteCollection(ctx, existingID); err != nil {
			return out, fmt.Errorf("delete existing collection: %w", err)
		}
	}
	if err := saveOpenAPIFolder(ctx, tx, "", root); err != nil {
		return out, err
	}
	if err := tx.Commit(); err != nil {
		return out, fmt.Errorf("commit: %w", err)
	}
	return out, nil
}

func saveOpenAPIFolder(
	ctx context.Context,
	tx store.TransactionalWriter,
	parentID string,
	folder *openapi.Collection,
) error {
	col := &domain.Collection{
		ID:          uuid.New().String(),
		ParentID:    parentID,
		Name:        folder.Name,
		Description: folder.Description,
	}
	if err := tx.SaveCollection(ctx, col); err != nil {
		return fmt.Errorf("save folder %q: %w", folder.Name, err)
	}
	for i, req := range folder.Requests {
		saved := *req
		saved.ID = uuid.New().String()
		saved.CollectionID = col.ID
		saved.SortOrder = i
		if err := tx.SaveRequest(ctx, &saved); err != nil {
			return fmt.Errorf("save request %q: %w", req.Name, err)
		}
	}
	if folder.Vars != nil {
		// SaveCollection creates the default environment; fill it in.
		env, err := tx.CreateDefaultEnvironment(ctx, col.ID)
		if err != nil {
			return fmt.Errorf("default environment of %q: %w", folder.Name, err)
		}
		env.SetVars(folder.Vars)
		if err := tx.SaveEnvironment(ctx, env); err != nil {
			return fmt.Errorf("save environment of %q: %w", folder.Name, err)
		}
	}
	for _, child := range folder.Children {
		if err := saveOpenAPIFolder(ctx, tx, col.ID, child); err != nil {
			return err
		}
	}
	return nil
}

func countFolders(c *openapi.Collection) int {
	n := len(c.Children)
	for _, child := range c.Children {
		n += countFolders(child)
	}
	return n
}

// uniqueOpenAPIName returns name, or "name N" with the smallest N not in used.
func uniqueOpenAPIName(name string, used map[string]bool) string {
	if !used[name] {
		return name
	}
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s %d", name, i)
		if !used[candidate] {
			return candidate
		}
	}
}

func printOpenAPIResult(cmd *cobra.Command, out openAPIOutcome, warnings []string) {
	w := cmd.OutOrStdout()
	if out.skipped {
		fmt.Fprintf(w, "Skipped %q (%d requests)\n", out.name, out.requests)
	} else {
		label := ""
		switch out.action {
		case openAPIDuplicateCopy:
			label = " (duplicated)"
		case openAPIDuplicateReplace:
			label = " (replaced)"
		}
		fmt.Fprintf(w, "Imported %d requests (%d folders) into %q%s\n",
			out.requests, out.folders, out.name, label)
	}
	for _, msg := range warnings {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s\n", msg)
	}
}
