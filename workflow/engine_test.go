package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/songzhibin97/workflow-engine/rules"

	"github.com/songzhibin97/workflow-engine/types"
)

// MockGenerator is a simple ID generator for testing.
type MockGenerator struct {
	id uint64
}

func (g *MockGenerator) NextID() (uint64, error) {
	g.id++
	return g.id, nil
}

// Modified MockAction to include an execution hook
type MockAction struct {
	name        string
	shouldErr   func(int) bool // Change to a function that takes attempt count
	executeHook func()
	attempts    int // Track attempts internally
}

func (a *MockAction) Execute(ctx context.Context, context map[string]interface{}) (interface{}, error) {
	a.attempts++
	if a.executeHook != nil {
		a.executeHook()
	}
	if a.shouldErr != nil && a.shouldErr(a.attempts) {
		return nil, errors.New("mock action error")
	}
	return a.name + "_result", nil
}

func (a *MockAction) ExecuteAsync(ctx context.Context, context map[string]interface{}) (chan interface{}, chan error) {
	resultChan := make(chan interface{}, 1)
	errChan := make(chan error, 1)
	go func() {
		a.attempts++
		if a.executeHook != nil {
			a.executeHook()
		}
		if a.shouldErr != nil && a.shouldErr(a.attempts) {
			errChan <- errors.New("mock action error")
		} else {
			resultChan <- a.name + "_result"
		}
	}()
	return resultChan, errChan
}

// MockStorage is a simple in-memory storage for testing.
type MockStorage struct {
	workflows map[uint64]types.Workflow
	instances map[uint64]types.WorkflowInstance
}

func NewMockStorage() *MockStorage {
	return &MockStorage{
		workflows: make(map[uint64]types.Workflow),
		instances: make(map[uint64]types.WorkflowInstance),
	}
}

func (s *MockStorage) SaveWorkflow(ctx context.Context, wf types.Workflow) error {
	s.workflows[wf.ID] = wf
	return nil
}

func (s *MockStorage) GetWorkflow(ctx context.Context, id uint64) (types.Workflow, error) {
	wf, ok := s.workflows[id]
	if !ok {
		return types.Workflow{}, errors.New("workflow not found")
	}
	return wf, nil
}

func (s *MockStorage) SaveInstance(ctx context.Context, inst types.WorkflowInstance) error {
	s.instances[inst.ID] = inst
	return nil
}

func (s *MockStorage) GetInstance(ctx context.Context, id uint64) (types.WorkflowInstance, error) {
	inst, ok := s.instances[id]
	if !ok {
		return types.WorkflowInstance{}, errors.New("instance not found")
	}
	return inst, nil
}

// TestNewWorkflowEngine tests the creation of a new WorkflowEngine.
func TestNewWorkflowEngine(t *testing.T) {
	gen := &MockGenerator{}
	store := NewMockStorage()

	engine, err := NewWorkflowEngine(gen, store, rules.NewExprEvaluator())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if engine == nil {
		t.Fatal("expected non-nil WorkflowEngine")
	}

	// Test with nil generator
	_, err = NewWorkflowEngine(nil, store, rules.NewExprEvaluator())
	if err == nil || err.Error() != "generator is required" {
		t.Errorf("expected error 'generator is required', got %v", err)
	}
}

// TestRegisterWorkflow tests registering a workflow.
func TestRegisterWorkflow(t *testing.T) {
	gen := &MockGenerator{}
	store := NewMockStorage()
	engine, _ := NewWorkflowEngine(gen, store, rules.NewExprEvaluator())

	wf := types.Workflow{
		ID:   1,
		Name: "test_workflow",
		Nodes: []types.Node{
			{ID: 1, Type: NodeTypeStart},
			{ID: 2, Type: NodeTypeEnd},
		},
		Transitions: []types.Transition{
			{FromNodeID: 1, ToNodeID: 2, Condition: "true"},
		},
	}

	ctx := context.Background()
	err := engine.RegisterWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	storedWf, err := engine.GetWorkflow(ctx, 1)
	if err != nil || storedWf.ID != 1 {
		t.Errorf("expected workflow ID 1, got %v, error: %v", storedWf.ID, err)
	}
}

