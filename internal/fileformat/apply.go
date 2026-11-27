package fileformat

import (
	"context"
	"fmt"
	"sort"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

// DuplicateAction says what Apply does when a root collection with the same
// name already exists. The values match 'quark import-postman --on-duplicate'.
type DuplicateAction string

const (
	ActionReplace   DuplicateAction = "replace"
	ActionDuplicate DuplicateAction = "duplicate"
	ActionMerge     DuplicateAction = "merge"
	ActionSkip      DuplicateAction = "skip"
)

// ParseDuplicateAction validates an --on-duplicate value.
func ParseDuplicateAction(s string) (DuplicateAction, error) {
	switch a := DuplicateAction(s); a {
	case ActionReplace, ActionDuplicate, ActionMerge, ActionSkip:
		return a, nil
	}
	return "", fmt.Errorf(
		"invalid --on-duplicate %q: must be replace, duplicate, merge, or skip", s,
	)
}

// ApplyStore is what Apply needs from the store.
type ApplyStore interface {
	store.CollectionLister
	ListRequests(ctx context.Context, collectionID string) ([]*domain.Request, error)
	ListCollectionEnvironments(
		ctx context.Context,
		collectionID string,
	) ([]*domain.Environment, error)
	BeginTransaction(ctx context.Context) (store.TransactionalWriter, error)
}

// Result summarises one Apply call.
type Result struct {
	CollectionName string
	// Action is the duplicate action taken, or empty when no collection of
	// that name existed.
	Action       DuplicateAction
	Imported     int
	Skipped      int
	Environments int
	Warnings     []string
}

// Apply saves c as a root collection in one transaction. When a root
// collection with the same name exists, action decides what happens:
// replace deletes it first, duplicate saves under a new name ("Name 1"),
// merge adds requests, sub-collections and variables that it does not have
// yet, and skip saves nothing.
func Apply(
	ctx context.Context,
	st ApplyStore,
	c *Collection,
	action DuplicateAction,
) (Result, error) {
	res := Result{CollectionName: c.Name}
	all, err := st.ListCollections(ctx)
	if err != nil {
		return res, fmt.Errorf("list collections: %w", err)
	}
	rootNames := map[string]bool{}
	var existing *domain.Collection
	for _, col := range all {
		if col.ParentID != "" {
			continue
		}
		rootNames[col.Name] = true
		if col.Name == c.Name {
			existing = col
		}
	}

	a := &applier{merge: map[string]*mergeState{}}
	if existing != nil {
		res.Action = action
		switch action {
		case ActionSkip:
			res.Skipped = c.CountRequests()
			return res, nil
		case ActionDuplicate:
			res.CollectionName = uniqueName(c.Name, rootNames)
		case ActionMerge:
			if err := a.readExisting(ctx, st, all); err != nil {
				return res, err
			}
		}
	}

	// Store uses one SQLite connection, so every read happens before the
	// transaction starts.
	tx, err := st.BeginTransaction(ctx)
	if err != nil {
		return res, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	a.tx = tx

	var target *domain.Collection
	switch {
	case existing != nil && action == ActionReplace:
		if err := tx.DeleteCollection(ctx, existing.ID); err != nil {
			return res, fmt.Errorf("delete existing collection: %w", err)
		}
	case existing != nil && action == ActionMerge:
		target = existing
	}
	if err := a.apply(ctx, "", res.CollectionName, c, target, &res); err != nil {
		return res, err
	}
	if err := tx.Commit(); err != nil {
		return res, fmt.Errorf("commit: %w", err)
	}
	sort.Strings(res.Warnings)
	return res, nil
}

type mergeState struct {
	children map[string]*domain.Collection
	existing map[string]bool // request names saved before this import
	requests map[string]bool // request names in use, including imported ones
	envs     map[string]*domain.Environment
	count    int
}

type applier struct {
	tx    store.TransactionalWriter
	merge map[string]*mergeState // by existing collection ID
}

func (a *applier) readExisting(
	ctx context.Context,
	st ApplyStore,
	all []*domain.Collection,
) error {
	for _, col := range all {
		state := a.state(col.ID)
		reqs, err := st.ListRequests(ctx, col.ID)
		if err != nil {
			return fmt.Errorf("list requests of %q: %w", col.Name, err)
		}
		for _, req := range reqs {
			state.existing[req.Name] = true
			state.requests[req.Name] = true
		}
		state.count = len(reqs)
		envs, err := st.ListCollectionEnvironments(ctx, col.ID)
		if err != nil {
			return fmt.Errorf("list environments of %q: %w", col.Name, err)
		}
		for _, env := range envs {
			state.envs[env.Name] = env
		}
		if col.ParentID != "" {
			a.state(col.ParentID).children[col.Name] = col
		}
	}
	return nil
}

func (a *applier) state(id string) *mergeState {
	s, ok := a.merge[id]
	if !ok {
		s = &mergeState{
			children: map[string]*domain.Collection{},
			existing: map[string]bool{},
			requests: map[string]bool{},
			envs:     map[string]*domain.Environment{},
		}
		a.merge[id] = s
	}
	return s
}

// apply saves c under parentID. target is the existing collection to merge
// into, or nil to create a new one named name.
func (a *applier) apply(
	ctx context.Context,
	parentID, name string,
	c *Collection,
	target *domain.Collection,
	res *Result,
) error {
	col := target
	if col == nil {
		col = &domain.Collection{
			ParentID:    parentID,
			Name:        name,
			Description: c.Description,
			Meta:        c.Meta,
		}
		if err := a.tx.SaveCollection(ctx, col); err != nil {
			return fmt.Errorf("save collection %q: %w", name, err)
		}
	}
	state := a.state(col.ID)

	order := state.count
	for _, src := range c.Requests {
		if state.existing[src.Name] {
			res.Skipped++
			continue
		}
		req := *src
		req.ID = ""
		req.CollectionID = col.ID
		req.Name = uniqueRequestName(src.Name, state.requests)
		req.SortOrder = order
		order++
		if err := a.tx.SaveRequest(ctx, &req); err != nil {
			return fmt.Errorf("save request %q: %w", req.Name, err)
		}
		res.Imported++
	}

	for _, env := range c.Environments {
		if err := a.applyEnvironment(ctx, col, env, state, res); err != nil {
			return err
		}
	}

	for _, child := range c.Children {
		var childTarget *domain.Collection
		if target != nil {
			childTarget = state.children[child.Name]
		}
		if err := a.apply(ctx, col.ID, child.Name, child, childTarget, res); err != nil {
			return err
		}
	}
	return nil
}

// applyEnvironment adds env to col. When col already has an environment of
// that name, missing variables are added and existing values are kept.
func (a *applier) applyEnvironment(
	ctx context.Context,
	col *domain.Collection,
	env Environment,
	state *mergeState,
	res *Result,
) error {
	existing := state.envs[env.Name]
	if existing == nil && env.Name == defaultEnvironment {
		// Saving a collection creates its default environment.
		def, err := a.tx.CreateDefaultEnvironment(ctx, col.ID)
		if err != nil {
			return fmt.Errorf("default environment of %q: %w", col.Name, err)
		}
		existing = def
	}
	if existing == nil {
		created := &domain.Environment{CollectionID: col.ID, Name: env.Name}
		created.SetVars(env.Vars)
		if err := a.tx.SaveEnvironment(ctx, created); err != nil {
			return fmt.Errorf("save environment %q of %q: %w", env.Name, col.Name, err)
		}
		state.envs[env.Name] = created
		res.Environments++
		return nil
	}
	vars, err := existing.DecodeVars()
	if err != nil {
		return fmt.Errorf("environment %q of %q: %w", env.Name, col.Name, err)
	}
	keys := make([]string, 0, len(env.Vars))
	for key := range env.Vars {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	changed := false
	for _, key := range keys {
		if current, ok := vars[key]; ok {
			if current != env.Vars[key] {
				res.Warnings = append(res.Warnings, fmt.Sprintf(
					"%s: environment %q variable %q already set; existing value kept",
					col.Name, env.Name, key,
				))
			}
			continue
		}
		vars[key] = env.Vars[key]
		changed = true
	}
	state.envs[env.Name] = existing
	res.Environments++
	if !changed {
		return nil
	}
	updated := *existing
	updated.SetVars(vars)
	if err := a.tx.SaveEnvironment(ctx, &updated); err != nil {
		return fmt.Errorf("save environment %q of %q: %w", env.Name, col.Name, err)
	}
	state.envs[env.Name] = &updated
	return nil
}

// uniqueName returns name, or "name N" with the smallest N not in used.
// It matches how import-postman names duplicated collections.
func uniqueName(name string, used map[string]bool) string {
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

// uniqueRequestName returns a name not in used ("name (N)" on a clash) and
// records it.
func uniqueRequestName(name string, used map[string]bool) string {
	candidate := name
	for i := 1; used[candidate]; i++ {
		candidate = fmt.Sprintf("%s (%d)", name, i)
	}
	used[candidate] = true
	return candidate
}
