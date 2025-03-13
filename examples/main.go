package main

import (
	"context"
	"fmt"
	"time"

	"github.com/songzhibin97/workflow-engine/rules"

	"github.com/songzhibin97/gkit/generator"
	"github.com/songzhibin97/workflow-engine/events"
	"github.com/songzhibin97/workflow-engine/storage"
	"github.com/songzhibin97/workflow-engine/types"
	"github.com/songzhibin97/workflow-engine/workflow"
)

type PrintAction struct {
	Delay     time.Duration
	FailAfter int
	attempts  int
}

func (a *PrintAction) Execute(ctx context.Context, context map[string]interface{}) (interface{}, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(a.Delay):
		a.attempts++
		fmt.Printf("Executing PrintAction (attempt %d) with context: %v\n", a.attempts, context)
		if a.FailAfter > 0 && a.attempts <= a.FailAfter {
			return nil, fmt.Errorf("simulated failure on attempt %d", a.attempts)
		}
		value, ok := context["input"]
		if !ok {
			return nil, fmt.Errorf("input not found in context")
		}
		return value, nil
	}
}

func (a *PrintAction) ExecuteAsync(ctx context.Context, context map[string]interface{}) (chan interface{}, chan error) {
	resultCh := make(chan interface{})
	errCh := make(chan error)
	go func() {
		defer close(resultCh)
		defer close(errCh)
		select {
		case <-ctx.Done():
			errCh <- ctx.Err()
		case <-time.After(a.Delay):
			a.attempts++
			fmt.Printf("Executing PrintAction async (attempt %d) with context: %v\n", a.attempts, context)
			if a.FailAfter > 0 && a.attempts <= a.FailAfter {
				errCh <- fmt.Errorf("simulated failure on attempt %d", a.attempts)
				return
			}
			value, ok := context["input"]
			if !ok {
				errCh <- fmt.Errorf("input not found in context")
				return
			}
			resultCh <- value
		}
	}()
	return resultCh, errCh
}

type LoggingHandler struct{}

func (h LoggingHandler) Handle(ctx context.Context, event events.Event) error {
	fmt.Printf("Event received: Type=%s, InstanceID=%d, Data=%v\n", event.Type, event.InstanceID, event.Data)
	return nil
}

func runSequentialWorkflow(ctx context.Context, engine *workflow.WorkflowEngine, store storage.Storage) {
	wf := types.Workflow{
		ID:   2001,
		Name: "Sequential Task Workflow",
		Nodes: []types.Node{
			{ID: 1, Type: "start"},
			{ID: 2, Type: "task", Action: "print", MaxRetries: 2, RetryDelaySec: 2},
			{ID: 3, Type: "end"},
			{ID: 4, Type: "error"},
		},
		Transitions: []types.Transition{
			{FromNodeID: 1, ToNodeID: 2, Condition: "true"},
			{FromNodeID: 2, ToNodeID: 3, Condition: "result != nil"},
			{FromNodeID: 2, ToNodeID: 4, Condition: "result == nil"},
		},
	}
	if err := engine.RegisterWorkflow(ctx, wf); err != nil {
		fmt.Println("Error registering Sequential Workflow:", err)
		return
	}
	engine.RegisterAction(ctx, "print", &PrintAction{Delay: 500 * time.Millisecond, FailAfter: 1})
	instance, err := engine.StartWorkflow(ctx, 2001, map[string]interface{}{"input": "sequential"})
	if err != nil {
		fmt.Println("Error starting Sequential Workflow:", err)
		return
	}
	fmt.Printf("Sequential Instance ID: %d, State: %s, CurrentNodeID: %d\n", instance.ID, instance.State, instance.CurrentNodeID)
	time.Sleep(10 * time.Second)
	finalInst, err := store.GetInstance(ctx, instance.ID)
	if err != nil {
		fmt.Println("Error getting Sequential final instance:", err)
		return
	}
	fmt.Printf("Sequential Final State: ID: %d, State: %s, CurrentNodeID: %d\n", finalInst.ID, finalInst.State, finalInst.CurrentNodeID)
}