// TestStartWorkflow tests starting a workflow instance.
func TestStartWorkflow(t *testing.T) {
	gen := &MockGenerator{}
	store := NewMockStorage()
	engine, _ := NewWorkflowEngine(gen, store, rules.NewExprEvaluator())

	wf := types.Workflow{
		ID:   1,
		Name: "test_workflow",
		Nodes: []types.Node{
			{ID: 1, Type: NodeTypeStart},
			{ID: 2, Type: NodeTypeTask, Action: "test_action"},
			{ID: 3, Type: NodeTypeEnd},
		},
		Transitions: []types.Transition{
			{FromNodeID: 1, ToNodeID: 2, Condition: "true"},
			{FromNodeID: 2, ToNodeID: 3, Condition: "true"},
		},
	}

	ctx := context.Background()
	err := engine.RegisterWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("failed to register workflow: %v", err)
	}

	engine.RegisterAction(ctx, "test_action", &MockAction{name: "test", shouldErr: func(i int) bool {
		return false
	}})

	inst, err := engine.StartWorkflow(ctx, 1, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if inst.State != StateCompleted {
		t.Errorf("expected state %s, got %s", StateCompleted, inst.State)
	}
	if inst.CurrentNodeID != 3 {
		t.Errorf("expected current node ID 3, got %d", inst.CurrentNodeID)
	}
}

// TestNextWithManualNode tests the Next function with a manual node.
func TestNextWithManualNode(t *testing.T) {
	gen := &MockGenerator{}
	store := NewMockStorage()
	engine, _ := NewWorkflowEngine(gen, store, rules.NewExprEvaluator())

	wf := types.Workflow{
		ID:   1,
		Name: "manual_workflow",
		Nodes: []types.Node{
			{ID: 1, Type: NodeTypeStart},
			{ID: 2, Type: NodeTypeManual},
			{ID: 3, Type: NodeTypeEnd},
		},
		Transitions: []types.Transition{
			{FromNodeID: 1, ToNodeID: 2, Condition: "true"},
			{FromNodeID: 2, ToNodeID: 3, Condition: "approved"},
		},
	}

	ctx := context.Background()
	err := engine.RegisterWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("failed to register workflow: %v", err)
	}

	inst, err := engine.StartWorkflow(ctx, 1, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if inst.State != StatePendingApproval {
		t.Errorf("expected state %s, got %s", StatePendingApproval, inst.State)
	}
	if inst.CurrentNodeID != 2 {
		t.Errorf("expected current node ID 2, got %d", inst.CurrentNodeID)
	}

	// Confirm transition
	err = engine.ConfirmTransition(ctx, inst.ID, true)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	updatedInst, _ := engine.GetInstance(ctx, inst.ID)
	if updatedInst.State != StateCompleted {
		t.Errorf("expected state %s, got %s", StateCompleted, updatedInst.State)
	}
	if updatedInst.CurrentNodeID != 3 {
		t.Errorf("expected current node ID 3, got %d", updatedInst.CurrentNodeID)
	}
}

