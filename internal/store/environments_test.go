package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

// --- Global Environment ---

func TestStore_GlobalEnvironment_AutoCreated(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	global, err := s.GetGlobalEnvironment(ctx)
	require.NoError(t, err)
	assert.True(t, global.IsGlobal())
	assert.Equal(t, "global", global.Name)
	assert.Equal(t, "{}", global.Data)
}

func TestStore_GlobalEnvironment_ExistsOnlyOnce(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	global1, err := s.GetGlobalEnvironment(ctx)
	require.NoError(t, err)

	global2, err := s.GetGlobalEnvironment(ctx)
	require.NoError(t, err)

	assert.Equal(t, global1.ID, global2.ID, "global should be the same record")
}

func TestStore_GlobalEnvironment_SetGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	global, err := s.GetGlobalEnvironment(ctx)
	require.NoError(t, err)

	vars := global.Vars()
	if vars == nil {
		vars = make(map[string]string)
	}
	vars["url"] = "http://localhost:8080"
	global.SetVars(vars)

	require.NoError(t, s.SaveEnvironment(ctx, global))

	got, err := s.GetGlobalEnvironment(ctx)
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:8080", got.Vars()["url"])
}

func TestStore_GlobalEnvironment_Delete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	global, err := s.GetGlobalEnvironment(ctx)
	require.NoError(t, err)

	err = s.DeleteEnvironment(ctx, global.ID)
	require.NoError(t, err, "global environment is deletable")

	_, err = s.GetGlobalEnvironment(ctx)
	assert.ErrorIs(t, err, store.ErrNotFound, "global should be gone after delete")
}

// --- Collection Environment ---

func TestStore_SaveEnvironment_New(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := &domain.Collection{ID: uuid.New().String(), Name: "Col"}
	require.NoError(t, s.SaveCollection(ctx, c))

	env := &domain.Environment{
		ID:           uuid.New().String(),
		CollectionID: c.ID,
		Name:         "dev",
		Data:         `{"url": "http://localhost/"}`,
		SortOrder:    1,
	}
	require.NoError(t, s.SaveEnvironment(ctx, env))

	got, err := s.GetEnvironment(ctx, env.ID)
	require.NoError(t, err)
	assert.Equal(t, "dev", got.Name)
	assert.Equal(t, "http://localhost/", got.Vars()["url"])
	assert.Equal(t, c.ID, got.CollectionID)
}

func TestStore_SaveEnvironment_Update(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := &domain.Collection{ID: uuid.New().String(), Name: "Col"}
	require.NoError(t, s.SaveCollection(ctx, c))

	env := &domain.Environment{
		ID:           uuid.New().String(),
		CollectionID: c.ID,
		Name:         "dev",
		Data:         `{"url": "http://localhost/"}`,
	}
	require.NoError(t, s.SaveEnvironment(ctx, env))

	// Update data
	env.Data = `{"url": "http://updated/"}`
	require.NoError(t, s.SaveEnvironment(ctx, env))

	got, err := s.GetEnvironment(ctx, env.ID)
	require.NoError(t, err)
	assert.Equal(t, "http://updated/", got.Vars()["url"])
}

func TestStore_SaveEnvironment_Duplicate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := &domain.Collection{ID: uuid.New().String(), Name: "Col"}
	require.NoError(t, s.SaveCollection(ctx, c))

	env1 := &domain.Environment{
		ID:           uuid.New().String(),
		CollectionID: c.ID,
		Name:         "dev",
		Data:         `{}`,
	}
	require.NoError(t, s.SaveEnvironment(ctx, env1))

	// Same collection + same name = duplicate
	env2 := &domain.Environment{
		ID:           uuid.New().String(),
		CollectionID: c.ID,
		Name:         "dev",
		Data:         `{}`,
	}
	err := s.SaveEnvironment(ctx, env2)
	assert.ErrorIs(t, err, store.ErrDuplicate)
}

func TestStore_SaveEnvironment_SameNameDifferentCollection(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c1 := &domain.Collection{ID: uuid.New().String(), Name: "Col1"}
	c2 := &domain.Collection{ID: uuid.New().String(), Name: "Col2"}
	require.NoError(t, s.SaveCollection(ctx, c1))
	require.NoError(t, s.SaveCollection(ctx, c2))

	env1 := &domain.Environment{
		ID:           uuid.New().String(),
		CollectionID: c1.ID,
		Name:         "dev",
		Data:         `{}`,
	}
	env2 := &domain.Environment{
		ID:           uuid.New().String(),
		CollectionID: c2.ID,
		Name:         "dev",
		Data:         `{}`,
	}

	require.NoError(t, s.SaveEnvironment(ctx, env1))
	require.NoError(t, s.SaveEnvironment(ctx, env2))

	got1, err := s.GetEnvironment(ctx, env1.ID)
	require.NoError(t, err)
	got2, err := s.GetEnvironment(ctx, env2.ID)
	require.NoError(t, err)

	assert.Equal(t, c1.ID, got1.CollectionID)
	assert.Equal(t, c2.ID, got2.CollectionID)
}