func runGatewayWorkflow(ctx context.Context, engine *workflow.WorkflowEngine, store storage.Storage) {
	wf := types.Workflow{
		ID:   2002,
		Name: "Conditional Gateway Workflow",
		Nodes: []types.Node{
			{ID: 1, Type: "start"},
			{ID: 2, Type: "task", Action: "print", MaxRetries: 2, RetryDelaySec: 1},
			{ID: 3, Type: "gateway"},
			{ID: 4, Type: "end", Metadata: map[string]interface{}{"path": "positive"}},
			{ID: 5, Type: "end", Metadata: map[string]interface{}{"path": "negative"}},
		},
		Transitions: []types.Transition{
			{FromNodeID: 1, ToNodeID: 2, Condition: "true"},
			{FromNodeID: 2, ToNodeID: 3, Condition: "result != nil"},
			{FromNodeID: 3, ToNodeID: 4, Condition: "result == 'positive'"},
			{FromNodeID: 3, ToNodeID: 5, Condition: "result == 'negative'"},
		},
	}
	if err := engine.RegisterWorkflow(ctx, wf); err != nil {
		fmt.Println("Error registering Gateway Workflow:", err)
		return
	}
	engine.RegisterAction(ctx, "print", &PrintAction{Delay: 500 * time.Millisecond, FailAfter: 1})
	instance, err := engine.StartWorkflow(ctx, 2002, map[string]interface{}{"input": "positive"})
	if err != nil {
		fmt.Println("Error starting Gateway Workflow:", err)
		return
	}
	fmt.Printf("Gateway Instance ID: %d, State: %s, CurrentNodeID: %d\n", instance.ID, instance.State, instance.CurrentNodeID)
	time.Sleep(12 * time.Second) // 增加等待时间
	finalInst, err := store.GetInstance(ctx, instance.ID)
	if err != nil {
		fmt.Println("Error getting Gateway final instance:", err)
		return
	}
	fmt.Printf("Gateway Final State: ID: %d, State: %s, CurrentNodeID: %d\n", finalInst.ID, finalInst.State, finalInst.CurrentNodeID)
}

func runParallelWorkflow(ctx context.Context, engine *workflow.WorkflowEngine, store storage.Storage) {
	wf := types.Workflow{
		ID:   2003,
		Name: "Parallel Task Workflow",
		Nodes: []types.Node{
			{ID: 1, Type: "start"},
			{ID: 2, Type: "task", Action: "print", MaxRetries: 2, RetryDelaySec: 1},
			{ID: 3, Type: "task", Action: "print", MaxRetries: 2, RetryDelaySec: 1},
			{ID: 4, Type: "gateway", Logic: "and"},
			{ID: 5, Type: "end"},
			{ID: 6, Type: "error"},
		},
		Transitions: []types.Transition{
			{FromNodeID: 1, ToNodeID: 2, Condition: "true"},
			{FromNodeID: 1, ToNodeID: 3, Condition: "true"},
			{FromNodeID: 2, ToNodeID: 4, Condition: "result != nil"},
			{FromNodeID: 3, ToNodeID: 4, Condition: "result != nil"},
			{FromNodeID: 4, ToNodeID: 5, Condition: "true"},
			{FromNodeID: 2, ToNodeID: 6, Condition: "result == nil"},
			{FromNodeID: 3, ToNodeID: 6, Condition: "result == nil"},
		},
	}
	if err := engine.RegisterWorkflow(ctx, wf); err != nil {
		fmt.Println("Error registering Parallel Workflow:", err)
		return
	}
	engine.RegisterAction(ctx, "print", &PrintAction{Delay: 500 * time.Millisecond, FailAfter: 1})
	instance, err := engine.StartWorkflow(ctx, 2003, map[string]interface{}{"input": "parallel"})
	if err != nil {
		fmt.Println("Error starting Parallel Workflow:", err)
		return
	}
	fmt.Printf("Parallel Instance ID: %d, State: %s, CurrentNodeID: %d\n", instance.ID, instance.State, instance.CurrentNodeID)
	time.Sleep(12 * time.Second)
	finalInst, err := store.GetInstance(ctx, instance.ID)
	if err != nil {
		fmt.Println("Error getting Parallel final instance:", err)
		return
	}
	fmt.Printf("Parallel Final State: ID: %d, State: %s, CurrentNodeID: %d\n", finalInst.ID, finalInst.State, finalInst.CurrentNodeID)
}

