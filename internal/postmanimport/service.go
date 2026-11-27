// Package postmanimport imports Postman Collection v2.1 files and bulk export
// directories into the Quark store. It owns the orchestration (duplicate
// handling, folder hierarchy, collection variables, environments) and returns
// a structured Result; the CLI and TUI format that result themselves.
package postmanimport

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/postman"
	"github.com/crazy-vedic/quark/internal/store"
)

// Store is the minimum store interface the import service requires.
type Store interface {
	store.CollectionLister
	store.RequestReader
	store.EnvironmentReader
	store.EnvironmentWriter
	BeginTransaction(ctx context.Context) (store.TransactionalWriter, error)
}

// Logger receives debug lines. A nil Logger discards them.
type Logger interface {
	Logf(format string, args ...any)
}

type nopLogger struct{}

func (nopLogger) Logf(string, ...any) {}

// Action is how an import treats a root collection whose name already exists.
type Action string

const (
	// ActionReplace deletes the existing root collection and imports a new one.
	ActionReplace Action = "replace"
	// ActionDuplicate imports into a new root collection with a suffixed name.
	ActionDuplicate Action = "duplicate"
	// ActionMerge adds requests that are not already present to the existing collection.
	ActionMerge Action = "merge"
	// ActionSkip leaves the existing collection alone and imports nothing.
	ActionSkip Action = "skip"
)

// Valid reports whether a is one of the four known actions.
func (a Action) Valid() bool {
	switch a {
	case ActionReplace, ActionDuplicate, ActionMerge, ActionSkip:
		return true
	}
	return false
}

// Conflict describes an incoming collection whose name matches an existing
// root collection.
type Conflict struct {
	Name             string
	ExistingID       string
	ExistingRequests []*domain.Request
	IncomingRequests []*domain.Request
}

// Resolver decides how to handle a Conflict. When applyToAll is true, the
// returned action is reused for every later conflict in the same Import call
// without calling the Resolver again.
type Resolver func(ctx context.Context, conflict Conflict) (action Action, applyToAll bool, err error)

// Options configures one Import call.
type Options struct {
	// CollectionName overrides the root collection name from the Postman file.
	CollectionName string
	// OnDuplicate is used for name conflicts when Resolve is nil. Empty means
	// ActionDuplicate.
	OnDuplicate Action
	// Resolve, when set, is asked about every name conflict.
	Resolve Resolver
	// IncludeDisabled imports headers that are switched off in Postman.
	IncludeDisabled bool
}

// Service imports Postman collections into a Store.
type Service struct {
	st     Store
	logger Logger
}

// New returns a Service writing to st. logger may be nil.
func New(st Store, logger Logger) *Service {
	if logger == nil {
		logger = nopLogger{}
	}
	return &Service{st: st, logger: logger}
}

// run holds state shared by every collection file of one Import call.
type run struct {
	*Service
	opts      Options
	importer  *postman.Importer
	allAction Action
}

// Import reads a Postman collection file, or a Postman bulk export directory,
// and writes it to the store.
//
// For a single file, a failure is returned as the error (and also recorded on
// the one CollectionResult). For a directory, per-collection failures are
// recorded on each CollectionResult and environment failures in
// Result.Environments.Errors; the error is only set when the directory is not
// a usable export.
func (s *Service) Import(ctx context.Context, path string, opts Options) (Result, error) {
	if opts.OnDuplicate == "" {
		opts.OnDuplicate = ActionDuplicate
	}
	if !opts.OnDuplicate.Valid() {
		return Result{}, fmt.Errorf(
			"invalid duplicate action %q: must be replace, duplicate, merge, or skip",
			opts.OnDuplicate,
		)
	}

	info, err := os.Stat(path)
	if err != nil {
		s.logger.Logf("stat path=%s err=%v", path, err)
		return Result{}, err
	}

	var importerOpts []postman.Option
	if opts.IncludeDisabled {
		importerOpts = append(importerOpts, postman.WithDisabledHeaders())
	}
	r := &run{Service: s, opts: opts, importer: postman.NewImporter(importerOpts...)}

	if info.IsDir() {
		s.logger.Logf("bulk import from directory=%s", path)
		result, err := r.importBulk(ctx, path)
		if err != nil {
			s.logger.Logf("bulk import failed: %v", err)
			return Result{Bulk: true}, err
		}
		return result, nil
	}

	s.logger.Logf("single file import path=%s", path)
	collection := r.importFile(ctx, path)
	if collection.Err != nil {
		s.logger.Logf("single file import failed: %v", collection.Err)
		return Result{Collections: []CollectionResult{collection}}, collection.Err
	}
	return Result{Collections: []CollectionResult{collection}}, nil
}

