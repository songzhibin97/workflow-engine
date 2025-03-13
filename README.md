# Workflow Engine Documentation

## Overview

The Workflow Engine is a lightweight, extensible system built in Go to define, execute, and manage workflows. It supports sequential and parallel task execution, error handling, manual approval steps, and persistence with pluggable storage backends (e.g., memory, Redis). The engine is designed for simplicity and flexibility, suitable for both single-process and distributed applications.

### Key Features
- **Node Types**: Supports `start`, `task`, `gateway` (AND/OR logic), `end`, `error`, and `manual` nodes.
- **Transitions**: Condition-based flow control using an expression evaluator.
- **Error Handling**: Retry mechanisms, rollback, and exception transitions.
- **Parallel Execution**: Gateway nodes for concurrent task processing.
- **Manual Approval**: Interruptible workflows requiring human confirmation.
- **Persistence**: Pluggable storage (Memory, Redis) for workflows and instances.
- **Event System**: Asynchronous event publishing for state changes and notifications.


## Installation

### Prerequisites
- Go 1.18 or later
- Redis (optional, for persistence)

### Dependencies
Install required packages:

```bash
go get github.com/songzhibin97/gkit
go get github.com/expr-lang/expr
go get github.com/redis/go-redis/v9  # Optional, for Redis storage
```


### Build
Clone the repository and build the project:

```bash
git clone https://github.com/songzhibin97/workflow-engine.git
cd workflow-engine
go build
```

## Usage

### Defining a Workflow
A workflow is defined using the `types.Workflow` struct.

```go
import "github.com/songzhibin97/workflow-engine/pkg/types"

wf := types.Workflow{
    ID:   1001,
    Name: "Sample Workflow",
    Nodes: []types.Node{
        {ID: 1, Type: "start"},
        {ID: 2, Type: "task", Action: "print", MaxRetries: 2, RetryDelaySec: 1},
        {ID: 3, Type: "manual"},
        {ID: 4, Type: "end"},
    },
    Transitions: []types.Transition{
        {FromNodeID: 1, ToNodeID: 2, Condition: "true"},
        {FromNodeID: 2, ToNodeID: 3, Condition: "result == 'hello'"},
        {FromNodeID: 3, ToNodeID: 4, Condition: "approved == true"},
    },
}
```

### Creating an Engine
Initialize the engine with a generator and storage backend.

```go
import (
    "github.com/songzhibin97/workflow-engine/pkg/workflow"
    "github.com/songzhibin97/workflow-engine/pkg/storage"
    "github.com/songzhibin97/gkit/generator"
)

snowflake := generator.NewSnowflake(time.Now(), 1)
store := storage.NewMemoryStorage() // Or storage.NewRedisStorage("localhost:6379", "", 0)
engine, err := workflow.NewWorkflowEngine(snowflake, store)
if err != nil {
    panic(err)
}
defer engine.Stop()
```

### Registering Actions
Actions are tasks executed by task nodes.

```go
type PrintAction struct {
    Delay time.Duration
}

func (a PrintAction) Execute(ctx context.Context, context map[string]interface{}) (interface{}, error) {
    time.Sleep(a.Delay)
    return context["input"], nil
}

engine.RegisterAction(ctx, "print", &PrintAction{Delay: 500 * time.Millisecond})
```

### Starting a Workflow
Start a workflow instance with an initial context.

```go
instance, err := engine.StartWorkflow(ctx, 1001, map[string]interface{}{"input": "hello"})
if err != nil {
    panic(err)
}
fmt.Printf("Started Instance ID: %d, State: %s\n", instance.ID, instance.State)
```

### Handling Manual Approval
Listen for `pending_approval` events and confirm manually.

```go
type LoggingHandler struct{}

func (h LoggingHandler) Handle(ctx context.Context, event events.Event) error {
    fmt.Printf("Event: %s, InstanceID: %d, Data: %v\n", event.Type, event.InstanceID, event.Data)
    return nil
}

engine.SubscribeEvent("pending_approval", LoggingHandler{})

// Simulate approval after a delay
time.Sleep(2 * time.Second)
engine.ConfirmTransition(ctx, instance.ID, true)
```

## Persistence
Workflows and instances are saved to the configured storage backend.

```go
loadedInst, err := store.GetInstance(ctx, instance.ID)
if err != nil {
    panic(err)
}
fmt.Printf("Loaded Instance: ID: %d, State: %s\n", loadedInst.ID, loadedInst.State)
```

## Testing
Run unit tests:

```bash
go test ./...
```

## Contributing
1. Fork the repository.
2. Create a feature branch (`git checkout -b feature/new-feature`).
3. Commit changes (`git commit -m "Add new feature"`).
4. Push to the branch (`git push origin feature/new-feature`).
5. Open a Pull Request.

## License
This project is licensed under the MIT License. See `LICENSE` for details.

