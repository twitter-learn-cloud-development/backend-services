package dsl

import "encoding/json"

const (
	// CurrentVersion identifies the execution semantics understood by the
	// current compiler. Missing versions are treated as v1 for legacy DSLs.
	CurrentVersion = "1.0"

	ReducerAppend = "append"
	ReducerSum    = "sum"
	ReducerMin    = "min"
	ReducerMax    = "max"
	ReducerMerge  = "merge"
	ReducerFirst  = "first"
	ReducerLast   = "last"
)

// WorkflowDSL 代表用户前端拖拽生成的 JSON 工作流 DSL
type WorkflowDSL struct {
	ID              uint64     `json:"id,omitempty"`
	UserID          uint64     `json:"user_id,omitempty"`
	Name            string     `json:"name"`
	DSLVersion      string     `json:"dsl_version,omitempty"`
	WorkflowVersion int64      `json:"workflow_version,omitempty"`
	Budget          *BudgetDSL `json:"budget,omitempty"`
	Nodes           []NodeDSL  `json:"nodes"`
	Edges           []EdgeDSL  `json:"edges"`
}

// BudgetDSL declares workflow-wide execution limits. Zero values use service
// defaults except cost, where zero explicitly disables monetary enforcement.
type BudgetDSL struct {
	MaxNodeExecutions      int   `json:"max_node_executions,omitempty"`
	MaxParallelNodes       int   `json:"max_parallel_nodes,omitempty"`
	TimeoutSec             int   `json:"timeout_sec,omitempty"`
	MaxTotalTokens         int   `json:"max_total_tokens,omitempty"`
	MaxEstimatedCostMicros int64 `json:"max_estimated_cost_micros,omitempty"`
}

// NodeDSL 定义工作流中单个节点的配置元数据
type NodeDSL struct {
	ID           string           `json:"id"`
	Type         string           `json:"type"`                    // 例如: start, end, llm, tool, router, parallel, merge
	Properties   json.RawMessage  `json:"properties,omitempty"`    // 自定义属性，例如 prompt 模板或工具名
	InputSchema  json.RawMessage  `json:"input_schema,omitempty"`  // 节点输入 JSON Schema
	OutputSchema json.RawMessage  `json:"output_schema,omitempty"` // 节点输出 JSON Schema
	TimeoutSec   int              `json:"timeout_sec,omitempty"`   // 单节点执行超时时间
	Retry        *RetryPolicyDSL  `json:"retry,omitempty"`
	Policy       json.RawMessage  `json:"policy,omitempty"`
	ProfileRef   string           `json:"profile_ref,omitempty"`
	ProviderRef  string           `json:"provider_ref,omitempty"`
	Writes       []StateWriteDSL  `json:"writes,omitempty"`
	Compensation *CompensationDSL `json:"compensation,omitempty"`
}

// RetryPolicyDSL declares bounded node retry semantics. The scheduler does
// not infer retries from node type or provider errors.
type RetryPolicyDSL struct {
	MaxAttempts      int     `json:"max_attempts,omitempty"`
	InitialBackoffMS int64   `json:"initial_backoff_ms,omitempty"`
	MaxBackoffMS     int64   `json:"max_backoff_ms,omitempty"`
	Multiplier       float64 `json:"multiplier,omitempty"`
	Jitter           float64 `json:"jitter,omitempty"`
}

// StateWriteDSL reserves an explicit global state path. Parallel writers must
// declare the same built-in reducer so the coordinator can merge deterministically.
// Node-scoped outputs remain isolated under the node ID and need no reducer.
type StateWriteDSL struct {
	Path    string `json:"path"`
	Source  string `json:"source,omitempty"`
	Reducer string `json:"reducer,omitempty"`
}

// CompensationDSL declares a governed tool call that can compensate one
// successfully completed tool node. It is planned durably before execution.
type CompensationDSL struct {
	ToolName   string          `json:"tool_name"`
	Properties json.RawMessage `json:"properties,omitempty"`
	TimeoutSec int             `json:"timeout_sec,omitempty"`
	Retry      *RetryPolicyDSL `json:"retry,omitempty"`
}

// EdgeDSL 定义节点之间的输入与输出管道的映射连线关系
type EdgeDSL struct {
	ID           string `json:"id"`
	Source       string `json:"source"`        // 源节点 ID
	Target       string `json:"target"`        // 目标节点 ID
	SourceHandle string `json:"source_handle"` // 上游输出槽 (如 "success", "fail")
	TargetHandle string `json:"target_handle"` // 下游输入槽 (如 "input_text")
}
