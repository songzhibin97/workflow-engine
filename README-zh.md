# 工作流引擎文档

## 概述

工作流引擎是一个用 Go 语言构建的轻量级、可扩展系统，用于定义、执行和管理工作流。它支持顺序和并行任务执行、错误处理、手动审批步骤，并提供可插拔存储后端（如内存、Redis）进行持久化。该引擎设计注重简洁性和灵活性，适用于单进程和分布式应用。

### 主要特性
- **节点类型**：支持 `start`、`task`、`gateway`（AND/OR 逻辑）、`end`、`error` 和 `manual` 节点。
- **流转控制**：基于条件表达式求值，实现动态流转。
- **错误处理**：支持重试机制、回滚和异常流转。
- **并行执行**：通过 `gateway` 节点处理并行任务。
- **手动审批**：支持 `manual` 节点，暂停工作流等待人工确认。
- **持久化存储**：可插拔存储后端（内存、Redis）。
- **事件系统**：异步事件发布，用于状态变更通知。

## 安装

### 前提条件
- Go 1.18 或更高版本
- Redis（可选，用于持久化）

### 依赖安装
```bash
go get github.com/songzhibin97/gkit
go get github.com/expr-lang/expr
go get github.com/redis/go-redis/v9
```

### 构建
```bash
git clone https://github.com/songzhibin97/workflow-engine.git
cd workflow-engine
go build
```

## 使用方法

### 定义工作流
```go
import "github.com/songzhibin97/workflow-engine/pkg/types"

wf := types.Workflow{
    ID:   1001,
    Name: "示例工作流",
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

### 创建引擎
```go
import (
    "github.com/songzhibin97/workflow-engine/pkg/workflow"
    "github.com/songzhibin97/workflow-engine/pkg/storage"
    "github.com/songzhibin97/gkit/generator"
)

snowflake := generator.NewSnowflake(time.Now(), 1)
store := storage.NewMemoryStorage() // 或 storage.NewRedisStorage("localhost:6379", "", 0)
engine, err := workflow.NewWorkflowEngine(snowflake, store)
if err != nil {
    panic(err)
}
defer engine.Stop()
```

### 注册动作
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

### 启动工作流
```go
instance, err := engine.StartWorkflow(ctx, 1001, map[string]interface{}{"input": "hello"})
if err != nil {
    panic(err)
}
fmt.Printf("启动实例 ID: %d, 状态: %s\n", instance.ID, instance.State)
```

### 处理手动审批
```go
type LoggingHandler struct{}

func (h LoggingHandler) Handle(ctx context.Context, event events.Event) error {
    fmt.Printf("事件: %s, 实例ID: %d, 数据: %v\n", event.Type, event.InstanceID, event.Data)
    return nil
}

engine.SubscribeEvent("pending_approval", LoggingHandler{})

time.Sleep(2 * time.Second)
engine.ConfirmTransition(ctx, instance.ID, true)
```

## 持久化存储

工作流和实例存储在配置的后端。
```go
loadedInst, err := store.GetInstance(ctx, instance.ID)
if err != nil {
    panic(err)
}
fmt.Printf("加载实例: ID: %d, 状态: %s\n", loadedInst.ID, loadedInst.State)
```

## 配置

### 节点类型
- `start`：工作流入口。
- `task`：执行任务，可配置 `MaxRetries` 和 `RetryDelaySec`。
- `gateway`：并行执行，支持 AND/OR 逻辑。
- `manual`：暂停等待手动审批。
- `end`：流程终止。
- `error`：异常处理。

### 事件类型
- `state_changed`：状态变更。
- `error_occurred`：错误事件。
- `pending_approval`：手动审批触发。

## 扩展

### 自定义存储
```go
type CustomStorage struct{}

func (s *CustomStorage) SaveWorkflow(ctx context.Context, wf types.Workflow) error { /* ... */ }
func (s *CustomStorage) GetWorkflow(ctx context.Context, id uint64) (types.Workflow, error) { /* ... */ }
func (s *CustomStorage) SaveInstance(ctx context.Context, inst types.WorkflowInstance) error { /* ... */ }
func (s *CustomStorage) GetInstance(ctx context.Context, id uint64) (types.WorkflowInstance, error) { /* ... */ }
```

### 自定义动作
```go
type CustomAction struct{}

func (a CustomAction) Execute(ctx context.Context, context map[string]interface{}) (interface{}, error) {
    return "自定义结果", nil
}
```

## 贡献指南
1. Fork 仓库。
2. 创建功能分支（`git checkout -b feature/new-feature`）。
3. 提交更改（`git commit -m "添加新功能"`）。
4. 推送分支（`git push origin feature/new-feature`）。
5. 提交 Pull Request。

## 许可证
本项目基于 MIT 许可证，详情见 LICENSE 文件。