// TestHandleGateway tests the gateway node handling with AND logic.
func TestHandleGateway(t *testing.T) {
	gen := &MockGenerator{}
	store := NewMockStorage()
	engine, _ := NewWorkflowEngine(gen, store, rules.NewExprEvaluator())

	wf := types.Workflow{
		ID:   1,
		Name: "gateway_workflow",
		Nodes: []types.Node{
			{ID: 1, Type: NodeTypeStart},
			{ID: 2, Type: NodeTypeTask, Action: "task1"},
			{ID: 3, Type: NodeTypeTask, Action: "task2"},
			{ID: 4, Type: NodeTypeGateway, Logic: LogicAnd},
			{ID: 5, Type: NodeTypeEnd},
		},
		Transitions: []types.Transition{
			{FromNodeID: 1, ToNodeID: 2, Condition: "true"},
			{FromNodeID: 1, ToNodeID: 3, Condition: "true"},
			{FromNodeID: 2, ToNodeID: 4, Condition: "true"},
			{FromNodeID: 3, ToNodeID: 4, Condition: "true"},
			{FromNodeID: 4, ToNodeID: 5, Condition: "true"},
		},
	}

	ctx := context.Background()
	err := engine.RegisterWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("failed to register workflow: %v", err)
	}

	engine.RegisterAction(ctx, "task1", &MockAction{name: "task1", shouldErr: func(i int) bool {
		return false
	}})
	engine.RegisterAction(ctx, "task2", &MockAction{name: "task2", shouldErr: func(i int) bool {
		return false
	}})

	inst, err := engine.StartWorkflow(ctx, 1, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Wait briefly for async tasks to complete (ideally use event subscription in production)
	time.Sleep(100 * time.Millisecond)
	updatedInst, err := engine.GetInstance(ctx, inst.ID)
	if err != nil {
		t.Fatalf("failed to get instance: %v", err)
	}
	if updatedInst.State != StateCompleted {
		t.Errorf("expected state %s, got %s", StateCompleted, updatedInst.State)
	}
	if updatedInst.CurrentNodeID != 5 {
		t.Errorf("expected current node ID 5, got %d", updatedInst.CurrentNodeID)
	}
}

// TestErrorHandling tests error handling with a failing task.
func TestErrorHandling(t *testing.T) {
	gen := &MockGenerator{}
	store := NewMockStorage()
	engine, _ := NewWorkflowEngine(gen, store, rules.NewExprEvaluator())

	wf := types.Workflow{
		ID:   1,
		Name: "error_workflow",
		Nodes: []types.Node{
			{ID: 1, Type: NodeTypeStart},
			{ID: 2, Type: NodeTypeTask, Action: "fail_task"},
			{ID: 3, Type: NodeTypeError},
			{ID: 4, Type: NodeTypeEnd},
		},
		Transitions: []types.Transition{
			{FromNodeID: 1, ToNodeID: 2, Condition: "true"},
			{FromNodeID: 2, ToNodeID: 3, Condition: "true"}, // Error transition
			{FromNodeID: 2, ToNodeID: 4, Condition: "false"},
		},
	}

	ctx := context.Background()
	err := engine.RegisterWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("failed to register workflow: %v", err)
	}

	engine.RegisterAction(ctx, "fail_task", &MockAction{name: "fail", shouldErr: func(i int) bool {
		return true
	}})

	inst, err := engine.StartWorkflow(ctx, 1, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	updatedInst, _ := engine.GetInstance(ctx, inst.ID)
	if updatedInst.State != StateFailed {
		t.Errorf("expected state %s, got %s", StateFailed, updatedInst.State)
	}
	if updatedInst.CurrentNodeID != 3 {
		t.Errorf("expected current node ID 3, got %d", updatedInst.CurrentNodeID)
	}
	if updatedInst.Context["error"] == nil {
		t.Error("expected error in context, got nil")
	}
}

// TestRecursionLimit tests the recursion depth limit.
func TestRecursionLimit(t *testing.T) {
	gen := &MockGenerator{}
	store := NewMockStorage()
	engine, _ := NewWorkflowEngine(gen, store, rules.NewExprEvaluator())

	// Create a workflow with a potential loop
	wf := types.Workflow{
		ID:   1,
		Name: "loop_workflow",
		Nodes: []types.Node{
			{ID: 1, Type: NodeTypeStart},
			{ID: 2, Type: NodeTypeTask, Action: "loop_task"},
		},
		Transitions: []types.Transition{
			{FromNodeID: 1, ToNodeID: 2, Condition: "true"},
			{FromNodeID: 2, ToNodeID: 2, Condition: "true"}, // Loop back
		},
	}

	ctx := context.Background()
	err := engine.RegisterWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("failed to register workflow: %v", err)
	}

	engine.RegisterAction(ctx, "loop_task", &MockAction{name: "loop", shouldErr: func(i int) bool {
		return false
	}})

	_, err = engine.StartWorkflow(ctx, 1, nil)
	if err == nil || err.Error() != "maximum recursion depth 100 exceeded" {
		t.Errorf("expected recursion depth error, got %v", err)
	}
}

