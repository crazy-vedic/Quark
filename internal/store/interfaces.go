package store

import (
	"context"
	"time"

	"github.com/crazy-vedic/quark/internal/domain"
)

// CollectionLister is the read interface for collection storage.
//
// Sort contract: ListCollections always returns collections sorted
// alphabetically by name. This is guaranteed by the SQL ORDER BY clause
// and must be preserved by any test fake.
type CollectionLister interface {
	// ListCollections returns all collections sorted alphabetically by name.
	// Returns nil, nil when no collections exist (not an empty slice).
	// Returns nil, err on failure — never returns partial results alongside error.
	ListCollections(ctx context.Context) ([]*domain.Collection, error)
}

// NestedCollectionStore exposes hierarchy operations while preserving the
// original flat collection interfaces for existing callers.
type NestedCollectionStore interface {
	ListRootCollections(context.Context) ([]*domain.Collection, error)
	ListChildCollections(context.Context, string) ([]*domain.Collection, error)
	MoveCollection(context.Context, string, string) error
	CollectionPath(context.Context, string) (string, error)
	CountDescendants(context.Context, string) (int, int, error)
}

// RequestReader is the read interface for request storage.
type RequestReader interface {
	// GetRequest returns the request with the given ID.
	// Returns nil, ErrNotFound if no request exists with that ID.
	GetRequest(ctx context.Context, id string) (*domain.Request, error)

	// ListRequests returns all requests in a collection ordered by
	// sort_order ASC, then created_at ASC, then id ASC.
	// Returns nil, nil when the collection exists but has no requests.
	// Returns nil, ErrNotFound if collectionID does not exist.
	ListRequests(ctx context.Context, collectionID string) ([]*domain.Request, error)
}

// RequestWriter is the mutation interface for request storage.
type RequestWriter interface {
	SaveRequest(ctx context.Context, req *domain.Request) error
	DeleteRequest(ctx context.Context, id string) error
}

// CollectionWriter is the mutation interface for collection storage.
type CollectionWriter interface {
	SaveCollection(ctx context.Context, c *domain.Collection) error
	DeleteCollection(ctx context.Context, id string) error
}

// EnvironmentReader is the read interface for environment storage.
type EnvironmentReader interface {
	// GetEnvironment returns the environment with the given ID.
	// Returns nil, ErrNotFound if no environment exists with that ID.
	GetEnvironment(ctx context.Context, id string) (*domain.Environment, error)

	// GetGlobalEnvironment returns the global environment.
	// Returns nil, ErrNotFound if the global environment does not exist.
	GetGlobalEnvironment(ctx context.Context) (*domain.Environment, error)

	// ListEnvironments returns all environments for a collection.
	// If collectionID is empty, returns global environments.
	// Ordered by sort_order ASC, then name ASC.
	ListEnvironments(ctx context.Context, collectionID string) ([]*domain.Environment, error)

	// ListCollectionEnvironments is a convenience alias for ListEnvironments with a non-empty collectionID.
	ListCollectionEnvironments(
		ctx context.Context,
		collectionID string,
	) ([]*domain.Environment, error)

	// ListAllEnvironments returns all non-global environments across all collections
	// in a single query. Ordered by collection_id, sort_order, name.
	ListAllEnvironments(ctx context.Context) ([]*domain.Environment, error)
}

// EnvironmentWriter is the mutation interface for environment storage.
type EnvironmentWriter interface {
	// SaveEnvironment inserts or updates an environment.
	// Returns ErrDuplicate if the environment name already exists for the collection.
	SaveEnvironment(ctx context.Context, env *domain.Environment) error

	// DeleteEnvironment deletes an environment by ID.
	// Returns ErrNotFound if the environment does not exist.
	DeleteEnvironment(ctx context.Context, id string) error

	// CreateDefaultEnvironment creates a default environment for a collection.
	// Returns ErrDuplicate if the collection already has a default environment.
	CreateDefaultEnvironment(ctx context.Context, collectionID string) (*domain.Environment, error)
}

// ActiveEnvironmentStore manages which environment is active per collection.
type ActiveEnvironmentStore interface {
	SetActiveEnvironment(ctx context.Context, collectionID, envID string) error
	GetActiveEnvironment(ctx context.Context, collectionID string) (string, error)
}

// ExecutionReader is the read interface for persisted request history.
type ExecutionReader interface {
	// ListExecutionsByRequest returns executions for a request ordered by
	// completed_at DESC, started_at DESC, id DESC.
	// Returns nil, nil when no executions exist for the request.
	ListExecutionsByRequest(ctx context.Context, requestID string) ([]*domain.Execution, error)
}

// ExecutionWriter is the mutation interface for persisted request history.
type ExecutionWriter interface {
	SaveExecution(ctx context.Context, ex *domain.Execution) error
}

// ScheduledRunReader reads delayed request executions.
type ScheduledRunReader interface {
	GetScheduledRun(ctx context.Context, id string) (*domain.ScheduledRun, error)
	ListScheduledRuns(ctx context.Context) ([]*domain.ScheduledRun, error)
	ListDueScheduledRuns(ctx context.Context, now time.Time) ([]*domain.ScheduledRun, error)
	NextPendingScheduledRun(ctx context.Context) (*domain.ScheduledRun, error)
}

// ScheduledRunWriter mutates delayed request executions.
type ScheduledRunWriter interface {
	SaveScheduledRun(ctx context.Context, run *domain.ScheduledRun) error
	DeleteScheduledRun(ctx context.Context, id string) error
}

// ScheduledRunStore reads and mutates delayed request executions.
type ScheduledRunStore interface {
	ScheduledRunReader
	ScheduledRunWriter
}

// TransactionalWriter combines collection, request, and environment mutation within a single
// database transaction. Call Commit() to persist or Rollback() to discard.
type TransactionalWriter interface {
	CollectionWriter
	RequestWriter
	EnvironmentWriter
	Commit() error
	Rollback() error
}