// importFile imports one Postman collection file. A failure is recorded on the
// returned CollectionResult.Err, so callers read one value.
func (r *run) importFile(ctx context.Context, path string) CollectionResult {
	result, err := r.writeCollectionFile(ctx, path)
	if err != nil {
		result.Err = err
	}
	return result
}

// writeCollectionFile imports one Postman collection file in one transaction.
//
//nolint:gocyclo // Coordinates one atomic import across hierarchy, variables, and requests.
func (r *run) writeCollectionFile(ctx context.Context, path string) (CollectionResult, error) {
	logger := r.logger
	logger.Logf("parsing file=%s", path)
	failed := CollectionResult{FilePath: path}
	f, err := os.Open(path)
	if err != nil {
		logger.Logf("open failed file=%s err=%v", path, err)
		return failed, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	parsed, err := r.importer.Parse(f)
	if err != nil {
		logger.Logf("parse failed file=%s err=%v", path, err)
		return failed, fmt.Errorf("parse: %w", err)
	}
	logger.Logf("parsed file=%s name=%s requests=%d warnings=%d security=%s",
		path, parsed.CollectionName, len(parsed.Requests), len(parsed.Warnings), parsed.Security)

	name := r.opts.CollectionName
	if name == "" {
		name = parsed.CollectionName
	}
	if name == "" {
		name = "Imported"
	}
	failed.Name = name

	action, existingID, err := r.resolveConflict(ctx, name, parsed.Requests)
	if err != nil {
		return failed, err
	}
	logger.Logf("resolved action=%s existingID=%s name=%s", action, existingID, name)

	if action == ActionSkip {
		logger.Logf("skipped name=%s requests=%d", name, len(parsed.Requests))
		return CollectionResult{
			FilePath: path,
			Name:     name,
			Outcome:  OutcomeSkipped,
			Total:    len(parsed.Requests),
			Warnings: parsed.Warnings,
			Security: parsed.Security,
		}, nil
	}

	outcome := OutcomeCreated
	switch {
	case existingID == "":
		// No conflict: a plain create.
	case action == ActionDuplicate:
		outcome = OutcomeDuplicated
		name, err = uniqueCollectionName(ctx, r.st, name)
		if err != nil {
			return failed, err
		}
		failed.Name = name
		logger.Logf("deduplicated name=%s", name)
	case action == ActionReplace:
		outcome = OutcomeReplaced
	case action == ActionMerge:
		outcome = OutcomeMerged
	}

	// Read existing collections before opening the transaction. Store uses a
	// single SQLite connection, so querying the store while tx is open would
	// block waiting for the connection held by that transaction.
	allCollections, err := r.st.ListCollections(ctx)
	if err != nil {
		return failed, fmt.Errorf("list collections: %w", err)
	}
	existingRequestNamesByCollection := make(map[string]map[string]bool)
	if outcome == OutcomeMerged {
		for _, existingCollection := range allCollections {
			existingRequests, listErr := r.st.ListRequests(ctx, existingCollection.ID)
			if listErr != nil {
				return failed, fmt.Errorf(
					"list requests of %q: %w", existingCollection.Name, listErr,
				)
			}
			names := make(map[string]bool, len(existingRequests))
			for _, existingRequest := range existingRequests {
				names[existingRequest.Name] = true
			}
			existingRequestNamesByCollection[existingCollection.ID] = names
		}
	}

	logger.Logf("tx begin name=%s", name)
	tx, err := r.st.BeginTransaction(ctx)
	if err != nil {
		logger.Logf("tx begin failed name=%s err=%v", name, err)
		return failed, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var col *domain.Collection

	if outcome == OutcomeReplaced {
		logger.Logf("deleting existing collection id=%s", existingID)
		if err := tx.DeleteCollection(ctx, existingID); err != nil {
			logger.Logf("delete existing failed id=%s err=%v", existingID, err)
			return failed, fmt.Errorf("delete existing collection: %w", err)
		}
	}

	if outcome == OutcomeMerged {
		col = findRootCollection(allCollections, func(c *domain.Collection) bool {
			return c.ID == existingID
		})
		if col == nil {
			logger.Logf(
				"merge failed: existing collection not found name=%s id=%s",
				name,
				existingID,
			)
			return failed, errors.New("merge: existing collection not found")
		}
		logger.Logf("merge target found id=%s name=%s", col.ID, col.Name)
	} else {
		col = &domain.Collection{Name: name}
		if err := tx.SaveCollection(ctx, col); err != nil {
			logger.Logf("save collection failed name=%s err=%v", name, err)
			return failed, fmt.Errorf("save collection: %w", err)
		}
		logger.Logf("collection created id=%s name=%s", col.ID, col.Name)
	}
	groups := parsed.Groups
	if len(groups) == 0 && len(parsed.Requests) > 0 {
		groups = []postman.RequestGroup{{Requests: parsed.Requests}}
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
			if outcome == OutcomeMerged {
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
					return failed, fmt.Errorf("save nested collection %q: %w", pathSoFar, err)
				}
			}
			collectionsByPath[pathSoFar] = child
			parent = child
		}
	}

	variableWarnings, err := mergeCollectionVariablesIntoRootDefault(
		ctx, tx, col.ID, parsed.CollectionVariables,
	)
	if err != nil {
		return failed, fmt.Errorf("save collection variables: %w", err)
	}
	warnings := slices.Concat(parsed.Warnings, variableWarnings)
	sort.Strings(warnings)

	imported := 0
	for _, group := range groups {
		target := collectionsByPath[group.Path]
		if target == nil {
			continue
		}
		existingNames := make(map[string]bool)
		if outcome == OutcomeMerged {
			for existingName := range existingRequestNamesByCollection[target.ID] {
				existingNames[existingName] = true
			}
		}
		requestsToSave := append([]*domain.Request(nil), group.Requests...)
		if outcome == OutcomeMerged {
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
				return failed, fmt.Errorf("save request %q: %w", req.Name, err)
			}
			imported++
		}
	}
	logger.Logf("saved requests count=%d", imported)

	if err := tx.Commit(); err != nil {
		logger.Logf("tx commit failed name=%s err=%v", name, err)
		return failed, fmt.Errorf("commit: %w", err)
	}
	logger.Logf("tx committed name=%s", name)

	return CollectionResult{
		FilePath: path,
		Name:     name,
		ID:       col.ID,
		Outcome:  outcome,
		Imported: imported,
		Total:    len(parsed.Requests),
		Warnings: warnings,
		Security: parsed.Security,
	}, nil
}

