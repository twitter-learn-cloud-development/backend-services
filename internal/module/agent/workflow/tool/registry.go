package tool

import (
	"context"
	"fmt"
	"sync"

	"twitter-clone/internal/module/agent/workflow/engine"
	"twitter-clone/internal/module/agent/workflow/guardrails"
)

// AgentTool 定义了所有可被工作流节点调用的业务工具标准接口
type AgentTool interface {
	Name() string
	Description() string
	InputSchema() string // 参数 JSON Schema，描述该工具需要哪些输入
	Execute(ctx context.Context, inputs map[string]interface{}) (map[string]interface{}, error)
}

// InputGuardrail 在工具执行前校验与改写入参，例如注入认证态 user_id。
type InputGuardrail interface {
	ValidateAndInjectToolInputs(ctx context.Context, toolName string, inputs map[string]interface{}) (map[string]interface{}, error)
}

// ToolRegistry 工具自注册中心
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]AgentTool
}

var (
	globalRegistry *ToolRegistry
	once           sync.Once
)

// GetRegistry 获取全局唯一的工具注册中心实例
func GetRegistry() *ToolRegistry {
	once.Do(func() {
		globalRegistry = &ToolRegistry{
			tools: make(map[string]AgentTool),
		}
	})
	return globalRegistry
}

// Register 注册一个工具
func (r *ToolRegistry) Register(t AgentTool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
}

// Get 依据名字装载工具
func (r *ToolRegistry) Get(name string) (AgentTool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// ToolNode 是一个适配器，将标准的 AgentTool 包装为可供调度引擎调用的 WorkflowNode
type ToolNode struct {
	id        string
	toolName  string
	guardrail InputGuardrail
}

// NewToolNode 实例化一个工具节点
func NewToolNode(id, toolName string) *ToolNode {
	return &ToolNode{
		id:        id,
		toolName:  toolName,
		guardrail: guardrails.NewSecurityGuardrail(),
	}
}

// NewToolNodeWithGuardrail 实例化一个带自定义护栏的工具节点，主要用于测试或灰度替换策略。
func NewToolNodeWithGuardrail(id, toolName string, guardrail InputGuardrail) *ToolNode {
	return &ToolNode{
		id:        id,
		toolName:  toolName,
		guardrail: guardrail,
	}
}

// ID 返回节点 ID
func (tn *ToolNode) ID() string {
	return tn.id
}

// Type 返回节点类型
func (tn *ToolNode) Type() string {
	return "tool"
}

// Execute 适配执行方法，自动从注册中心装载对应的 AgentTool 并触发运行
func (tn *ToolNode) Execute(ctx context.Context, blackboard *engine.Blackboard, inputs map[string]interface{}) (map[string]interface{}, error) {
	reg := GetRegistry()
	tool, ok := reg.Get(tn.toolName)
	if !ok {
		return nil, fmt.Errorf("agent tool %s not registered in global registry", tn.toolName)
	}

	guardedInputs := inputs
	if tn.guardrail != nil {
		var err error
		guardedInputs, err = tn.guardrail.ValidateAndInjectToolInputs(ctx, tn.toolName, cloneInputs(inputs))
		if err != nil {
			return nil, fmt.Errorf("tool %s blocked by security guardrail: %w", tn.toolName, err)
		}
	}

	// 触发工具执行
	outputs, err := tool.Execute(ctx, guardedInputs)
	if err != nil {
		return nil, fmt.Errorf("tool %s execution error: %w", tn.toolName, err)
	}

	return outputs, nil
}

func cloneInputs(inputs map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(inputs))
	for k, v := range inputs {
		cloned[k] = v
	}
	return cloned
}