func TestStore_SaveEnvironment_AutoID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := &domain.Collection{ID: uuid.New().String(), Name: "Col"}
	require.NoError(t, s.SaveCollection(ctx, c))

	env := &domain.Environment{
		CollectionID: c.ID,
		Name:         "dev",
		Data:         `{}`,
	}
	require.NoError(t, s.SaveEnvironment(ctx, env))
	assert.NotEmpty(t, env.ID, "ID should be auto-generated")

	got, err := s.GetEnvironment(ctx, env.ID)
	require.NoError(t, err)
	assert.Equal(t, "dev", got.Name)
}

func TestStore_GetEnvironment_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetEnvironment(ctx, uuid.New().String())
	assert.ErrorIs(t, err, store.ErrNotFound)
}

// --- List Environments ---

func TestStore_ListEnvironments_ByCollection(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := &domain.Collection{ID: uuid.New().String(), Name: "Col"}
	require.NoError(t, s.SaveCollection(ctx, c))

	env := &domain.Environment{
		ID:           uuid.New().String(),
		CollectionID: c.ID,
		Name:         "dev",
		Data:         `{"url": "http://localhost/"}`,
	}
	require.NoError(t, s.SaveEnvironment(ctx, env))

	envs, err := s.ListEnvironments(ctx, c.ID)
	require.NoError(t, err)
	require.Len(t, envs, 2) // default + dev

	names := make([]string, len(envs))
	for i, e := range envs {
		names[i] = e.Name
	}
	assert.Equal(t, []string{"default", "dev"}, names)
}

func TestStore_ListEnvironments_Sorted(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := &domain.Collection{ID: uuid.New().String(), Name: "Col"}
	require.NoError(t, s.SaveCollection(ctx, c))

	for _, name := range []string{"dev", "prod", "staging"} {
		env := &domain.Environment{
			ID:           uuid.New().String(),
			CollectionID: c.ID,
			Name:         name,
			Data:         `{}`,
		}
		require.NoError(t, s.SaveEnvironment(ctx, env))
	}

	envs, err := s.ListEnvironments(ctx, c.ID)
	require.NoError(t, err)
	require.Len(t, envs, 4) // default + 3

	// Should be ordered by name ASC when sort_order is same
	names := make([]string, len(envs))
	for i, e := range envs {
		names[i] = e.Name
	}
	assert.Equal(t, []string{"default", "dev", "prod", "staging"}, names)
}

func TestStore_ListEnvironments_Global(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	globals, err := s.ListEnvironments(ctx, "")
	require.NoError(t, err)
	require.Len(t, globals, 1)
	assert.Equal(t, "global", globals[0].Name)
}

func TestStore_ListEnvironments_EmptyCollection(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// A collection that doesn't exist has no environments
	envs, err := s.ListCollectionEnvironments(ctx, uuid.New().String())
	require.NoError(t, err)
	assert.Nil(t, envs)
}

// --- ListAllEnvironments ---

func TestStore_ListAllEnvironments(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c1 := &domain.Collection{ID: uuid.New().String(), Name: "Col1"}
	c2 := &domain.Collection{ID: uuid.New().String(), Name: "Col2"}
	require.NoError(t, s.SaveCollection(ctx, c1))
	require.NoError(t, s.SaveCollection(ctx, c2))

	env1 := &domain.Environment{
		ID:           uuid.New().String(),
		CollectionID: c1.ID,
		Name:         "dev",
		Data:         `{}`,
	}
	env2 := &domain.Environment{
		ID:           uuid.New().String(),
		CollectionID: c2.ID,
		Name:         "prod",
		Data:         `{}`,
	}
	require.NoError(t, s.SaveEnvironment(ctx, env1))
	require.NoError(t, s.SaveEnvironment(ctx, env2))

	all, err := s.ListAllEnvironments(ctx)
	require.NoError(t, err)
	require.Len(t, all, 4) // 2 defaults + 2 custom

	// Verify all expected envs are present, but don't assert cross-collection order
	// because collection_id is a UUID and ordering is non-deterministic across runs.
	nameSet := make(map[string]int, len(all))
	for _, e := range all {
		nameSet[e.Name]++
	}
	assert.Equal(t, 2, nameSet["default"], "expected two default environments")
	assert.Equal(t, 1, nameSet["dev"])
	assert.Equal(t, 1, nameSet["prod"])

	// Within each collection, envs should be ordered by sort_order then name.
	byCol := make(map[string][]string)
	for _, e := range all {
		byCol[e.CollectionID] = append(byCol[e.CollectionID], e.Name)
	}
	for _, names := range byCol {
		assert.LessOrEqual(t, len(names), 2, "each collection has at most 2 envs")
		assert.Equal(t, "default", names[0], "default should come first within each collection")
	}
}

func TestStore_ListAllEnvironments_Empty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	all, err := s.ListAllEnvironments(ctx)
	require.NoError(t, err)
	assert.Nil(t, all)
}