// resolveConflict returns the action for name and the ID of the existing root
// collection it conflicts with ("" when there is no conflict).
func (r *run) resolveConflict(
	ctx context.Context,
	name string,
	incoming []*domain.Request,
) (Action, string, error) {
	cols, err := r.st.ListCollections(ctx)
	if err != nil {
		r.logger.Logf("list collections failed: %v", err)
		return "", "", fmt.Errorf("list collections: %w", err)
	}

	existing := findRootCollection(cols, func(c *domain.Collection) bool {
		return c.Name == name
	})
	if existing == nil {
		r.logger.Logf("no conflict name=%s", name)
		return ActionDuplicate, "", nil
	}
	r.logger.Logf("conflict detected name=%s existingID=%s", name, existing.ID)

	if r.allAction != "" {
		r.logger.Logf("using apply-to-all action=%s", r.allAction)
		return r.allAction, existing.ID, nil
	}
	if r.opts.Resolve == nil {
		r.logger.Logf("configured action=%s", r.opts.OnDuplicate)
		return r.opts.OnDuplicate, existing.ID, nil
	}

	existingRequests, err := r.st.ListRequests(ctx, existing.ID)
	if err != nil {
		return "", "", fmt.Errorf("list requests of %q: %w", name, err)
	}
	action, applyToAll, err := r.opts.Resolve(ctx, Conflict{
		Name:             name,
		ExistingID:       existing.ID,
		ExistingRequests: existingRequests,
		IncomingRequests: incoming,
	})
	if err != nil {
		return "", "", fmt.Errorf("resolve duplicate %q: %w", name, err)
	}
	if !action.Valid() {
		action = ActionDuplicate
	}
	if applyToAll {
		r.allAction = action
	}
	r.logger.Logf("resolver action=%s applyToAll=%t", action, applyToAll)
	return action, existing.ID, nil
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
				warnings = append(
					warnings,
					fmt.Sprintf(
						"collection variable %q conflicts with root default; existing value kept",
						key,
					),
				)
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

// findRootCollection returns the first top-level collection that match accepts,
// or nil. Nested folders are never returned: duplicate, merge and replace
// decisions apply to root collections only.
func findRootCollection(
	cols []*domain.Collection,
	match func(*domain.Collection) bool,
) *domain.Collection {
	for _, c := range cols {
		if c.ParentID == "" && match(c) {
			return c
		}
	}
	return nil
}

// uniqueCollectionName returns name, or name with the lowest free " N" suffix
// when a root collection already has it.
func uniqueCollectionName(
	ctx context.Context,
	st store.CollectionLister,
	name string,
) (string, error) {
	cols, err := st.ListCollections(ctx)
	if err != nil {
		return "", fmt.Errorf("list collections: %w", err)
	}

	existing := make(map[string]bool)
	for _, c := range cols {
		if c.ParentID == "" {
			existing[c.Name] = true
		}
	}

	if !existing[name] {
		return name, nil
	}

	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s %d", name, i)
		if !existing[candidate] {
			return candidate, nil
		}
	}
}
