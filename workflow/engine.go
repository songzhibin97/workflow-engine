package workflow

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/songzhibin97/gkit/generator"
	"github.com/songzhibin97/workflow-engine/events"
	"github.com/songzhibin97/workflow-engine/rules"
	"github.com/songzhibin97/workflow-engine/storage"
	"github.com/songzhibin97/workflow-engine/types"
)

// Standard error definitions
var (
	ErrWorkflowNotFound    = errors.New("workflow not found")
	ErrInstanceNotFound    = errors.New("instance not found")
	ErrNodeNotFound        = errors.New("node not found")
	ErrNoNodes             = errors.New("workflow has no nodes")
	ErrActionNotRegistered = errors.New("action not registered")
	ErrNotPendingApproval  = errors.New("instance is not in pending_approval state")
	ErrNotManualNode       = errors.New("current node is not a manual node")
	ErrNoUpstreamNodes     = errors.New("no upstream nodes found for gateway")
)

// State and node type constants
const (
	// Instance states
	StateRunning         = "running"
	StateWaiting         = "waiting"
	StatePendingApproval = "pending_approval"
	StateCompleted       = "completed"
	StateFailed          = "failed"

	// Node types
	NodeTypeStart   = "start"
	NodeTypeTask    = "task"
	NodeTypeManual  = "manual"
	NodeTypeGateway = "gateway"
	NodeTypeEnd     = "end"
	NodeTypeError   = "error"

	// Gateway logic
	LogicAnd = "and"
	LogicOr  = "or"

	// Event types
	EventStateChanged    = "state_changed"
	EventPendingApproval = "pending_approval"
	EventErrorOccurred   = "error_occurred"

	// Maximum recursion depth to prevent stack overflow
	MaxRecursionDepth = 100
)

// WorkflowEngine manages workflows and their instances.
type WorkflowEngine struct {
	workflows          map[uint64]types.Workflow
	instances          map[uint64]types.WorkflowInstance
	actions            map[string]Action
	evaluator          rules.Evaluator
	storage            storage.Storage
	eventBus           *events.EventBus
	mu                 sync.RWMutex
	generate           generator.Generator
	defaultMaxRetries  int
	defaultRetryDelay  time.Duration
	customErrorHandler func(ctx context.Context, inst *types.WorkflowInstance, err error) error
}

// NewWorkflowEngine creates a new WorkflowEngine instance with the given generator and storage.
func NewWorkflowEngine(generate generator.Generator, store storage.Storage, evaluator rules.Evaluator) (*WorkflowEngine, error) {
	if generate == nil {
		return nil, errors.New("generator is required")
	}

	if store == nil {
		store = storage.NewMemoryStorage()
	}

	return &WorkflowEngine{
		workflows:         make(map[uint64]types.Workflow),
		instances:         make(map[uint64]types.WorkflowInstance),
		actions:           make(map[string]Action),
		evaluator:         evaluator,
		storage:           store,
		eventBus:          events.NewEventBus(),
		generate:          generate,
		defaultMaxRetries: 3,
		defaultRetryDelay: time.Second,
	}, nil
}

// SubscribeEvent subscribes an event handler to a specific event type.
func (e *WorkflowEngine) SubscribeEvent(eventType string, handler events.EventHandler) {
	e.eventBus.Subscribe(eventType, handler)
}

// SetEvaluator sets a custom evaluator for condition evaluation.
func (e *WorkflowEngine) SetEvaluator(evaluator rules.Evaluator) {
	if evaluator == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.evaluator = evaluator
}

// SetErrorHandler sets a custom error handler for workflow errors.
func (e *WorkflowEngine) SetErrorHandler(handler func(ctx context.Context, inst *types.WorkflowInstance, err error) error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.customErrorHandler = handler
}

// GenerateID generates a unique ID using the configured generator.
func (e *WorkflowEngine) GenerateID() (uint64, error) {
	return e.generate.NextID()
}

// RegisterAction registers an action for use in workflow tasks.
func (e *WorkflowEngine) RegisterAction(ctx context.Context, name string, action Action) error {
	if name == "" || action == nil {
		return errors.New("name and action are required")
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		e.mu.Lock()
		defer e.mu.Unlock()
		e.actions[name] = action
		return nil
	}
}