func TestHandleGatewaySingleUpstream(t *testing.T) {
	gen := &MockGenerator{}
	store := NewMockStorage()
	engine, _ := NewWorkflowEngine(gen, store, rules.NewExprEvaluator())

	wf := types.Workflow{
		ID:   1,
		Name: "single_upstream_gateway_workflow",
		Nodes: []types.Node{
			{ID: 1, Type: NodeTypeStart},
			{ID: 2, Type: NodeTypeTask, Action: "task1", MaxRetries: 0},
			{ID: 3, Type: NodeTypeGateway},
			{ID: 4, Type: NodeTypeEnd},
		},
		Transitions: []types.Transition{
			{FromNodeID: 1, ToNodeID: 2, Condition: "true"},
			{FromNodeID: 2, ToNodeID: 3, Condition: "result != nil"},
			{FromNodeID: 3, ToNodeID: 4, Condition: "result == 'task1_result'"},
		},
	}

	ctx := context.Background()
	err := engine.RegisterWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("failed to register workflow: %v", err)
	}

	engine.RegisterAction(ctx, "task1", &MockAction{name: "task1", shouldErr: func(int) bool { return false }})

	inst, err := engine.StartWorkflow(ctx, 1, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	time.Sleep(100 * time.Millisecond) // Wait for async completion
	updatedInst, err := engine.GetInstance(ctx, inst.ID)
	if err != nil {
		t.Fatalf("failed to get instance: %v", err)
	}
	if updatedInst.State != StateCompleted {
		t.Errorf("expected state %s, got %s", StateCompleted, updatedInst.State)
	}
	if updatedInst.CurrentNodeID != 4 {
		t.Errorf("expected current node ID 4, got %d", updatedInst.CurrentNodeID)
	}
	if updatedInst.Context["result"] != "task1_result" {
		t.Errorf("expected result 'task1_result', got %v", updatedInst.Context["result"])
	}
}

func TestTaskRetry(t *testing.T) {
	gen := &MockGenerator{}
	store := NewMockStorage()
	engine, _ := NewWorkflowEngine(gen, store, rules.NewExprEvaluator())

	wf := types.Workflow{
		ID:   1,
		Name: "retry_workflow",
		Nodes: []types.Node{
			{ID: 1, Type: NodeTypeStart},
			{ID: 2, Type: NodeTypeTask, Action: "retry_task", MaxRetries: 2, RetryDelaySec: 1},
			{ID: 3, Type: NodeTypeEnd},
			{ID: 4, Type: NodeTypeError},
		},
		Transitions: []types.Transition{
			{FromNodeID: 1, ToNodeID: 2, Condition: "true"},
			{FromNodeID: 2, ToNodeID: 3, Condition: "result != nil"},
			{FromNodeID: 2, ToNodeID: 4, Condition: "result == nil"},
		},
	}

	ctx := context.Background()
	err := engine.RegisterWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("failed to register workflow: %v", err)
	}

	// Mock action that fails twice then succeeds
	action := &MockAction{
		name: "retry",
		shouldErr: func(attempt int) bool {
			return attempt <= 2 // Fail on first 2 attempts, succeed on 3rd
		},
	}
	engine.RegisterAction(ctx, "retry_task", action)

	inst, err := engine.StartWorkflow(ctx, 1, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	time.Sleep(3 * time.Second) // Wait for retries
	updatedInst, err := engine.GetInstance(ctx, inst.ID)
	if err != nil {
		t.Fatalf("failed to get instance: %v", err)
	}
	if updatedInst.State != StateCompleted {
		t.Errorf("expected state %s, got %s", StateCompleted, updatedInst.State)
	}
	if updatedInst.CurrentNodeID != 3 {
		t.Errorf("expected current node ID 3, got %d", updatedInst.CurrentNodeID)
	}
	if action.attempts != 3 {
		t.Errorf("expected 3 attempts (2 retries), got %d", action.attempts)
	}
}