// --- Delete Environment ---

func TestStore_DeleteEnvironment(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := &domain.Collection{ID: uuid.New().String(), Name: "Col"}
	require.NoError(t, s.SaveCollection(ctx, c))

	envs, err := s.ListCollectionEnvironments(ctx, c.ID)
	require.NoError(t, err)
	require.Len(t, envs, 1)

	require.NoError(t, s.DeleteEnvironment(ctx, envs[0].ID))

	envs, err = s.ListCollectionEnvironments(ctx, c.ID)
	require.NoError(t, err)
	assert.Nil(t, envs)
}

func TestStore_DeleteEnvironment_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	err := s.DeleteEnvironment(ctx, uuid.New().String())
	assert.ErrorIs(t, err, store.ErrNotFound)
}

// --- CreateDefaultEnvironment ---

func TestStore_CreateDefaultEnvironment(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := &domain.Collection{ID: uuid.New().String(), Name: "Col"}
	require.NoError(t, s.SaveCollection(ctx, c))

	// Already has default env from auto-creation
	err := s.SaveCollection(ctx, c)
	require.NoError(t, err)

	envs, err := s.ListCollectionEnvironments(ctx, c.ID)
	require.NoError(t, err)
	require.Len(t, envs, 1)
	assert.Equal(t, "default", envs[0].Name)
}

func TestStore_CreateDefaultEnvironment_Idempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := &domain.Collection{ID: uuid.New().String(), Name: "Col"}
	require.NoError(t, s.SaveCollection(ctx, c))

	// Calling CreateDefaultEnvironment again should be idempotent
	// (no error, returns the existing env).
	env, err := s.CreateDefaultEnvironment(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, "default", env.Name)
	assert.Equal(t, c.ID, env.CollectionID)
}

// --- ActiveEnvironment ---

func TestStore_SetActiveEnvironment(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := &domain.Collection{ID: uuid.New().String(), Name: "Col"}
	require.NoError(t, s.SaveCollection(ctx, c))

	envs, err := s.ListCollectionEnvironments(ctx, c.ID)
	require.NoError(t, err)
	require.Len(t, envs, 1)

	err = s.SetActiveEnvironment(ctx, c.ID, envs[0].ID)
	require.NoError(t, err)

	got, err := s.GetActiveEnvironment(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, envs[0].ID, got)
}

func TestStore_GetActiveEnvironment_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := &domain.Collection{ID: uuid.New().String(), Name: "Col"}
	require.NoError(t, s.SaveCollection(ctx, c))

	got, err := s.GetActiveEnvironment(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, "", got, "no active env should return empty string")
}

func TestStore_SetActiveEnvironment_Update(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := &domain.Collection{ID: uuid.New().String(), Name: "Col"}
	require.NoError(t, s.SaveCollection(ctx, c))

	envs, err := s.ListCollectionEnvironments(ctx, c.ID)
	require.NoError(t, err)
	require.Len(t, envs, 1)

	// Set first active env
	err = s.SetActiveEnvironment(ctx, c.ID, envs[0].ID)
	require.NoError(t, err)

	// Create a new env and update to it
	newEnv := &domain.Environment{
		ID:           uuid.New().String(),
		CollectionID: c.ID,
		Name:         "dev",
		Data:         `{}`,
	}
	require.NoError(t, s.SaveEnvironment(ctx, newEnv))

	// Update active env
	err = s.SetActiveEnvironment(ctx, c.ID, newEnv.ID)
	require.NoError(t, err)

	got, err := s.GetActiveEnvironment(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, newEnv.ID, got)
}

// --- Environment.Vars() ---

func TestStore_Environment_VarsRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := &domain.Collection{ID: uuid.New().String(), Name: "Col"}
	require.NoError(t, s.SaveCollection(ctx, c))

	env := &domain.Environment{
		ID:           uuid.New().String(),
		CollectionID: c.ID,
		Name:         "dev",
		Data:         `{"url": "http://localhost/", "key": "secret"}`,
	}
	require.NoError(t, s.SaveEnvironment(ctx, env))

	got, err := s.GetEnvironment(ctx, env.ID)
	require.NoError(t, err)

	vars := got.Vars()
	assert.Equal(t, "http://localhost/", vars["url"])
	assert.Equal(t, "secret", vars["key"])
}

// --- Cascade Delete ---

func TestStore_DeleteCollection_CascadesEnvironments(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := &domain.Collection{ID: uuid.New().String(), Name: "Col"}
	require.NoError(t, s.SaveCollection(ctx, c))

	envs, err := s.ListCollectionEnvironments(ctx, c.ID)
	require.NoError(t, err)
	require.Len(t, envs, 1)

	require.NoError(t, s.DeleteCollection(ctx, c.ID))

	envs, err = s.ListCollectionEnvironments(ctx, c.ID)
	require.NoError(t, err)
	assert.Nil(t, envs)
}