// RegisterWorkflow registers and persists a workflow definition.
func (e *WorkflowEngine) RegisterWorkflow(ctx context.Context, wf types.Workflow) error {
	if wf.ID == 0 {
		return errors.New("workflow ID cannot be zero")
	}
	if len(wf.Nodes) == 0 {
		return errors.New("workflow must have at least one node")
	}

	// Check for duplicate node IDs
	nodeIDs := make(map[uint64]bool)
	for _, node := range wf.Nodes {
		if node.ID == 0 {
			return errors.New("node ID cannot be zero")
		}
		if nodeIDs[node.ID] {
			return fmt.Errorf("duplicate node ID %d found in workflow", node.ID)
		}
		nodeIDs[node.ID] = true
	}

	// Check for start node
	hasStart := false
	for _, node := range wf.Nodes {
		if node.Type == NodeTypeStart {
			hasStart = true
			break
		}
	}
	if !hasStart {
		return errors.New("workflow must have a start node")
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	return e.storage.SaveWorkflow(ctx, wf)
}

// getWorkflow retrieves a workflow by ID, checking cache first then storage.
func (e *WorkflowEngine) getWorkflow(ctx context.Context, workflowID uint64) (types.Workflow, error) {
	e.mu.RLock()
	wf, ok := e.workflows[workflowID]
	e.mu.RUnlock()

	if ok {
		return wf, nil
	}

	wf, err := e.storage.GetWorkflow(ctx, workflowID)
	if err != nil {
		return types.Workflow{}, fmt.Errorf("failed to get workflow: %w", err)
	}

	e.mu.Lock()
	e.workflows[wf.ID] = wf
	e.mu.Unlock()

	return wf, nil
}

// getInstance retrieves an instance by ID, checking cache first then storage.
func (e *WorkflowEngine) getInstance(ctx context.Context, instanceID uint64) (types.WorkflowInstance, error) {
	e.mu.RLock()
	inst, ok := e.instances[instanceID]
	e.mu.RUnlock()

	if ok {
		return inst, nil
	}

	inst, err := e.storage.GetInstance(ctx, instanceID)
	if err != nil {
		return types.WorkflowInstance{}, fmt.Errorf("failed to get instance: %w", err)
	}

	e.mu.Lock()
	e.instances[inst.ID] = inst
	e.mu.Unlock()

	return inst, nil
}

// saveInstance saves an instance to both cache and storage.
func (e *WorkflowEngine) saveInstance(ctx context.Context, inst types.WorkflowInstance) error {
	if err := e.storage.SaveInstance(ctx, inst); err != nil {
		return fmt.Errorf("failed to save instance: %w", err)
	}

	e.mu.Lock()
	e.instances[inst.ID] = inst
	e.mu.Unlock()

	return nil
}

// publishEvent publishes an event asynchronously to the event bus.
func (e *WorkflowEngine) publishEvent(ctx context.Context, eventType string, instanceID uint64, data map[string]interface{}) {
	go e.eventBus.Publish(ctx, events.Event{
		Type:       eventType,
		InstanceID: instanceID,
		Data:       data,
	})
}

// StartWorkflow starts a new workflow instance with the given initial context.
func (e *WorkflowEngine) StartWorkflow(ctx context.Context, workflowID uint64, initialContext map[string]interface{}) (*types.WorkflowInstance, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		wf, err := e.getWorkflow(ctx, workflowID)
		if err != nil {
			return nil, ErrWorkflowNotFound
		}

		if len(wf.Nodes) == 0 || wf.Nodes[0].Type != NodeTypeStart {
			return nil, fmt.Errorf("workflow must start with a 'start' node")
		}

		id, err := e.GenerateID()
		if err != nil {
			return nil, fmt.Errorf("failed to generate ID: %w", err)
		}

		if initialContext == nil {
			initialContext = make(map[string]interface{})
		}
		initialContext["depth"] = 0 // Initialize recursion depth

		now := time.Now().UnixMilli()
		inst := types.WorkflowInstance{
			ID:            id,
			WorkflowID:    workflowID,
			State:         StateRunning,
			CurrentNodeID: wf.Nodes[0].ID,
			Context:       initialContext,
			CreatedAt:     now,
			UpdatedAt:     now,
		}

		if err := e.saveInstance(ctx, inst); err != nil {
			return nil, err
		}

		e.publishEvent(ctx, EventStateChanged, inst.ID, map[string]interface{}{
			"state":        inst.State,
			"current_node": inst.CurrentNodeID,
		})

		return &inst, e.Next(ctx, &inst)
	}
}