func TestHandleGatewayNoDuplicateExecution(t *testing.T) {
	gen := &MockGenerator{}
	store := NewMockStorage()
	engine, _ := NewWorkflowEngine(gen, store, rules.NewExprEvaluator())

	wf := types.Workflow{
		ID:   1,
		Name: "parallel_no_duplicate_workflow",
		Nodes: []types.Node{
			{ID: 1, Type: NodeTypeStart},
			{ID: 2, Type: NodeTypeTask, Action: "task1", MaxRetries: 0},
			{ID: 3, Type: NodeTypeTask, Action: "task2", MaxRetries: 0},
			{ID: 4, Type: NodeTypeGateway, Logic: LogicAnd},
			{ID: 5, Type: NodeTypeEnd},
		},
		Transitions: []types.Transition{
			{FromNodeID: 1, ToNodeID: 2, Condition: "true"},
			{FromNodeID: 1, ToNodeID: 3, Condition: "true"},
			{FromNodeID: 2, ToNodeID: 4, Condition: "result != nil"},
			{FromNodeID: 3, ToNodeID: 4, Condition: "result != nil"},
			{FromNodeID: 4, ToNodeID: 5, Condition: "true"},
		},
	}

	ctx := context.Background()
	err := engine.RegisterWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("failed to register workflow: %v", err)
	}

	var task1Attempts, task2Attempts int
	engine.RegisterAction(ctx, "task1", &MockAction{
		name:      "task1",
		shouldErr: func(int) bool { return false },
		executeHook: func() {
			task1Attempts++
			t.Logf("Task1 executed, attempts: %d", task1Attempts)
		},
	})
	engine.RegisterAction(ctx, "task2", &MockAction{
		name:      "task2",
		shouldErr: func(int) bool { return false },
		executeHook: func() {
			task2Attempts++
			t.Logf("Task2 executed, attempts: %d", task2Attempts)

		},
	})

	inst, err := engine.StartWorkflow(ctx, 1, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	time.Sleep(500 * time.Millisecond) // Wait for async completion
	updatedInst, err := engine.GetInstance(ctx, inst.ID)
	if err != nil {
		t.Fatalf("failed to get instance: %v", err)
	}
	if updatedInst.State != StateCompleted {
		t.Errorf("expected state %s, got %s", StateCompleted, updatedInst.State)
	}
	if updatedInst.CurrentNodeID != 5 {
		t.Errorf("expected current node ID 5, got %d", updatedInst.CurrentNodeID)
	}
	if task1Attempts != 1 || task2Attempts != 1 {
		t.Errorf("expected 1 execution per task, got task1: %d, task2: %d", task1Attempts, task2Attempts)
	}
}

func TestInvalidWorkflow(t *testing.T) {
	gen := &MockGenerator{}
	store := NewMockStorage()
	engine, _ := NewWorkflowEngine(gen, store, rules.NewExprEvaluator())

	wf := types.Workflow{
		ID:   1,
		Name: "invalid_workflow",
		Nodes: []types.Node{
			{ID: 1, Type: NodeTypeTask, Action: "task1"}, // No start node
			{ID: 2, Type: NodeTypeEnd},
		},
		Transitions: []types.Transition{
			{FromNodeID: 1, ToNodeID: 2, Condition: "true"},
		},
	}

	ctx := context.Background()
	err := engine.RegisterWorkflow(ctx, wf)
	if err == nil {
		t.Errorf("expected error 'workflow must have a start node', got nil")
	} else if !strings.Contains(err.Error(), "workflow must have a start node") {
		t.Errorf("expected error 'workflow must have a start node', got %v", err)
	}
}

