package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/postman"
	"github.com/crazy-vedic/quark/internal/store"
)

type duplicateAction string

const (
	actionReplace   duplicateAction = "replace"
	actionDuplicate duplicateAction = "duplicate"
	actionMerge     duplicateAction = "merge"
	actionSkip      duplicateAction = "skip"

	globalEnvironmentDisplayName = "Global"
)

// ImportPostmanStore is the minimum interface NewImportPostmanCmd requires.
type ImportPostmanStore interface {
	store.CollectionLister
	store.RequestReader
	store.EnvironmentReader
	store.EnvironmentWriter
	BeginTransaction(ctx context.Context) (store.TransactionalWriter, error)
}

type importStats struct {
	filePath       string
	collectionName string
	imported       int
	total          int
	warnings       int
	warningMsgs    []string
	security       postman.SecurityLevel
	action         duplicateAction
	err            error
}

// NewImportPostmanCmd returns the 'quark import-postman' subcommand.
func NewImportPostmanCmd(st ImportPostmanStore, logger *DebugLogger) *cobra.Command {
	var collectionName, onDuplicate string

	cmd := &cobra.Command{
		Use:   "import-postman <collection.json|directory>",
		Short: "Import a Postman Collection v2.1 JSON file or a bulk export directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			ctx := cmd.Context()

			info, err := os.Stat(path)
			if err != nil {
				logger.Logf("stat path=%s err=%v", path, err)
				return fmt.Errorf("import-postman: %w", err)
			}

			validActions := map[string]bool{
				"replace":   true,
				"duplicate": true,
				"merge":     true,
				"skip":      true,
			}
			if !validActions[onDuplicate] {
				logger.Logf("invalid --on-duplicate=%s", onDuplicate)
				return fmt.Errorf(
					"invalid --on-duplicate %q: must be replace, duplicate, merge, or skip",
					onDuplicate,
				)
			}

			var allStats []importStats
			var globalAction duplicateAction

			var envResult envImportResult
			if info.IsDir() {
				logger.Logf("bulk import from directory=%s", path)
				allStats, envResult, err = importBulk(
					ctx,
					cmd,
					st,
					path,
					collectionName,
					onDuplicate,
					&globalAction,
					logger,
				)
				if err != nil {
					logger.Logf("bulk import failed: %v", err)
					return err
				}
			} else {
				logger.Logf("single file import path=%s", path)
				stat, err := importSingleFile(
					ctx,
					cmd,
					st,
					path,
					collectionName,
					onDuplicate,
					&globalAction,
					logger,
				)
				if err != nil {
					logger.Logf("single file import failed: %v", err)
					return err
				}
				allStats = []importStats{stat}
			}

			totalImported, totalSkipped, totalWarnings, totalErrors := 0, 0, 0, 0
			for _, s := range allStats {
				if s.err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "❌ %s: %v\n", s.filePath, s.err)
					totalErrors++
					continue
				}
				totalImported += s.imported
				totalSkipped += s.total - s.imported
				totalWarnings += s.warnings
				printImportResult(cmd, s)
			}
			totalWarnings += len(envResult.warnings)
			totalErrors += len(envResult.errors)

			// Print environment import results.
			if envResult.imported > 0 || len(envResult.errors) > 0 || len(envResult.warnings) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "\nEnvironments: %d imported", envResult.imported)
				for _, warning := range envResult.warnings {
					fmt.Fprintf(cmd.ErrOrStderr(), "\n  Warning: %s", warning)
				}
				if len(envResult.errors) > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), ", %d errors", len(envResult.errors))
					for _, errMsg := range envResult.errors {
						fmt.Fprintf(cmd.ErrOrStderr(), "\n  ⚠️ %s", errMsg)
					}
				}
				fmt.Fprintln(cmd.OutOrStdout())
			}

			if len(allStats) > 1 {
				fmt.Fprintf(
					cmd.OutOrStdout(),
					"\nSummary: %d imported, %d skipped, %d warnings, %d errors, %d envs across %d collections\n",
					totalImported,
					totalSkipped,
					totalWarnings,
					totalErrors,
					envResult.imported,
					len(allStats),
				)
			} else {
				logger.Logf("import complete imported=%d skipped=%d warnings=%d errors=%d envs=%d",
					totalImported, totalSkipped, totalWarnings, totalErrors, envResult.imported)
			}
			if len(envResult.errors) > 0 {
				return fmt.Errorf("import-postman: %d environment error(s)", len(envResult.errors))
			}
			return nil
		},
	}

	cmd.Flags().
		StringVarP(&collectionName, "collection-name", "n", "", "Override the imported collection name")
	cmd.Flags().
		StringVar(&onDuplicate, "on-duplicate", "duplicate", "Action when collection name already exists: replace, duplicate, merge, skip")

	return cmd
}

