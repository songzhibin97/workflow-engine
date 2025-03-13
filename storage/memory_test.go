package storage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/songzhibin97/workflow-engine/types"
	"github.com/stretchr/testify/assert"
)

func TestMemoryStorage(t *testing.T) {
	// Helper function to create a sample workflow
	newWorkflow := func(id uint64) types.Workflow {
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
	newInstance := func(id uint64, state string) types.WorkflowInstance {
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

	t.Run("NewMemoryStorage", func(t *testing.T) {
		store := NewMemoryStorage()
		assert.NotNil(t, store)
		assert.NotNil(t, store.workflows)
		assert.NotNil(t, store.instances)
		assert.Empty(t, store.workflows)
		assert.Empty(t, store.instances)
	})

	t.Run("SaveAndGetWorkflow", func(t *testing.T) {
		store := NewMemoryStorage()
		ctx := context.Background()

		wf := newWorkflow(1)
		err := store.SaveWorkflow(ctx, wf)
		assert.NoError(t, err)

		got, err := store.GetWorkflow(ctx, 1)
		assert.NoError(t, err)
		assert.Equal(t, wf, got)

		_, err = store.GetWorkflow(ctx, 2)
		assert.ErrorIs(t, err, ErrWorkflowNotFound)
	})

	t.Run("SaveAndGetInstance", func(t *testing.T) {
		store := NewMemoryStorage()
		ctx := context.Background()

		inst := newInstance(1, "running")
		err := store.SaveInstance(ctx, inst)
		assert.NoError(t, err)

		got, err := store.GetInstance(ctx, 1)
		assert.NoError(t, err)
		assert.Equal(t, inst, got)

		_, err = store.GetInstance(ctx, 2)
		assert.ErrorIs(t, err, ErrInstanceNotFound)
	})

	t.Run("SaveWorkflows", func(t *testing.T) {
		store := NewMemoryStorage()
		ctx := context.Background()

		wfs := []types.Workflow{newWorkflow(1), newWorkflow(2), newWorkflow(3)}
		err := store.SaveWorkflows(ctx, wfs)
		assert.NoError(t, err)

		for _, wf := range wfs {
			got, err := store.GetWorkflow(ctx, wf.ID)
			assert.NoError(t, err)
			assert.Equal(t, wf, got)
		}
	})

	t.Run("ClearCompleted", func(t *testing.T) {
		store := NewMemoryStorage()
		ctx := context.Background()

		// Save some instances
		err := store.SaveInstance(ctx, newInstance(1, "running"))
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
		assert.ErrorIs(t, err, ErrInstanceNotFound) // Should be cleared (completed)
		_, err = store.GetInstance(ctx, 3)
		assert.ErrorIs(t, err, ErrInstanceNotFound) // Should be cleared (failed)
	})

	t.Run("ContextCancellation", func(t *testing.T) {
		store := NewMemoryStorage()
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		err := store.SaveWorkflow(ctx, newWorkflow(1))
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
		store := NewMemoryStorage()
		ctx := context.Background()
		var wgWrite sync.WaitGroup
		var wgRead sync.WaitGroup

		// Concurrent writes
		for i := 0; i < 100; i++ {
			wgWrite.Add(1)
			go func(id int) {
				defer wgWrite.Done()
				err := store.SaveWorkflow(ctx, newWorkflow(uint64(id)))
				if err != nil {
					t.Errorf("SaveWorkflow failed for id=%d: %v", id, err)
				}
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
}

func TestGetItem(t *testing.T) {
	ctx := context.Background()
	m := map[uint64]string{1: "one", 2: "two"}

	t.Run("Found", func(t *testing.T) {
		result, err := getItem(ctx, m, 1, errors.New("not found"))
		assert.NoError(t, err)
		assert.Equal(t, "one", result)
	})

	t.Run("NotFound", func(t *testing.T) {
		_, err := getItem(ctx, m, 3, errors.New("not found"))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found: id=3")
	})

	t.Run("CanceledContext", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := getItem(ctx, m, 1, errors.New("not found"))
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestWithContext(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		ctx := context.Background()
		result, err := withContext(ctx, func() (string, error) {
			return "success", nil
		})
		assert.NoError(t, err)
		assert.Equal(t, "success", result)
	})

	t.Run("Error", func(t *testing.T) {
		ctx := context.Background()
		_, err := withContext(ctx, func() (string, error) {
			return "", errors.New("fail")
		})
		assert.Error(t, err)
		assert.Equal(t, "fail", err.Error())
	})

	t.Run("CanceledContext", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := withContext(ctx, func() (string, error) {
			return "success", nil
		})
		assert.ErrorIs(t, err, context.Canceled)
	})
}