func TestCustomErrorHandler(t *testing.T) {
	gen := &MockGenerator{}
	store := NewMockStorage()
	engine, _ := NewWorkflowEngine(gen, store, rules.NewExprEvaluator())

	wf := types.Workflow{
		ID:   1,
		Name: "custom_error_workflow",
		Nodes: []types.Node{
			{ID: 1, Type: NodeTypeStart},
			{ID: 2, Type: NodeTypeTask, Action: "fail_task"},
			{ID: 3, Type: NodeTypeEnd},
		},
		Transitions: []types.Transition{
			{FromNodeID: 1, ToNodeID: 2, Condition: "true"},
			{FromNodeID: 2, ToNodeID: 3, Condition: "true"},
		},
	}

	ctx := context.Background()
	err := engine.RegisterWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("failed to register workflow: %v", err)
	}

	engine.RegisterAction(ctx, "fail_task", &MockAction{name: "fail", shouldErr: func(i int) bool {
		return true
	}})

	// Set custom error handler to retry once
	engine.SetErrorHandler(func(ctx context.Context, inst *types.WorkflowInstance, err error) error {
		inst.Context["retry"] = true
		inst.CurrentNodeID = 2 // Retry the task
		return nil
	})

	inst, err := engine.StartWorkflow(ctx, 1, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	updatedInst, err := engine.GetInstance(ctx, inst.ID)
	if err != nil {
		t.Fatalf("failed to get instance: %v", err)
	}
	if updatedInst.State != StateRunning {
		t.Errorf("expected state %s, got %s", StateRunning, updatedInst.State)
	}
	if updatedInst.CurrentNodeID != 2 {
		t.Errorf("expected current node ID 2, got %d", updatedInst.CurrentNodeID)
	}
	if updatedInst.Context["retry"] != true {
		t.Errorf("expected retry flag in context, got %v", updatedInst.Context["retry"])
	}
}

