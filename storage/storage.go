package storage

import (
	"context"

	"github.com/songzhibin97/workflow-engine/types"
)

// Storage defines the interface for persisting and retrieving workflows and instances.
type Storage interface {
	// SaveWorkflow saves a workflow definition.
	SaveWorkflow(ctx context.Context, wf types.Workflow) error

	// GetWorkflow retrieves a workflow by ID.
	GetWorkflow(ctx context.Context, id uint64) (types.Workflow, error)

	// SaveInstance saves a workflow instance.
	SaveInstance(ctx context.Context, inst types.WorkflowInstance) error

	// GetInstance retrieves a workflow instance by ID.
	GetInstance(ctx context.Context, id uint64) (types.WorkflowInstance, error)
}

// withContext is a standalone generic helper function.
func withContext[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	var zero T
	select {
	case <-ctx.Done():
		return zero, ctx.Err()
	default:
		return fn()
	}
}
