package types

// Workflow defines the structure of a workflow.
type Workflow struct {
	ID          uint64       `json:"id"`
	Name        string       `json:"name"`
	Nodes       []Node       `json:"nodes"`
	Transitions []Transition `json:"transitions"`
}

// Node represents a node in the workflow.
type Node struct {
	ID            uint64                 `json:"id"`
	Type          string                 `json:"type"` // "start", "task", "gateway", "end", "error", "manual"
	Action        string                 `json:"action"`
	Logic         string                 `json:"logic"` // "and" or "or"
	Metadata      map[string]interface{} `json:"metadata"`
	MaxRetries    int                    `json:"max_retries,omitempty"`
	RetryDelaySec int                    `json:"retry_delay_sec,omitempty"`
}

// Transition defines the transition rules between nodes.
type Transition struct {
	FromNodeID uint64 `json:"from_node_id"`
	ToNodeID   uint64 `json:"to_node_id"`
	Condition  string `json:"condition"` // For manual nodes, condition can be "approved" or "rejected"
}

// WorkflowInstance represents a running instance of a workflow.
type WorkflowInstance struct {
	ID            uint64                 `json:"id"`
	WorkflowID    uint64                 `json:"workflow_id"`
	State         string                 `json:"state"` // "running", "completed", "failed", "waiting", "pending_approval"
	CurrentNodeID uint64                 `json:"current_node_id"`
	Context       map[string]interface{} `json:"context"`
	CreatedAt     int64                  `json:"created_at"`
	UpdatedAt     int64                  `json:"updated_at"`
}