// Next advances the workflow instance to the next node based on conditions.
func (e *WorkflowEngine) Next(ctx context.Context, instance *types.WorkflowInstance) error {
	if instance == nil {
		return errors.New("instance cannot be nil")
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		depth, ok := instance.Context["depth"].(int)
		if !ok || depth > MaxRecursionDepth {
			return fmt.Errorf("maximum recursion depth %d exceeded", MaxRecursionDepth)
		}
		instance.Context["depth"] = depth + 1

		wf, err := e.getWorkflow(ctx, instance.WorkflowID)
		if err != nil {
			return ErrWorkflowNotFound
		}

		node := e.findNode(wf, instance.CurrentNodeID)
		if node.ID == 0 {
			return ErrNodeNotFound
		}

		snapshot := *instance
		defer func() {
			if r := recover(); r != nil {
				*instance = snapshot
				e.handleError(ctx, instance, fmt.Errorf("panic occurred: %v", r))
			}
		}()

		switch node.Type {
		case NodeTypeStart:
			var nextNodes []uint64
			for _, t := range wf.Transitions {
				if t.FromNodeID == node.ID {
					shouldTransition, err := e.evaluator.Evaluate(t.Condition, instance.Context)
					if err != nil {
						return e.handleError(ctx, instance, fmt.Errorf("failed to evaluate condition '%s': %w", t.Condition, err))
					}
					if shouldTransition || t.Condition == "true" {
						nextNodes = append(nextNodes, t.ToNodeID)
					}
				}
			}
			if len(nextNodes) == 0 {
				return ErrNodeNotFound
			}
			if len(nextNodes) > 1 {
				// Parallel execution
				var wg sync.WaitGroup
				errChan := make(chan error, len(nextNodes))
				results := make(map[uint64]interface{})
				var resultsMu sync.Mutex

				for _, nextNodeID := range nextNodes {
					wg.Add(1)
					go func(nid uint64) {
						defer wg.Done()
						taskNode := e.findNode(wf, nid)
						if taskNode.Type != NodeTypeTask {
							return
						}
						action, ok := e.actions[taskNode.Action]
						if !ok {
							errChan <- fmt.Errorf("%w: %s", ErrActionNotRegistered, taskNode.Action)
							return
						}
						result, err := e.executeTaskWithRetry(ctx, action, instance.Context, taskNode)
						resultsMu.Lock()
						if err != nil {
							results[nid] = err // Store error for OR logic
						} else {
							results[nid] = result
						}
						resultsMu.Unlock()
					}(nextNodeID)
				}

				wg.Wait()
				close(errChan)
				var firstErr error
				for err := range errChan {
					if err != nil && firstErr == nil {
						firstErr = err
					}
				}

				e.mu.Lock()
				instance.Context["upstream_results"] = results
				e.mu.Unlock()

				// Move to the gateway node
				for _, t := range wf.Transitions {
					if t.FromNodeID == nextNodes[0] { // Assume all tasks lead to the same gateway
						instance.CurrentNodeID = t.ToNodeID
						break
					}
				}
				instance.UpdatedAt = time.Now().UnixMilli()
				if err := e.saveInstance(ctx, *instance); err != nil {
					return err
				}
				if firstErr != nil {
					return firstErr
				}
				return e.Next(ctx, instance)
			}
			// Single next node
			instance.CurrentNodeID = nextNodes[0]
			instance.UpdatedAt = time.Now().UnixMilli()
			if err := e.saveInstance(ctx, *instance); err != nil {
				return err
			}
			return e.Next(ctx, instance)

		case NodeTypeTask:
			action, ok := e.actions[node.Action]
			if !ok {
				return fmt.Errorf("%w: %s", ErrActionNotRegistered, node.Action)
			}
			result, err := e.executeTaskWithRetry(ctx, action, instance.Context, node)
			if err != nil {
				return e.handleError(ctx, instance, err)
			}
			e.mu.Lock()
			instance.Context["result"] = result
			upstreamResults, _ := instance.Context["upstream_results"].(map[uint64]interface{})
			if upstreamResults == nil {
				upstreamResults = make(map[uint64]interface{})
			}
			upstreamResults[node.ID] = result
			instance.Context["upstream_results"] = upstreamResults
			e.mu.Unlock()

		case NodeTypeManual:
			if instance.State == StateRunning && instance.Context["approved"] == nil {
				instance.State = StatePendingApproval
				instance.UpdatedAt = time.Now().UnixMilli()
				if err := e.saveInstance(ctx, *instance); err != nil {
					return err
				}
				e.publishEvent(ctx, EventPendingApproval, instance.ID, map[string]interface{}{
					"state":        instance.State,
					"current_node": instance.CurrentNodeID,
				})
				return nil
			}
			if approved, ok := instance.Context["approved"].(bool); ok && approved {
				e.mu.Lock()
				upstreamResults, _ := instance.Context["upstream_results"].(map[uint64]interface{})
				if upstreamResults == nil {
					upstreamResults = make(map[uint64]interface{})
				}
				upstreamResults[node.ID] = "approved"
				instance.Context["upstream_results"] = upstreamResults
				e.mu.Unlock()
			}

		case NodeTypeGateway:
			if err := e.handleGateway(ctx, instance, node, wf); err != nil {
				return e.handleError(ctx, instance, err)
			}
			if instance.State == StateWaiting {
				return nil
			}

		case NodeTypeEnd:
			instance.State = StateCompleted
			instance.UpdatedAt = time.Now().UnixMilli()
			if err := e.saveInstance(ctx, *instance); err != nil {
				return err
			}
			e.publishEvent(ctx, EventStateChanged, instance.ID, map[string]interface{}{
				"state":        instance.State,
				"current_node": instance.CurrentNodeID,
			})
			return nil
		}

		// Process transitions
		hasTransition := false
		for _, t := range wf.Transitions {
			if t.FromNodeID == node.ID {
				shouldTransition, err := e.evaluator.Evaluate(t.Condition, instance.Context)
				if err != nil {
					return e.handleError(ctx, instance, fmt.Errorf("failed to evaluate condition '%s': %w", t.Condition, err))
				}
				if shouldTransition || t.Condition == "true" {
					hasTransition = true
					instance.CurrentNodeID = t.ToNodeID
					instance.UpdatedAt = time.Now().UnixMilli()
					if err := e.saveInstance(ctx, *instance); err != nil {
						return err
					}
					e.publishEvent(ctx, EventStateChanged, instance.ID, map[string]interface{}{
						"state":        instance.State,
						"current_node": instance.CurrentNodeID,
					})
					return e.Next(ctx, instance)
				}
			}
		}

		if !hasTransition && node.Type != NodeTypeGateway {
			instance.State = StateWaiting
			instance.UpdatedAt = time.Now().UnixMilli()
			if err := e.saveInstance(ctx, *instance); err != nil {
				return err
			}
			e.publishEvent(ctx, EventStateChanged, instance.ID, map[string]interface{}{
				"state":        instance.State,
				"current_node": instance.CurrentNodeID,
			})
		}
		return nil
	}
}

