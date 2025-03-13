package storage

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/songzhibin97/workflow-engine/types"
	"github.com/stretchr/testify/assert"
)

// Helper function to create a sample workflow
func newWorkflow(id uint64) types.Workflow {
	return types.Workflow{
		ID:   id,
		Name: "Test Workflow",
		Nodes: []types.Node{
			{ID: 1, Type: "start"},
			{ID: 2, Type: "end"},
		},
		Transitions: []types.Transition{
			{FromNodeID: 1, ToNodeID: 2, Condition: "true"},
		},
	}
}

// Helper function to create a sample instance
func newInstance(id uint64, state string) types.WorkflowInstance {
	return types.WorkflowInstance{
		ID:            id,
		WorkflowID:    1,
		State:         state,
		CurrentNodeID: 1,
		Context:       map[string]interface{}{"key": "value"},
		CreatedAt:     time.Now().UnixMilli(),
		UpdatedAt:     time.Now().UnixMilli(),
	}
}

func TestRedisStorage(t *testing.T) {
	// Setup Redis options (assumes Redis is running locally)
	opts := RedisOptions{
		Addr:         "localhost:6379",
		Password:     "",
		DB:           0,
		PoolSize:     10,
		MinIdleConns: 2,
		IdleTimeout:  5 * time.Minute,
	}

	t.Run("NewRedisStorage", func(t *testing.T) {
		store, err := NewRedisStorage(opts)
		assert.NoError(t, err)
		assert.NotNil(t, store)
		assert.NotNil(t, store.client)
		defer store.Close()

		// Test connection failure
		badOpts := opts
		badOpts.Addr = "invalid:6379"
		_, err = NewRedisStorage(badOpts)
		assert.Error(t, err)
	})

	t.Run("SaveAndGetWorkflow", func(t *testing.T) {
		store, err := NewRedisStorage(opts)
		assert.NoError(t, err)
		defer store.Close()
		ctx := context.Background()

		wf := newWorkflow(1)
		err = store.SaveWorkflow(ctx, wf)
		assert.NoError(t, err)

		got, err := store.GetWorkflow(ctx, 1)
		assert.NoError(t, err)
		assert.Equal(t, wf, got)

		_, err = store.GetWorkflow(ctx, 999)
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("SaveAndGetInstance", func(t *testing.T) {
		store, err := NewRedisStorage(opts)
		assert.NoError(t, err)
		defer store.Close()
		ctx := context.Background()

		inst := newInstance(1, "running")
		err = store.SaveInstance(ctx, inst)
		assert.NoError(t, err)

		got, err := store.GetInstance(ctx, 1)
		assert.NoError(t, err)
		assert.Equal(t, inst, got)

		_, err = store.GetInstance(ctx, 999)
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("SaveWorkflows", func(t *testing.T) {
		store, err := NewRedisStorage(opts)
		assert.NoError(t, err)
		defer store.Close()
		ctx := context.Background()

		wfs := []types.Workflow{newWorkflow(1), newWorkflow(2), newWorkflow(3)}
		err = store.SaveWorkflows(ctx, wfs)
		assert.NoError(t, err)

		for _, wf := range wfs {
			got, err := store.GetWorkflow(ctx, wf.ID)
			assert.NoError(t, err)
			assert.Equal(t, wf, got)
		}
	})

	t.Run("ClearCompleted", func(t *testing.T) {
		store, err := NewRedisStorage(opts)
		assert.NoError(t, err)
		defer store.Close()
		ctx := context.Background()

		// Save some instances
		err = store.SaveInstance(ctx, newInstance(1, "running"))
		assert.NoError(t, err)
		err = store.SaveInstance(ctx, newInstance(2, "completed"))
		assert.NoError(t, err)
		err = store.SaveInstance(ctx, newInstance(3, "failed"))
		assert.NoError(t, err)

		// Clear completed and failed instances
		err = store.ClearCompleted(ctx)
		assert.NoError(t, err)

		// Check remaining instances
		_, err = store.GetInstance(ctx, 1)
		assert.NoError(t, err) // Should still exist (running)
		_, err = store.GetInstance(ctx, 2)
		assert.ErrorIs(t, err, ErrNotFound) // Should be cleared (completed)
		_, err = store.GetInstance(ctx, 3)
		assert.ErrorIs(t, err, ErrNotFound) // Should be cleared (failed)
	})

	t.Run("ContextCancellation", func(t *testing.T) {
		store, err := NewRedisStorage(opts)
		assert.NoError(t, err)
		defer store.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		err = store.SaveWorkflow(ctx, newWorkflow(1))
		assert.ErrorIs(t, err, context.Canceled)

		_, err = store.GetWorkflow(ctx, 1)
		assert.ErrorIs(t, err, context.Canceled)

		err = store.SaveInstance(ctx, newInstance(1, "running"))
		assert.ErrorIs(t, err, context.Canceled)

		_, err = store.GetInstance(ctx, 1)
		assert.ErrorIs(t, err, context.Canceled)

		err = store.SaveWorkflows(ctx, []types.Workflow{newWorkflow(1)})
		assert.ErrorIs(t, err, context.Canceled)

		err = store.ClearCompleted(ctx)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("ConcurrentAccess", func(t *testing.T) {
		store, err := NewRedisStorage(opts)
		assert.NoError(t, err)
		defer store.Close()
		ctx := context.Background()

		var wgWrite sync.WaitGroup
		var wgRead sync.WaitGroup

		// Concurrent writes
		for i := 0; i < 100; i++ {
			wgWrite.Add(1)
			go func(id int) {
				defer wgWrite.Done()
				err := store.SaveWorkflow(ctx, newWorkflow(uint64(id)))
				assert.NoError(t, err)
			}(i)
		}

		// Wait for all writes to complete
		wgWrite.Wait()

		// Concurrent reads
		errors := make(chan error, 100)
		for i := 0; i < 100; i++ {
			wgRead.Add(1)
			go func(id int) {
				defer wgRead.Done()
				_, err := store.GetWorkflow(ctx, uint64(id))
				if err != nil {
					errors <- fmt.Errorf("GetWorkflow failed for id=%d: %v", id, err)
				}
			}(i)
		}

		// Wait for all reads to complete
		wgRead.Wait()
		close(errors)

		// Check for any errors
		for err := range errors {
			assert.NoError(t, err)
		}

		// Verify all workflows are present
		for i := 0; i < 100; i++ {
			_, err := store.GetWorkflow(ctx, uint64(i))
			assert.NoError(t, err)
		}
	})

	t.Run("Close", func(t *testing.T) {
		store, err := NewRedisStorage(opts)
		assert.NoError(t, err)
		err = store.Close()
		assert.NoError(t, err)

		// After closing, operations should fail
		ctx := context.Background()
		err = store.SaveWorkflow(ctx, newWorkflow(1))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "closed")
	})
}

func TestGetFromRedis(t *testing.T) {
	opts := RedisOptions{
		Addr: "localhost:6379",
	}
	store, err := NewRedisStorage(opts)
	assert.NoError(t, err)
	defer store.Close()
	ctx := context.Background()

	t.Run("Found", func(t *testing.T) {
		wf := newWorkflow(100)
		err := store.SaveWorkflow(ctx, wf)
		assert.NoError(t, err)

		result, err := getFromRedis[types.Workflow](ctx, store.client, workflowPrefix, 100)
		assert.NoError(t, err)
		assert.Equal(t, wf, result)
	})

	t.Run("NotFound", func(t *testing.T) {
		_, err := getFromRedis[types.Workflow](ctx, store.client, workflowPrefix, 999)
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("CanceledContext", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := getFromRedis[types.Workflow](ctx, store.client, workflowPrefix, 100)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestWithContextError(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		ctx := context.Background()
		err := withContextError(ctx, func() error {
			return nil
		})
		assert.NoError(t, err)
	})

	t.Run("Error", func(t *testing.T) {
		ctx := context.Background()
		err := withContextError(ctx, func() error {
			return fmt.Errorf("fail")
		})
		assert.Error(t, err)
		assert.Equal(t, "fail", err.Error())
	})

	t.Run("CanceledContext", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := withContextError(ctx, func() error {
			return nil
		})
		assert.ErrorIs(t, err, context.Canceled)
	})
}
