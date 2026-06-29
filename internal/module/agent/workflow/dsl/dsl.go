package dsl

import "encoding/json"

// WorkflowDSL 代表用户前端拖拽生成的 JSON 工作流 DSL
type WorkflowDSL struct {
	ID     uint64    `json:"id"`
	UserID uint64    `json:"user_id"`
	Name   string    `json:"name"`
	Nodes  []NodeDSL `json:"nodes"`
	Edges  []EdgeDSL `json:"edges"`
}

// NodeDSL 定义工作流中单个节点的配置元数据
type NodeDSL struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`        // 例如: start, end, llm, tool, router, parallel, merge
	Properties json.RawMessage `json:"properties"`  // 自定义属性，例如 prompt 模板或工具名
	TimeoutSec int             `json:"timeout_sec"`  // 单节点执行超时时间
}

// EdgeDSL 定义节点之间的输入与输出管道的映射连线关系
type EdgeDSL struct {
	ID           string `json:"id"`
	Source       string `json:"source"`        // 源节点 ID
	Target       string `json:"target"`        // 目标节点 ID
	SourceHandle string `json:"source_handle"` // 上游输出槽 (如 "success", "fail")
	TargetHandle string `json:"target_handle"` // 下游输入槽 (如 "input_text")
}