//nolint:gocyclo // Coordinates one atomic import across hierarchy, variables, and requests.
func importSingleFile(
	ctx context.Context,
	cmd *cobra.Command,
	st ImportPostmanStore,
	path, overrideName, onDuplicate string,
	globalAction *duplicateAction,
	logger *DebugLogger,
) (importStats, error) {
	logger.Logf("parsing file=%s", path)
	f, err := os.Open(path)
	if err != nil {
		logger.Logf("open failed file=%s err=%v", path, err)
		return importStats{filePath: path, err: err}, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	result, err := postman.NewImporter().Parse(f)
	if err != nil {
		logger.Logf("parse failed file=%s err=%v", path, err)
		return importStats{filePath: path, err: err}, fmt.Errorf("parse: %w", err)
	}
	logger.Logf("parsed file=%s name=%s requests=%d warnings=%d security=%s",
		path, result.CollectionName, len(result.Requests), len(result.Warnings), result.Security)

	name := overrideName
	if name == "" {
		name = result.CollectionName
	}
	if name == "" {
		name = "Imported"
	}

	action, existingID := resolveDuplicate(
		ctx,
		cmd,
		st,
		name,
		result.Requests,
		onDuplicate,
		globalAction,
		logger,
	)
	logger.Logf("resolved action=%s existingID=%s name=%s", action, existingID, name)

	// No actual conflict occurred — suppress the "duplicated" label in output.
	if action == actionDuplicate && existingID == "" {
		action = ""
	}

	if action == actionSkip {
		logger.Logf("skipped name=%s requests=%d", name, len(result.Requests))
		return importStats{
			filePath:       path,
			collectionName: name,
			action:         actionSkip,
			total:          len(result.Requests),
			warnings:       len(result.Warnings),
			warningMsgs:    result.Warnings,
			security:       result.Security,
		}, nil
	}

	if action == actionDuplicate {
		name = uniqueCollectionName(ctx, st, name)
		logger.Logf("deduplicated name=%s", name)
	}
	// Read existing collections before opening the transaction. Store uses a
	// single SQLite connection, so querying the store while tx is open would
	// block waiting for the connection held by that transaction.
	allCollections, _ := st.ListCollections(ctx)
	existingRequestNamesByCollection := make(map[string]map[string]bool)
	if action == actionMerge {
		for _, existingCollection := range allCollections {
			existingRequests, listErr := st.ListRequests(ctx, existingCollection.ID)
			if listErr != nil {
				continue
			}
			names := make(map[string]bool, len(existingRequests))
			for _, existingRequest := range existingRequests {
				names[existingRequest.Name] = true
			}
			existingRequestNamesByCollection[existingCollection.ID] = names
		}
	}

	logger.Logf("tx begin name=%s", name)
	tx, err := st.BeginTransaction(ctx)
	if err != nil {
		logger.Logf("tx begin failed name=%s err=%v", name, err)
		return importStats{
				filePath:       path,
				collectionName: name,
				err:            err,
			}, fmt.Errorf(
				"begin transaction: %w",
				err,
			)
	}
	defer func() { _ = tx.Rollback() }()

	var col *domain.Collection

	if action == actionReplace && existingID != "" {
		logger.Logf("deleting existing collection id=%s", existingID)
		if err := tx.DeleteCollection(ctx, existingID); err != nil {
			logger.Logf("delete existing failed id=%s err=%v", existingID, err)
			return importStats{
					filePath:       path,
					collectionName: name,
					err:            err,
				}, fmt.Errorf(
					"delete existing collection: %w",
					err,
				)
		}
	}

	if action == actionMerge {
		// Find existing collection by name or ID.
		for _, c := range allCollections {
			if c.ID == existingID || c.Name == name {
				col = c
				break
			}
		}
		if col == nil {
			logger.Logf(
				"merge failed: existing collection not found name=%s id=%s",
				name,
				existingID,
			)
			return importStats{
					filePath:       path,
					collectionName: name,
					err:            err,
				}, fmt.Errorf(
					"merge: existing collection not found",
				)
		}
		logger.Logf("merge target found id=%s name=%s", col.ID, col.Name)
	} else {
		col = &domain.Collection{Name: name}
		if err := tx.SaveCollection(ctx, col); err != nil {
			logger.Logf("save collection failed name=%s err=%v", name, err)
			return importStats{
					filePath:       path,
					collectionName: name,
					err:            err,
				}, fmt.Errorf(
					"save collection: %w",
					err,
				)
		}
		logger.Logf("collection created id=%s name=%s", col.ID, col.Name)
	}
	groups := result.Groups
	if len(groups) == 0 && len(result.Requests) > 0 {
		groups = []postman.RequestGroup{{Requests: result.Requests}}
	}
	collectionsByPath := map[string]*domain.Collection{"": col}
	for _, group := range groups {
		if group.Path == "" {
			continue
		}
		parent := col
		pathParts := strings.Split(group.Path, "/")
		pathSoFar := ""
		for _, part := range pathParts {
			if pathSoFar == "" {
				pathSoFar = part
			} else {
				pathSoFar += "/" + part
			}
			if existing, ok := collectionsByPath[pathSoFar]; ok {
				parent = existing
				continue
			}
			var child *domain.Collection
			if action == actionMerge {
				for _, candidate := range allCollections {
					if candidate.ParentID == parent.ID && candidate.Name == part {
						child = candidate
						break
					}
				}
			}
			if child == nil {
				child = &domain.Collection{Name: part, ParentID: parent.ID}
				if err := tx.SaveCollection(ctx, child); err != nil {
					return importStats{filePath: path, collectionName: name, err: err}, fmt.Errorf("save nested collection %q: %w", pathSoFar, err)
				}
			}
			collectionsByPath[pathSoFar] = child
			parent = child
		}
	}

	variableWarnings, err := mergeCollectionVariablesIntoRootDefault(
		ctx, tx, col.ID, result.CollectionVariables,
	)
	if err != nil {
		return importStats{filePath: path, collectionName: name, err: err}, fmt.Errorf("save collection variables: %w", err)
	}
	result.Warnings = append(result.Warnings, variableWarnings...)
	sort.Strings(result.Warnings)

	imported := 0
	for _, group := range groups {
		target := collectionsByPath[group.Path]
		if target == nil {
			continue
		}
		existingNames := make(map[string]bool)
		if action == actionMerge {
			for existingName := range existingRequestNamesByCollection[target.ID] {
				existingNames[existingName] = true
			}
		}
		requestsToSave := append([]*domain.Request(nil), group.Requests...)
		if action == actionMerge {
			filtered := make([]*domain.Request, 0, len(requestsToSave))
			for _, req := range requestsToSave {
				if !existingNames[req.Name] {
					filtered = append(filtered, req)
				}
			}
			requestsToSave = filtered
		}
		deduplicateImportedRequestNames(requestsToSave, existingNames)
		for _, req := range requestsToSave {
			req.CollectionID = target.ID
			if err := tx.SaveRequest(ctx, req); err != nil {
				logger.Logf("save request failed name=%s err=%v", req.Name, err)
				return importStats{filePath: path, collectionName: name, err: err}, fmt.Errorf("save request %q: %w", req.Name, err)
			}
			imported++
		}
	}
	logger.Logf("saved requests count=%d", imported)

	if err := tx.Commit(); err != nil {
		logger.Logf("tx commit failed name=%s err=%v", name, err)
		return importStats{
				filePath:       path,
				collectionName: name,
				err:            err,
			}, fmt.Errorf(
				"commit: %w",
				err,
			)
	}
	logger.Logf("tx committed name=%s", name)

	return importStats{
		filePath:       path,
		collectionName: name,
		imported:       imported,
		total:          len(result.Requests),
		warnings:       len(result.Warnings),
		warningMsgs:    result.Warnings,
		security:       result.Security,
		action:         action,
	}, nil
}

func mergeCollectionVariablesIntoRootDefault(
	ctx context.Context,
	tx store.TransactionalWriter,
	collectionID string,
	incoming map[string]string,
) ([]string, error) {
	if len(incoming) == 0 {
		return nil, nil
	}
	defaultEnvironment, err := tx.CreateDefaultEnvironment(ctx, collectionID)
	if err != nil {
		return nil, err
	}
	vars, err := defaultEnvironment.DecodeVars()
	if err != nil {
		return nil, fmt.Errorf("decode root default: %w", err)
	}
	keys := make([]string, 0, len(incoming))
	for key := range incoming {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var warnings []string
	changed := false
	for _, key := range keys {
		value := incoming[key]
		if existing, present := vars[key]; present {
			if existing != value {
				warnings = append(warnings, fmt.Sprintf("collection variable %q conflicts with root default; existing value kept", key))
			}
			continue
		}
		vars[key] = value
		changed = true
	}
	if changed {
		defaultEnvironment.SetVars(vars)
		if err := tx.SaveEnvironment(ctx, defaultEnvironment); err != nil {
			return nil, err
		}
	}
	return warnings, nil
}

// deduplicateImportedRequestNames makes request names unique for one import
// operation. The database constraint remains the final guard for all other
// request writes; this only avoids rejecting valid Postman collections that
// contain multiple items with the same name.
func deduplicateImportedRequestNames(requests []*domain.Request, existingNames map[string]bool) {
	usedNames := make(map[string]bool, len(existingNames)+len(requests))
	for name := range existingNames {
		usedNames[name] = true
	}

	for _, req := range requests {
		baseName := req.Name
		if !usedNames[baseName] {
			usedNames[baseName] = true
			continue
		}

		for suffix := 1; ; suffix++ {
			candidate := fmt.Sprintf("%s (%d)", baseName, suffix)
			if !usedNames[candidate] {
				req.Name = candidate
				usedNames[candidate] = true
				break
			}
		}
	}
}

type envImportResult struct {
	imported int
	errors   []string
	warnings []string
}

type parsedEnvironmentFile struct {
	filename string
	vars     map[string]string
}

type parsedEnvironmentResult struct {
	files    []parsedEnvironmentFile
	errors   []string
	warnings []string
}

func importBulk(
	ctx context.Context,
	cmd *cobra.Command,
	st ImportPostmanStore,
	dir, overrideName, onDuplicate string,
	globalAction *duplicateAction,
	logger *DebugLogger,
) ([]importStats, envImportResult, error) {
	var files []string

	// Try archive.json manifest first.
	archivePath := filepath.Join(dir, "archive.json")
	if data, err := os.ReadFile(archivePath); err == nil {
		var manifest struct {
			Collection map[string]bool `json:"collection"`
		}
		if err := json.Unmarshal(data, &manifest); err == nil {
			logger.Logf("archive.json manifest: %d collections", len(manifest.Collection))
			for id := range manifest.Collection {
				if !safeManifestID(id) {
					logger.Logf("manifest rejected unsafe id=%q", id)
					continue
				}
				p := filepath.Join(dir, "collection", id+".json")
				if info, err := os.Stat(p); err == nil && !info.IsDir() {
					files = append(files, p)
				} else {
					logger.Logf("manifest file missing id=%s path=%s", id, p)
				}
			}
		} else {
			logger.Logf("archive.json parse failed: %v", err)
		}
	} else {
		logger.Logf("archive.json not found, scanning collection/")
	}

	// Fallback: scan collection/ directory.
	if len(files) == 0 {
		collDir := filepath.Join(dir, "collection")
		entries, err := os.ReadDir(collDir)
		if err != nil {
			logger.Logf("scan collection/ failed: %v", err)
			return nil, envImportResult{}, fmt.Errorf(
				"not a valid Postman bulk export: missing collection/ directory: %w",
				err,
			)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			files = append(files, filepath.Join(collDir, entry.Name()))
		}
	}

	if len(files) == 0 {
		logger.Logf("no collection files found in %s", dir)
		return nil, envImportResult{}, fmt.Errorf("no collection files found in %q", dir)
	}

	// Parse standalone environments independently. They are merged only after
	// every collection import has had a chance to complete.
	parsedEnvironments := parseEnvironmentsInDir(dir, logger)

	sort.Strings(files)
	logger.Logf("found %d collection files", len(files))

	var allStats []importStats
	for _, f := range files {
		stat, err := importSingleFile(
			ctx,
			cmd,
			st,
			f,
			overrideName,
			onDuplicate,
			globalAction,
			logger,
		)
		if err != nil {
			stat.err = err
		}
		allStats = append(allStats, stat)
	}

	envResult := envImportResult{
		imported: len(parsedEnvironments.files),
		errors:   append([]string(nil), parsedEnvironments.errors...),
		warnings: append([]string(nil), parsedEnvironments.warnings...),
	}
	if len(parsedEnvironments.files) > 0 {
		mergeWarnings, err := mergeParsedEnvironmentsIntoGlobal(ctx, st, parsedEnvironments.files, logger)
		envResult.warnings = append(envResult.warnings, mergeWarnings...)
		if err != nil {
			envResult.imported = 0
			errMsg := fmt.Sprintf("save standalone environments to Global failed: %v", err)
			logger.Logf("%s", errMsg)
			envResult.errors = append(envResult.errors, errMsg)
		}
	}
	sort.Strings(envResult.errors)
	sort.Strings(envResult.warnings)

	return allStats, envResult, nil
}

// mergeEnvironmentsIntoGlobal is retained for focused callers and tests. Input
// order is authoritative after existing Global values.
func mergeEnvironmentsIntoGlobal(
	ctx context.Context,
	st ImportPostmanStore,
	envs []*domain.Environment,
	logger *DebugLogger,
) error {
	parsed := make([]parsedEnvironmentFile, 0, len(envs))
	for _, environment := range envs {
		if environment == nil {
			continue
		}
		vars, err := environment.DecodeVars()
		if err != nil {
			return err
		}
		parsed = append(parsed, parsedEnvironmentFile{filename: environment.Name, vars: vars})
	}
	_, err := mergeParsedEnvironmentsIntoGlobal(ctx, st, parsed, logger)
	return err
}

func mergeParsedEnvironmentsIntoGlobal(
	ctx context.Context,
	st ImportPostmanStore,
	files []parsedEnvironmentFile,
	logger *DebugLogger,
) ([]string, error) {
	global, err := st.GetGlobalEnvironment(ctx)
	if err != nil {
		logger.Logf("get global env failed: %v", err)
		return nil, err
	}

	vars, err := global.DecodeVars()
	if err != nil {
		return nil, fmt.Errorf("decode Global: %w", err)
	}
	clone := *global
	sources := make(map[string]string, len(vars))
	for key := range vars {
		sources[key] = globalEnvironmentDisplayName
	}
	changed := false
	var warnings []string
	for _, file := range files {
		keys := make([]string, 0, len(file.vars))
		for key := range file.vars {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value := file.vars[key]
			if existing, present := vars[key]; present {
				if existing != value {
					warnings = append(warnings, fmt.Sprintf("%s: variable %q conflicts with %s; existing value kept", file.filename, key, sources[key]))
				}
				continue
			}
			vars[key] = value
			sources[key] = file.filename
			changed = true
		}
	}
	if !changed {
		return warnings, nil
	}
	clone.SetVars(vars)
	if err := st.SaveEnvironment(ctx, &clone); err != nil {
		logger.Logf("save merged global env failed: %v", err)
		return warnings, err
	}
	logger.Logf("merged %d environment file(s) into Global, %d variables total", len(files), len(vars))
	return warnings, nil
}

// parseEnvironmentsInDir parses all .json files in the environment/ subdirectory.
func parseEnvironmentsInDir(
	dir string,
	logger *DebugLogger,
) parsedEnvironmentResult {
	envDir := filepath.Join(dir, "environment")
	entries, err := os.ReadDir(envDir)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Logf("no environment/ directory")
			return parsedEnvironmentResult{}
		}
		logger.Logf("no environment/ directory: %v", err)
		return parsedEnvironmentResult{errors: []string{fmt.Sprintf("read environment directory: %v", err)}}
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var result parsedEnvironmentResult
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(envDir, entry.Name())
		f, err := os.Open(path)
		if err != nil {
			errMsg := fmt.Sprintf("%s: open failed: %v", entry.Name(), err)
			logger.Logf("%s", errMsg)
			result.errors = append(result.errors, errMsg)
			continue
		}
		pmEnv, err := postman.ParseEnvironment(f)
		f.Close()
		if err != nil {
			errMsg := fmt.Sprintf("%s: %v", entry.Name(), err)
			logger.Logf("parse environment file failed: %s", entry.Name())
			result.errors = append(result.errors, errMsg)
			continue
		}
		vars, warnings := pmEnv.ToMapWithWarnings()
		for _, warning := range warnings {
			result.warnings = append(result.warnings, fmt.Sprintf("%s: %s", entry.Name(), warning))
		}
		result.files = append(result.files, parsedEnvironmentFile{filename: entry.Name(), vars: vars})
	}
	return result
}

