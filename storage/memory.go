package storage

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/songzhibin97/workflow-engine/types"
)

// MemoryStorage is an in-memory implementation of the Storage interface.
type MemoryStorage struct {
	workflows map[uint64]types.Workflow
	instances map[uint64]types.WorkflowInstance
	mu        sync.RWMutex
}

// NewMemoryStorage creates a new MemoryStorage instance.
func NewMemoryStorage() *MemoryStorage {
	ms := &MemoryStorage{
		workflows: make(map[uint64]types.Workflow),
		instances: make(map[uint64]types.WorkflowInstance),
	}
	if ms.workflows == nil || ms.instances == nil {
		panic("MemoryStorage initialization failed")
	}
	return ms
}

// getItem is a standalone generic helper function.
func getItem[T any](ctx context.Context, m map[uint64]T, id uint64, errNotFound error) (T, error) {
	return withContext(ctx, func() (T, error) {
		item, ok := m[id]
		if !ok {
			var zero T
			return zero, fmt.Errorf("%w: id=%d", errNotFound, id)
		}
		return item, nil
	})
}

// SaveWorkflow saves a workflow to memory.
func (s *MemoryStorage) SaveWorkflow(ctx context.Context, wf types.Workflow) error {
	_, err := withContext(ctx, func() (struct{}, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.workflows[wf.ID] = wf
		return struct{}{}, nil
	})
	return err
}

// GetWorkflow retrieves a workflow from memory.
func (s *MemoryStorage) GetWorkflow(ctx context.Context, id uint64) (types.Workflow, error) {
	return getItem(ctx, s.workflows, id, ErrWorkflowNotFound)
}

// SaveInstance saves a workflow instance to memory.
func (s *MemoryStorage) SaveInstance(ctx context.Context, inst types.WorkflowInstance) error {
	_, err := withContext(ctx, func() (struct{}, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.instances[inst.ID] = inst
		return struct{}{}, nil
	})
	return err
}

// GetInstance retrieves a workflow instance from memory.
func (s *MemoryStorage) GetInstance(ctx context.Context, id uint64) (types.WorkflowInstance, error) {
	return getItem(ctx, s.instances, id, ErrInstanceNotFound)
}

// SaveWorkflows saves multiple workflows in a single lock.
func (s *MemoryStorage) SaveWorkflows(ctx context.Context, wfs []types.Workflow) error {
	_, err := withContext(ctx, func() (struct{}, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, wf := range wfs {
			s.workflows[wf.ID] = wf
		}
		return struct{}{}, nil
	})
	return err
}

// ClearCompleted removes completed or failed instances.
func (s *MemoryStorage) ClearCompleted(ctx context.Context) error {
	_, err := withContext(ctx, func() (struct{}, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		for id, inst := range s.instances {
			if inst.State == "completed" || inst.State == "failed" {
				delete(s.instances, id)
			}
		}
		return struct{}{}, nil
	})
	return err
}

// Errors
var (
	ErrWorkflowNotFound = errors.New("workflow not found")
	ErrInstanceNotFound = errors.New("instance not found")
)