func runComplexWorkflow(ctx context.Context, engine *workflow.WorkflowEngine, store storage.Storage) {
	wf := types.Workflow{
		ID:   2004,
		Name: "Complex Approval Workflow",
		Nodes: []types.Node{
			{ID: 1, Type: "start"},
			{ID: 2, Type: "task", Action: "print", MaxRetries: 2, RetryDelaySec: 1},
			{ID: 3, Type: "manual"},
			{ID: 4, Type: "gateway"},
			{ID: 5, Type: "task", Action: "print", MaxRetries: 1, RetryDelaySec: 1},
			{ID: 6, Type: "end"},
			{ID: 7, Type: "error"},
		},
		Transitions: []types.Transition{
			{FromNodeID: 1, ToNodeID: 2, Condition: "true"},
			{FromNodeID: 2, ToNodeID: 3, Condition: "result != nil"},
			{FromNodeID: 2, ToNodeID: 7, Condition: "result == nil"},
			{FromNodeID: 3, ToNodeID: 4, Condition: "approved == true"},
			{FromNodeID: 3, ToNodeID: 7, Condition: "approved == false"},
			{FromNodeID: 4, ToNodeID: 5, Condition: "true"},
			{FromNodeID: 5, ToNodeID: 6, Condition: "result != nil"},
			{FromNodeID: 5, ToNodeID: 7, Condition: "result == nil"},
		},
	}
	if err := engine.RegisterWorkflow(ctx, wf); err != nil {
		fmt.Println("Error registering Complex Workflow:", err)
		return
	}
	engine.RegisterAction(ctx, "print", &PrintAction{Delay: 500 * time.Millisecond, FailAfter: 1})
	instance, err := engine.StartWorkflow(ctx, 2004, map[string]interface{}{"input": "complex"})
	if err != nil {
		fmt.Println("Error starting Complex Workflow:", err)
		return
	}
	fmt.Printf("Complex Instance ID: %d, State: %s, CurrentNodeID: %d\n", instance.ID, instance.State, instance.CurrentNodeID)
	time.Sleep(6 * time.Second)
	inst, err := store.GetInstance(ctx, instance.ID)
	if err != nil {
		fmt.Println("Error checking instance state:", err)
		return
	}
	if inst.State != "pending_approval" {
		fmt.Println("Cannot confirm transition: instance is not in pending_approval state, current state:", inst.State)
		return
	}
	fmt.Println("Simulating manual approval...")
	if err := engine.ConfirmTransition(ctx, instance.ID, true); err != nil {
		fmt.Println("Error confirming transition:", err)
		return
	}
	time.Sleep(12 * time.Second) // 增加等待时间
	finalInst, err := store.GetInstance(ctx, instance.ID)
	if err != nil {
		fmt.Println("Error getting Complex final instance:", err)
		return
	}
	fmt.Printf("Complex Final State: ID: %d, State: %s, CurrentNodeID: %d\n", finalInst.ID, finalInst.State, finalInst.CurrentNodeID)
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Second)
	defer cancel()

	snowflake := generator.NewSnowflake(time.Now().Add(-1*time.Second), 1)
	store, err := storage.NewRedisStorage(storage.RedisOptions{
		Addr:         "localhost:6379",
		Password:     "",
		DB:           0,
		PoolSize:     10,
		MinIdleConns: 2,
		IdleTimeout:  5 * time.Minute,
	})
	if err != nil {
		fmt.Println("Failed to initialize Redis storage:", err)
		return
	}

	engine, err := workflow.NewWorkflowEngine(snowflake, store, rules.NewExprEvaluator())
	if err != nil {
		fmt.Println("Failed to create engine:", err)
		return
	}
	defer engine.Stop(context.Background())

	engine.SubscribeEvent("state_changed", LoggingHandler{})
	engine.SubscribeEvent("error_occurred", LoggingHandler{})
	engine.SubscribeEvent("pending_approval", LoggingHandler{})

	fmt.Println("=== Running Sequential Workflow ===")
	sequentialCtx, _ := context.WithTimeout(ctx, 25*time.Second)
	runSequentialWorkflow(sequentialCtx, engine, store)

	fmt.Println("\n=== Running Gateway Workflow ===")
	gatewayCtx, _ := context.WithTimeout(ctx, 25*time.Second)
	runGatewayWorkflow(gatewayCtx, engine, store)

	fmt.Println("\n=== Running Parallel Workflow ===")
	parallelCtx, _ := context.WithTimeout(ctx, 25*time.Second)
	runParallelWorkflow(parallelCtx, engine, store)

	fmt.Println("\n=== Running Complex Workflow ===")
	complexCtx, _ := context.WithTimeout(ctx, 30*time.Second)
	runComplexWorkflow(complexCtx, engine, store)

	fmt.Println("\n=== All Workflows Completed ===")
}