func resolveDuplicate(
	ctx context.Context,
	cmd *cobra.Command,
	st ImportPostmanStore,
	name string,
	newRequests []*domain.Request,
	onDuplicate string,
	globalAction *duplicateAction,
	logger *DebugLogger,
) (duplicateAction, string) {
	cols, err := st.ListCollections(ctx)
	if err != nil {
		logger.Logf("list collections failed: %v", err)
		return actionDuplicate, ""
	}

	var existing *domain.Collection
	for _, c := range cols {
		if c.Name == name {
			existing = c
			break
		}
	}
	if existing == nil {
		logger.Logf("no conflict name=%s", name)
		return actionDuplicate, ""
	}
	logger.Logf("conflict detected name=%s existingID=%s", name, existing.ID)

	// Global action takes precedence.
	if globalAction != nil && *globalAction != "" {
		logger.Logf("using global action=%s", *globalAction)
		return *globalAction, existing.ID
	}

	// Explicit --on-duplicate flag.
	if cmd.Flags().Changed("on-duplicate") {
		action := duplicateAction(onDuplicate)
		logger.Logf("flag action=%s", action)
		switch action {
		case actionReplace, actionDuplicate, actionMerge, actionSkip:
			return action, existing.ID
		}
		return actionDuplicate, existing.ID
	}

	// Non-TTY: default to duplicate.
	if !isTerminal(cmd.InOrStdin()) {
		logger.Logf("non-TTY default=duplicate")
		return actionDuplicate, existing.ID
	}

	// Interactive prompt.
	existingReqs, _ := st.ListRequests(ctx, existing.ID)

	fmt.Fprintf(cmd.OutOrStdout(),
		"\nCollection %q already exists (%d existing, %d incoming).\n",
		name, len(existingReqs), len(newRequests))

	if len(existingReqs) > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "Existing requests:")
		for _, r := range existingReqs {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s %s\n", r.Method, r.Name)
		}
	}
	if len(newRequests) > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "Incoming requests:")
		for _, r := range newRequests {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s %s\n", r.Method, r.Name)
		}
	}

	reader := bufio.NewReader(cmd.InOrStdin())
	for {
		fmt.Fprint(cmd.OutOrStdout(), "[r]eplace, [d]uplicate, [m]erge, [s]kip, [a]pply to all? ")
		line, err := reader.ReadString('\n')
		if err != nil {
			logger.Logf("interactive read error, defaulting to duplicate")
			return actionDuplicate, existing.ID
		}
		line = strings.TrimSpace(strings.ToLower(line))
		if line == "" {
			continue
		}

		switch line {
		case "r", "replace":
			logger.Logf("interactive choice=replace")
			return actionReplace, existing.ID
		case "d", "duplicate":
			logger.Logf("interactive choice=duplicate")
			return actionDuplicate, existing.ID
		case "m", "merge":
			logger.Logf("interactive choice=merge")
			return actionMerge, existing.ID
		case "s", "skip":
			logger.Logf("interactive choice=skip")
			return actionSkip, existing.ID
		case "a", "apply":
			fmt.Fprint(
				cmd.OutOrStdout(),
				"Apply which action to all? [r]eplace, [d]uplicate, [m]erge, [s]kip? ",
			)
			line2, err := reader.ReadString('\n')
			if err != nil {
				logger.Logf("interactive apply read error, defaulting to duplicate")
				return actionDuplicate, existing.ID
			}
			line2 = strings.TrimSpace(strings.ToLower(line2))
			switch line2 {
			case "r", "replace":
				*globalAction = actionReplace
				logger.Logf("apply-to-all=replace")
			case "d", "duplicate":
				*globalAction = actionDuplicate
				logger.Logf("apply-to-all=duplicate")
			case "m", "merge":
				*globalAction = actionMerge
				logger.Logf("apply-to-all=merge")
			case "s", "skip":
				*globalAction = actionSkip
				logger.Logf("apply-to-all=skip")
			default:
				fmt.Fprintln(cmd.OutOrStdout(), "Invalid choice.")
				continue
			}
			return *globalAction, existing.ID
		default:
			fmt.Fprintln(cmd.OutOrStdout(), "Invalid choice. Please enter r, d, m, s, or a.")
		}
	}
}