// executeTaskWithRetry executes a task with retry logic.
func (e *WorkflowEngine) executeTaskWithRetry(ctx context.Context, action Action, context map[string]interface{}, node types.Node) (interface{}, error) {
	maxRetries := e.defaultMaxRetries
	retryDelay := e.defaultRetryDelay

	if node.MaxRetries > 0 {
		maxRetries = node.MaxRetries
	}
	if node.RetryDelaySec > 0 {
		retryDelay = time.Duration(node.RetryDelaySec) * time.Second
	}

	var lastErr error
	for i := 0; i <= maxRetries; i++ { // Total attempts = 1 initial + maxRetries
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			result, err := action.Execute(ctx, context)
			if err == nil {
				return result, nil
			}
			lastErr = err
			if i < maxRetries { // Only sleep if there are retries left
				time.Sleep(retryDelay) // Simplified backoff for testing
			}
		}
	}
	return nil, fmt.Errorf("task execution failed after %d retries: %w", maxRetries, lastErr)
}

// ConfirmTransition manually confirms a transition for a manual node.
func (e *WorkflowEngine) ConfirmTransition(ctx context.Context, instanceID uint64, approved bool) error {
	inst, err := e.GetInstance(ctx, instanceID)
	if err != nil {
		return err
	}
	if inst.State != StatePendingApproval {
		return fmt.Errorf("instance %d is not pending approval", instanceID)
	}
	e.mu.Lock()
	inst.Context["approved"] = approved
	inst.State = StateRunning
	inst.UpdatedAt = time.Now().UnixMilli()
	e.mu.Unlock()
	if err := e.saveInstance(ctx, *inst); err != nil {
		return err
	}
	e.publishEvent(ctx, EventStateChanged, inst.ID, map[string]interface{}{
		"state":        inst.State,
		"current_node": inst.CurrentNodeID,
		"approved":     approved,
	})
	return e.Next(ctx, inst)
}

