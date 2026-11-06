package cli

import (
	"context"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

type completionTestStore struct {
	cols map[string]*domain.Collection
	reqs map[string][]*domain.Request
	envs map[string][]*domain.Environment
}

func newCompletionTestStore() *completionTestStore {
	api := &domain.Collection{ID: "col-api", Name: "API"}
	billing := &domain.Collection{ID: "col-billing", Name: "Billing"}
	return &completionTestStore{
		cols: map[string]*domain.Collection{
			api.ID:     api,
			billing.ID: billing,
		},
		reqs: map[string][]*domain.Request{
			api.ID: {
				{ID: "req-1", CollectionID: api.ID, Method: "GET", Name: "List Payments"},
				{ID: "req-2", CollectionID: api.ID, Method: "POST", Name: "Create Payment"},
			},
			billing.ID: {
				{ID: "req-3", CollectionID: billing.ID, Method: "GET", Name: "Get Invoice"},
			},
		},
		envs: map[string][]*domain.Environment{
			api.ID: {
				{ID: "env-1", CollectionID: api.ID, Name: "default"},
				{ID: "env-2", CollectionID: api.ID, Name: "staging"},
			},
		},
	}
}

func (s *completionTestStore) ListCollections(context.Context) ([]*domain.Collection, error) {
	return []*domain.Collection{s.cols["col-api"], s.cols["col-billing"]}, nil
}

func (s *completionTestStore) GetRequest(_ context.Context, id string) (*domain.Request, error) {
	for _, reqs := range s.reqs {
		for _, req := range reqs {
			if req.ID == id {
				return req, nil
			}
		}
	}
	return nil, nil
}

func (s *completionTestStore) ListRequests(
	_ context.Context,
	collectionID string,
) ([]*domain.Request, error) {
	return s.reqs[collectionID], nil
}

func (s *completionTestStore) GetEnvironment(
	_ context.Context,
	id string,
) (*domain.Environment, error) {
	for _, envs := range s.envs {
		for _, env := range envs {
			if env.ID == id {
				return env, nil
			}
		}
	}
	return nil, nil
}

func (s *completionTestStore) GetGlobalEnvironment(context.Context) (*domain.Environment, error) {
	return &domain.Environment{ID: "global", Name: "global"}, nil
}

func (s *completionTestStore) ListEnvironments(
	_ context.Context,
	collectionID string,
) ([]*domain.Environment, error) {
	if collectionID == "" {
		return []*domain.Environment{{ID: "global", Name: "global"}}, nil
	}
	return s.envs[collectionID], nil
}

func (s *completionTestStore) ListCollectionEnvironments(
	_ context.Context,
	collectionID string,
) ([]*domain.Environment, error) {
	return s.envs[collectionID], nil
}

func (s *completionTestStore) ListAllEnvironments(context.Context) ([]*domain.Environment, error) {
	var out []*domain.Environment
	for _, envs := range s.envs {
		out = append(out, envs...)
	}
	return out, nil
}

func (s *completionTestStore) SaveEnvironment(context.Context, *domain.Environment) error {
	return nil
}

func (s *completionTestStore) DeleteEnvironment(context.Context, string) error {
	return nil
}

func (s *completionTestStore) CreateDefaultEnvironment(
	context.Context,
	string,
) (*domain.Environment, error) {
	return &domain.Environment{ID: "default", Name: "default"}, nil
}

func (s *completionTestStore) GetEnvironmentByName(
	_ context.Context,
	collectionID, name string,
) (*domain.Environment, error) {
	for _, env := range s.envs[collectionID] {
		if env.Name == name {
			return env, nil
		}
	}
	return nil, nil
}

func (s *completionTestStore) SetActiveEnvironment(context.Context, string, string) error {
	return nil
}

func (s *completionTestStore) GetActiveEnvironment(context.Context, string) (string, error) {
	return "", nil
}

func TestCompleteRequestPathsSuggestsCollectionsFirst(t *testing.T) {
	st := newCompletionTestStore()
	completion := CompleteRequestPaths(st.ListCollections, st.ListRequests)

	items, directive := completion(nilCommand(), nil, "A")
	assert.Equal(t, directiveNoSpace(), directive)
	assert.Equal(t, []string{"API/\tcollection"}, items)
}

func TestCompleteRequestPathsSuggestsFullRequestPaths(t *testing.T) {
	st := newCompletionTestStore()
	completion := CompleteRequestPaths(st.ListCollections, st.ListRequests)

	items, directive := completion(nilCommand(), nil, "API/Cr")
	assert.Equal(t, directiveNoFiles(), directive)
	assert.Equal(t, []string{"API/Create Payment\tPOST"}, items)
}

func TestEnvCommandCompletesCollectionThenEnvironment(t *testing.T) {
	st := newCompletionTestStore()
	cmd := NewEnvCmd(st)

	var deleteCmd *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "delete" {
			deleteCmd = sub
			break
		}
	}
	require.NotNil(t, deleteCmd)
	require.NotNil(t, deleteCmd.ValidArgsFunction)

	items, directive := deleteCmd.ValidArgsFunction(deleteCmd, nil, "col-a")
	assert.Equal(t, directiveNoFiles(), directive)
	assert.Equal(t, []string{"col-api\tAPI"}, items)

	items, directive = deleteCmd.ValidArgsFunction(deleteCmd, []string{"col-api"}, "st")
	assert.Equal(t, directiveNoFiles(), directive)
	assert.Equal(t, []string{"staging"}, items)
}

func TestRequestListRegistersCollectionFlagCompletion(t *testing.T) {
	st := newCompletionTestStore()
	cmd := NewRequestCmd(st)

	var listCmd *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "list" {
			listCmd = sub
			break
		}
	}
	require.NotNil(t, listCmd)

	completion, ok := listCmd.GetFlagCompletionFunc("collection")
	require.True(t, ok)

	items, directive := completion(listCmd, nil, "bill")
	assert.Equal(t, directiveNoFiles(), directive)
	assert.Equal(t, []string{"col-billing\tBilling"}, items)
}

func nilCommand() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	return cmd
}

func directiveNoFiles() cobra.ShellCompDirective {
	return cobra.ShellCompDirectiveNoFileComp
}

func directiveNoSpace() cobra.ShellCompDirective {
	return cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
}