func TestComplexWorkflowFullPath(t *testing.T) {
	gen := &MockGenerator{}
	store := NewMockStorage()
	engine, _ := NewWorkflowEngine(gen, store, rules.NewExprEvaluator())

	wf := types.Workflow{
		ID:   1,
		Name: "complex_workflow",
		Nodes: []types.Node{
			{ID: 1, Type: NodeTypeStart},
			{ID: 2, Type: NodeTypeTask, Action: "task1", MaxRetries: 0},
			{ID: 3, Type: NodeTypeManual},
			{ID: 4, Type: NodeTypeGateway, Logic: LogicAnd},
			{ID: 5, Type: NodeTypeTask, Action: "task2", MaxRetries: 0},
			{ID: 6, Type: NodeTypeEnd},
		},
		Transitions: []types.Transition{
			{FromNodeID: 1, ToNodeID: 2, Condition: "true"},
			{FromNodeID: 2, ToNodeID: 3, Condition: "result != nil"},
			{FromNodeID: 3, ToNodeID: 4, Condition: "approved"},
			{FromNodeID: 4, ToNodeID: 5, Condition: "true"},
			{FromNodeID: 5, ToNodeID: 6, Condition: "result != nil"},
		},
	}

	ctx := context.Background()
	err := engine.RegisterWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("failed to register workflow: %v", err)
	}

	engine.RegisterAction(ctx, "task1", &MockAction{name: "task1", shouldErr: func(int) bool { return false }})
	engine.RegisterAction(ctx, "task2", &MockAction{name: "task2", shouldErr: func(int) bool { return false }})

	inst, err := engine.StartWorkflow(ctx, 1, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Wait for manual node
	time.Sleep(100 * time.Millisecond)
	pendingInst, err := engine.GetInstance(ctx, inst.ID)
	if err != nil {
		t.Fatalf("failed to get instance: %v", err)
	}
	if pendingInst.State != StatePendingApproval || pendingInst.CurrentNodeID != 3 {
		t.Errorf("expected state %s at node 3, got %s at node %d", StatePendingApproval, pendingInst.State, pendingInst.CurrentNodeID)
	}

	// Simulate approval
	err = engine.ConfirmTransition(ctx, inst.ID, true)
	if err != nil {
		t.Fatalf("failed to confirm transition: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	updatedInst, err := engine.GetInstance(ctx, inst.ID)
	if err != nil {
		t.Fatalf("failed to get instance: %v", err)
	}
	if updatedInst.State != StateCompleted {
		t.Errorf("expected state %s, got %s", StateCompleted, updatedInst.State)
	}
	if updatedInst.CurrentNodeID != 6 {
		t.Errorf("expected current node ID 6, got %d", updatedInst.CurrentNodeID)
	}
	if updatedInst.Context["result"] != "task2_result" {
		t.Errorf("expected result 'task2_result', got %v", updatedInst.Context["result"])
	}
}

func TestParallelWorkflowWithConditions(t *testing.T) {
	gen := &MockGenerator{}
	store := NewMockStorage()
	r := rules.NewExprEvaluator()
	r.AddOptionFunc("find", func(m map[string]interface{}) interface{} {
		return func(k int) interface{} {
			if v, ok := m["upstream_results"].(map[uint64]interface{})[uint64(k)]; ok {
				return v
			}
			return nil
		}
	})
	engine, _ := NewWorkflowEngine(gen, store, r)

	wf := types.Workflow{
		ID:   1,
		Name: "parallel_conditions_workflow",
		Nodes: []types.Node{
			{ID: 1, Type: NodeTypeStart},
			{ID: 2, Type: NodeTypeTask, Action: "task1", MaxRetries: 0},
			{ID: 3, Type: NodeTypeTask, Action: "task2", MaxRetries: 0},
			{ID: 4, Type: NodeTypeGateway, Logic: LogicAnd},
			{ID: 5, Type: NodeTypeEnd},
		},
		Transitions: []types.Transition{
			{FromNodeID: 1, ToNodeID: 2, Condition: "true"},
			{FromNodeID: 1, ToNodeID: 3, Condition: "true"},
			{FromNodeID: 2, ToNodeID: 4, Condition: "result == 'task1_result'"},
			{FromNodeID: 3, ToNodeID: 4, Condition: "result == 'task2_result'"},
			{FromNodeID: 4, ToNodeID: 5, Condition: "find(2) == 'task1_result' && find(3) == 'task2_result'"},
		},
	}

	ctx := context.Background()
	err := engine.RegisterWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("failed to register workflow: %v", err)
	}

	engine.RegisterAction(ctx, "task1", &MockAction{
		name:      "task1",
		shouldErr: func(int) bool { return false },
		executeHook: func() {
			// Assume context is passed via Execute method
		},
	})
	engine.RegisterAction(ctx, "task2", &MockAction{
		name:      "task2",
		shouldErr: func(int) bool { return false },
		executeHook: func() {
			// Assume context is passed via Execute method
		},
	})

	inst, err := engine.StartWorkflow(ctx, 1, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	updatedInst, err := engine.GetInstance(ctx, inst.ID)
	if err != nil {
		t.Fatalf("failed to get instance: %v", err)
	}
	if updatedInst.State != StateCompleted {
		t.Errorf("expected state %s, got %s", StateCompleted, updatedInst.State)
	}
	if updatedInst.CurrentNodeID != 5 {
		t.Errorf("expected current node ID 5, got %d", updatedInst.CurrentNodeID)
	}
	t.Logf("upstream_results: %v", updatedInst.Context["upstream_results"])
}

func TestGatewayWithORLogic(t *testing.T) {
	gen := &MockGenerator{}
	store := NewMockStorage()
	engine, _ := NewWorkflowEngine(gen, store, rules.NewExprEvaluator())

	wf := types.Workflow{
		ID:   1,
		Name: "or_logic_gateway_workflow",
		Nodes: []types.Node{
			{ID: 1, Type: NodeTypeStart},
			{ID: 2, Type: NodeTypeTask, Action: "task1", MaxRetries: 0},
			{ID: 3, Type: NodeTypeTask, Action: "task2", MaxRetries: 0},
			{ID: 4, Type: NodeTypeGateway, Logic: LogicOr},
			{ID: 5, Type: NodeTypeEnd},
		},
		Transitions: []types.Transition{
			{FromNodeID: 1, ToNodeID: 2, Condition: "true"},
			{FromNodeID: 1, ToNodeID: 3, Condition: "true"},
			{FromNodeID: 2, ToNodeID: 4, Condition: "result != nil"},
			{FromNodeID: 3, ToNodeID: 4, Condition: "result != nil"},
			{FromNodeID: 4, ToNodeID: 5, Condition: "true"},
		},
	}

	ctx := context.Background()
	err := engine.RegisterWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("failed to register workflow: %v", err)
	}

	engine.RegisterAction(ctx, "task1", &MockAction{name: "task1", shouldErr: func(int) bool { return false }})
	engine.RegisterAction(ctx, "task2", &MockAction{name: "task2", shouldErr: func(int) bool { return true }}) // task2 fails

	inst, err := engine.StartWorkflow(ctx, 1, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	updatedInst, err := engine.GetInstance(ctx, inst.ID)
	if err != nil {
		t.Fatalf("failed to get instance: %v", err)
	}
	if updatedInst.State != StateCompleted {
		t.Errorf("expected state %s, got %s", StateCompleted, updatedInst.State)
	}
	if updatedInst.CurrentNodeID != 5 {
		t.Errorf("expected current node ID 5, got %d", updatedInst.CurrentNodeID)
	}
}

func TestTaskRetryWithGateway(t *testing.T) {
	gen := &MockGenerator{}
	store := NewMockStorage()
	engine, _ := NewWorkflowEngine(gen, store, rules.NewExprEvaluator())

	wf := types.Workflow{
		ID:   1,
		Name: "retry_with_gateway_workflow",
		Nodes: []types.Node{
			{ID: 1, Type: NodeTypeStart},
			{ID: 2, Type: NodeTypeTask, Action: "task1", MaxRetries: 2, RetryDelaySec: 1},
			{ID: 3, Type: NodeTypeGateway, Logic: LogicAnd},
			{ID: 4, Type: NodeTypeEnd},
		},
		Transitions: []types.Transition{
			{FromNodeID: 1, ToNodeID: 2, Condition: "true"},
			{FromNodeID: 2, ToNodeID: 3, Condition: "result != nil"},
			{FromNodeID: 3, ToNodeID: 4, Condition: "result == 'task1_result'"},
		},
	}

	ctx := context.Background()
	err := engine.RegisterWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("failed to register workflow: %v", err)
	}

	action := &MockAction{
		name: "task1",
		shouldErr: func(attempt int) bool {
			return attempt <= 2 // Fail on first 2 attempts, succeed on 3rd
		},
	}
	engine.RegisterAction(ctx, "task1", action)

	inst, err := engine.StartWorkflow(ctx, 1, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	time.Sleep(3 * time.Second) // Wait for retries
	updatedInst, err := engine.GetInstance(ctx, inst.ID)
	if err != nil {
		t.Fatalf("failed to get instance: %v", err)
	}
	if updatedInst.State != StateCompleted {
		t.Errorf("expected state %s, got %s", StateCompleted, updatedInst.State)
	}
	if updatedInst.CurrentNodeID != 4 {
		t.Errorf("expected current node ID 4, got %d", updatedInst.CurrentNodeID)
	}
	if action.attempts != 3 {
		t.Errorf("expected 3 attempts (2 retries), got %d", action.attempts)
	}
}

func TestDuplicateNodeIDs(t *testing.T) {
	gen := &MockGenerator{}
	store := NewMockStorage()
	engine, _ := NewWorkflowEngine(gen, store, rules.NewExprEvaluator())

	wf := types.Workflow{
		ID:   1,
		Name: "duplicate_nodes_workflow",
		Nodes: []types.Node{
			{ID: 1, Type: NodeTypeStart},
			{ID: 1, Type: NodeTypeTask, Action: "task1"}, // Duplicate ID
			{ID: 2, Type: NodeTypeEnd},
		},
		Transitions: []types.Transition{
			{FromNodeID: 1, ToNodeID: 1, Condition: "true"},
			{FromNodeID: 1, ToNodeID: 2, Condition: "result != nil"},
		},
	}

	ctx := context.Background()
	err := engine.RegisterWorkflow(ctx, wf)
	if err == nil {
		t.Errorf("expected error 'duplicate node ID', got nil")
	} else if !strings.Contains(err.Error(), "duplicate node ID") {
		t.Errorf("expected error 'duplicate node ID', got %v", err)
	}
}

func TestMain(m *testing.M) {
	// Setup if needed
	m.Run()
}