// handleGateway handles the execution of gateway nodes (AND/OR logic).
func (e *WorkflowEngine) handleGateway(ctx context.Context, instance *types.WorkflowInstance, node types.Node, wf types.Workflow) error {
	// Check if upstream results are already computed
	upstreamResults, hasUpstreamResults := instance.Context["upstream_results"].(map[uint64]interface{})
	if !hasUpstreamResults || len(upstreamResults) == 0 {
		// For single upstream task or manual node, fall back to result or approved status
		if result, ok := instance.Context["result"]; ok {
			upstreamNodes := make([]types.Node, 0)
			for _, t := range wf.Transitions {
				if t.ToNodeID == node.ID {
					upstreamNode := e.findNode(wf, t.FromNodeID)
					if upstreamNode.ID != 0 && (upstreamNode.Type == NodeTypeTask || upstreamNode.Type == NodeTypeManual) {
						upstreamNodes = append(upstreamNodes, upstreamNode)
					}
				}
			}
			if len(upstreamNodes) == 1 {
				if upstreamNodes[0].Type == NodeTypeTask {
					upstreamResults = map[uint64]interface{}{
						upstreamNodes[0].ID: result,
					}
				} else if upstreamNodes[0].Type == NodeTypeManual {
					if approved, ok := instance.Context["approved"].(bool); ok && approved {
						upstreamResults = map[uint64]interface{}{
							upstreamNodes[0].ID: "approved",
						}
					} else {
						return fmt.Errorf("manual node %d not approved", upstreamNodes[0].ID)
					}
				}
				e.mu.Lock()
				instance.Context["upstream_results"] = upstreamResults
				e.mu.Unlock()
			} else {
				return fmt.Errorf("no upstream results found for gateway node %d", node.ID)
			}
		} else {
			return fmt.Errorf("no upstream results or result found for gateway node %d", node.ID)
		}
	}

	// Verify upstream nodes
	upstreamNodes := make([]types.Node, 0)
	for _, t := range wf.Transitions {
		if t.ToNodeID == node.ID {
			upstreamNode := e.findNode(wf, t.FromNodeID)
			if upstreamNode.ID != 0 && (upstreamNode.Type == NodeTypeTask || upstreamNode.Type == NodeTypeManual) {
				upstreamNodes = append(upstreamNodes, upstreamNode)
			}
		}
	}

	if len(upstreamNodes) == 0 {
		return ErrNoUpstreamNodes
	}

	// Evaluate transitions
	for _, t := range wf.Transitions {
		if t.FromNodeID == node.ID {
			shouldTransition := false
			if node.Logic == LogicAnd || node.Logic == "" { // 默认为 AND
				shouldTransition = true
				for _, n := range upstreamNodes {
					if result, exists := upstreamResults[n.ID]; !exists || result == nil {
						shouldTransition = false
						break
					} else if _, isErr := result.(error); isErr {
						shouldTransition = false
						break
					}
				}
			} else if node.Logic == LogicOr {
				shouldTransition = false
				for _, n := range upstreamNodes {
					if result, exists := upstreamResults[n.ID]; exists && result != nil {
						if _, isErr := result.(error); !isErr {
							shouldTransition = true
							break
						}
					}
				}
			}

			if shouldTransition {
				// 确保在条件评估前已将上游结果添加到上下文中
				tempContext := make(map[string]interface{})
				for k, v := range instance.Context {
					tempContext[k] = v
				}

				shouldTransition, err := e.evaluator.Evaluate(t.Condition, tempContext)
				if err != nil {
					return fmt.Errorf("failed to evaluate condition '%s': %w", t.Condition, err)
				}

				if shouldTransition || t.Condition == "true" {
					instance.CurrentNodeID = t.ToNodeID
					instance.UpdatedAt = time.Now().UnixMilli()
					instance.State = StateRunning // 确保状态为 running，而不是 failed
					if err := e.saveInstance(ctx, *instance); err != nil {
						return err
					}
					e.publishEvent(ctx, EventStateChanged, instance.ID, map[string]interface{}{
						"state":        instance.State,
						"current_node": instance.CurrentNodeID,
					})
					return e.Next(ctx, instance)
				}
			}
		}
	}

	// 如果没有触发任何转换，将状态设置为 waiting
	instance.State = StateWaiting
	instance.UpdatedAt = time.Now().UnixMilli()
	if err := e.saveInstance(ctx, *instance); err != nil {
		return err
	}
	return nil
}

