package workflow

import "context"

// Action defines the interface for task execution in the workflow.
type Action interface {
	// Execute runs the action synchronously.
	Execute(ctx context.Context, context map[string]interface{}) (interface{}, error)

	// ExecuteAsync runs the action asynchronously, returning channels for result and error.
	ExecuteAsync(ctx context.Context, context map[string]interface{}) (chan interface{}, chan error)
}