func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	return isatty.IsTerminal(f.Fd())
}

func printImportResult(cmd *cobra.Command, s importStats) {
	if s.err != nil {
		return
	}

	actionLabel := ""
	switch s.action {
	case actionReplace:
		actionLabel = " (replaced)"
	case actionDuplicate:
		actionLabel = " (duplicated)"
	case actionMerge:
		actionLabel = " (merged)"
	case actionSkip:
		fmt.Fprintf(cmd.OutOrStdout(), "Skipped %q (%d requests)\n", s.collectionName, s.total)
		return
	}

	fmt.Fprintf(cmd.OutOrStdout(),
		"Imported %d/%d requests into collection %q%s",
		s.imported, s.total, s.collectionName, actionLabel)
	if s.warnings > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), " (%d warnings)", s.warnings)
	}
	fmt.Fprintln(cmd.OutOrStdout())

	if s.security > postman.Safe {
		secLabel := "Review"
		if s.security == postman.Dangerous {
			secLabel = "Dangerous"
		}
		fmt.Fprintf(cmd.ErrOrStderr(),
			"Security: %s — collection contains potential credentials. Verify before sharing.\n",
			secLabel)
	}

	for _, w := range s.warningMsgs {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s\n", w)
	}
}

// safeManifestID rejects manifest IDs that contain path traversal characters.
func safeManifestID(id string) bool {
	if id == "" {
		return false
	}
	if strings.ContainsAny(id, `/\`) {
		return false
	}
	return !strings.Contains(id, "..")
}

func uniqueCollectionName(ctx context.Context, st interface {
	ListCollections(ctx context.Context) ([]*domain.Collection, error)
}, name string) string {
	cols, err := st.ListCollections(ctx)
	if err != nil {
		return name
	}

	existing := make(map[string]bool)
	for _, c := range cols {
		existing[c.Name] = true
	}

	if !existing[name] {
		return name
	}

	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s %d", name, i)
		if !existing[candidate] {
			return candidate
		}
	}
}