// handleError processes errors with custom handling or default failure state.
func (e *WorkflowEngine) handleError(ctx context.Context, instance *types.WorkflowInstance, err error) error {
	if err == nil {
		return nil
	}

	wf, getErr := e.getWorkflow(ctx, instance.WorkflowID)
	if getErr != nil {
		e.publishEvent(ctx, EventErrorOccurred, instance.ID, map[string]interface{}{
			"state": instance.State,
			"error": fmt.Sprintf("failed to get workflow: %v", getErr),
		})
		return getErr
	}

	for _, t := range wf.Transitions {
		if t.FromNodeID == instance.CurrentNodeID {
			errorNode := e.findNode(wf, t.ToNodeID)
			if errorNode.Type == NodeTypeError {
				instance.CurrentNodeID = t.ToNodeID
				instance.State = StateFailed
				instance.UpdatedAt = time.Now().UnixMilli()
				e.mu.Lock()
				instance.Context["error"] = err.Error()
				e.mu.Unlock()

				if saveErr := e.saveInstance(ctx, *instance); saveErr != nil {
					return fmt.Errorf("original error: %v, failed to save error state: %w", err, saveErr)
				}
				e.publishEvent(ctx, EventErrorOccurred, instance.ID, map[string]interface{}{
					"state": instance.State,
					"error": err.Error(),
				})
				return nil
			}
		}
	}

	if e.customErrorHandler != nil {
		if customErr := e.customErrorHandler(ctx, instance, err); customErr != nil {
			return customErr
		}
		if saveErr := e.saveInstance(ctx, *instance); saveErr != nil {
			return fmt.Errorf("original error: %v, failed to save after custom error handler: %w", err, saveErr)
		}
		e.publishEvent(ctx, EventErrorOccurred, instance.ID, map[string]interface{}{
			"state": instance.State,
			"error": err.Error(),
		})
		return nil
	}

	instance.State = StateFailed
	instance.UpdatedAt = time.Now().UnixMilli()
	e.mu.Lock()
	instance.Context["error"] = err.Error()
	e.mu.Unlock()

	if saveErr := e.saveInstance(ctx, *instance); saveErr != nil {
		return fmt.Errorf("original error: %v, failed to save error state: %w", err, saveErr)
	}
	e.publishEvent(ctx, EventErrorOccurred, instance.ID, map[string]interface{}{
		"state": instance.State,
		"error": err.Error(),
	})
	return nil
}

// findNode finds a node by ID in the workflow.
func (e *WorkflowEngine) findNode(wf types.Workflow, nodeID uint64) types.Node {
	for _, node := range wf.Nodes {
		if node.ID == nodeID {
			return node
		}
	}
	return types.Node{}
}

// GetInstance retrieves a workflow instance by ID.
func (e *WorkflowEngine) GetInstance(ctx context.Context, instanceID uint64) (*types.WorkflowInstance, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		inst, err := e.getInstance(ctx, instanceID)
		if err != nil {
			return nil, err
		}
		return &inst, nil
	}
}

// GetWorkflow retrieves a workflow by ID.
func (e *WorkflowEngine) GetWorkflow(ctx context.Context, workflowID uint64) (*types.Workflow, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		wf, err := e.getWorkflow(ctx, workflowID)
		if err != nil {
			return nil, err
		}
		return &wf, nil
	}
}

// Stop gracefully stops the workflow engine.
func (e *WorkflowEngine) Stop(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		e.eventBus.Stop()
		return nil
	}
}
