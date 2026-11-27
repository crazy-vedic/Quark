package cli

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/store"
)

// NewRequestCmd returns the 'quark request' subcommand tree.
func NewRequestCmd(r SearchStore) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "request",
		Short: "Manage requests",
	}
	cmd.AddCommand(newRequestListCmd(r))
	cmd.AddCommand(newRequestExportCurlCmd(r))
	cmd.AddCommand(newRequestCreateCmd(r))
	cmd.AddCommand(newRequestShowCmd(r))
	cmd.AddCommand(newRequestRenameCmd(r))
	cmd.AddCommand(newRequestDeleteCmd(r))
	return cmd
}

// requestMethods mirrors the CHECK constraint on requests.method in the schema.
var requestMethods = []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}

// splitRequestPath splits "Collection/Request" at the last slash, so a nested
// collection path such as API/Users/Get user keeps its parent collections.
func splitRequestPath(ref string) (collectionRef, name string, err error) {
	i := strings.LastIndex(ref, "/")
	if i <= 0 {
		return "", "", fmt.Errorf("request path must be 'Collection/Request', got %q", ref)
	}
	collectionRef = strings.TrimSpace(ref[:i])
	name = strings.TrimSpace(ref[i+1:])
	if collectionRef == "" || name == "" {
		return "", "", fmt.Errorf("request path must be 'Collection/Request', got %q", ref)
	}
	return collectionRef, name, nil
}

// lookupRequestPath returns the request at a Collection/Request path. The
// collection part accepts the same forms as --collection elsewhere.
func lookupRequestPath(ctx context.Context, r SearchStore, ref string) (*domain.Request, error) {
	collectionRef, name, err := splitRequestPath(ref)
	if err != nil {
		return nil, err
	}
	col, err := resolveCollectionReference(ctx, r, collectionRef)
	if err != nil {
		return nil, fmt.Errorf("resolve collection: %w", err)
	}
	reqs, err := r.ListRequests(ctx, col.ID)
	if err != nil {
		return nil, fmt.Errorf("list requests: %w", err)
	}
	for _, req := range reqs {
		if req.Name == name {
			return req, nil
		}
	}
	return nil, fmt.Errorf("request %q not found in collection %q", name, col.Name)
}

// requestWriter returns the store's request writer, or an error when the
// store cannot write requests.
func requestWriter(r SearchStore, command string) (store.RequestWriter, error) {
	w, ok := r.(store.RequestWriter)
	if !ok {
		return nil, fmt.Errorf("%s: store cannot save requests", command)
	}
	return w, nil
}

func validateRequestName(name string) error {
	if strings.Contains(name, "/") {
		return fmt.Errorf("request name must not contain '/', got %q", name)
	}
	return nil
}

func newRequestCreateCmd(r SearchStore) *cobra.Command {
	var method, url string
	cmd := &cobra.Command{
		Use:   "create <Collection/Request>",
		Short: "Create a request in a collection",
		Long: "Create a request in a collection. Use --method and --url to set its " +
			"method (default GET) and URL; headers, body and auth are set in the TUI.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w, err := requestWriter(r, "request create")
			if err != nil {
				return err
			}
			collectionRef, name, err := splitRequestPath(args[0])
			if err != nil {
				return fmt.Errorf("request create: %w", err)
			}
			if err := validateRequestName(name); err != nil {
				return fmt.Errorf("request create: %w", err)
			}
			verb := strings.ToUpper(strings.TrimSpace(method))
			if !slices.Contains(requestMethods, verb) {
				return fmt.Errorf("request create: --method must be one of %s, got %q",
					strings.Join(requestMethods, ", "), method)
			}
			col, err := resolveCollectionReference(cmd.Context(), r, collectionRef)
			if err != nil {
				return fmt.Errorf("request create: resolve collection: %w", err)
			}
			existing, err := r.ListRequests(cmd.Context(), col.ID)
			if err != nil {
				return fmt.Errorf("request create: %w", err)
			}
			req := &domain.Request{
				ID:           uuid.New().String(),
				CollectionID: col.ID,
				Name:         name,
				Method:       verb,
				URL:          strings.TrimSpace(url),
				Headers:      "{}",
				SortOrder:    len(existing),
				Enabled:      true,
			}
			if err := w.SaveRequest(cmd.Context(), req); err != nil {
				return fmt.Errorf("request create: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created request %q (%s)\n", args[0], shortID(req.ID))
			return nil
		},
	}
	cmd.Flags().StringVar(&method, "method", "GET",
		"HTTP method: GET, POST, PUT, DELETE, PATCH, HEAD or OPTIONS")
	cmd.Flags().StringVar(&url, "url", "", "Request URL")
	return cmd
}

func newRequestShowCmd(r SearchStore) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <Collection/Request>",
		Short: "Show a saved request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := lookupRequestPath(cmd.Context(), r, args[0])
			if err != nil {
				return fmt.Errorf("request show: %w", err)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, args[0])
			fmt.Fprintf(out, "  %-8s %s\n", "Method", req.Method)
			fmt.Fprintf(out, "  %-8s %s\n", "URL", req.URL)
			fmt.Fprintf(out, "  %-8s %s\n", "Enabled", yesNo(req.Enabled))
			if req.AuthType != "" && req.AuthType != "none" {
				fmt.Fprintf(out, "  %-8s %s\n", "Auth", req.AuthType)
			}
			if req.Headers != "" && req.Headers != "{}" {
				fmt.Fprintf(out, "  %-8s %s\n", "Headers", req.Headers)
			}
			if req.Body != "" {
				fmt.Fprintf(out, "  %s\n%s\n", "Body", req.Body)
			}
			return nil
		},
	}
	cmd.ValidArgsFunction = CompleteRequestPaths(r.ListCollections, r.ListRequests)
	return cmd
}

func newRequestRenameCmd(r SearchStore) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rename <Collection/Request> <new-name>",
		Short: "Rename a saved request",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			w, err := requestWriter(r, "request rename")
			if err != nil {
				return err
			}
			newName := strings.TrimSpace(args[1])
			if newName == "" {
				return fmt.Errorf("request rename: new name must not be empty")
			}
			if err := validateRequestName(newName); err != nil {
				return fmt.Errorf("request rename: %w", err)
			}
			req, err := lookupRequestPath(cmd.Context(), r, args[0])
			if err != nil {
				return fmt.Errorf("request rename: %w", err)
			}
			req.Name = newName
			if err := w.SaveRequest(cmd.Context(), req); err != nil {
				return fmt.Errorf("request rename: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Renamed request %q to %q\n", args[0], newName)
			return nil
		},
	}
	cmd.ValidArgsFunction = CompleteRequestPaths(r.ListCollections, r.ListRequests)
	return cmd
}

func newRequestDeleteCmd(r SearchStore) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <Collection/Request> --yes",
		Short: "Delete a saved request",
		Long:  "Delete a saved request. Requires --yes.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return fmt.Errorf("request delete: refusing to delete %q without --yes", args[0])
			}
			w, err := requestWriter(r, "request delete")
			if err != nil {
				return err
			}
			req, err := lookupRequestPath(cmd.Context(), r, args[0])
			if err != nil {
				return fmt.Errorf("request delete: %w", err)
			}
			if err := w.DeleteRequest(cmd.Context(), req.ID); err != nil {
				return fmt.Errorf("request delete: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Deleted request %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm the deletion")
	cmd.ValidArgsFunction = CompleteRequestPaths(r.ListCollections, r.ListRequests)
	return cmd
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
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
