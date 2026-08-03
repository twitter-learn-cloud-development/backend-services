package tool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"twitter-clone/internal/module/agent/workflow/engine"
	"twitter-clone/internal/module/agent/workflow/guardrails"
)

type inputValidator func(inputs map[string]interface{}) error

type RegisteredTool struct {
	Spec     ToolSpec
	Handler  ToolHandler
	validate inputValidator
}

// ToolRegistry is an injected catalog. It has no package-global mutable
// instance, so tests and service roles cannot leak registrations into each
// other.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]RegisteredTool
}

func NewRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]RegisteredTool)}
}

func (r *ToolRegistry) Register(tool AgentTool) error {
	if tool == nil {
		return fmt.Errorf("register tool: handler is nil")
	}
	return r.RegisterHandler(tool.Spec(), tool)
}

func (r *ToolRegistry) RegisterHandler(spec ToolSpec, handler ToolHandler) error {
	if r == nil {
		return fmt.Errorf("register tool: registry is nil")
	}
	if handler == nil {
		return fmt.Errorf("register tool: handler is nil")
	}
	normalized, err := spec.Normalize()
	if err != nil {
		return fmt.Errorf("register tool: %w", err)
	}
	validator, err := compileInputSchema(normalized)
	if err != nil {
		return fmt.Errorf("register tool %s schema: %w", normalized.Name, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[normalized.Name]; exists {
		return fmt.Errorf("register tool %s: duplicate name", normalized.Name)
	}
	r.tools[normalized.Name] = RegisteredTool{
		Spec: normalized, Handler: handler, validate: validator,
	}
	return nil
}

func (r *ToolRegistry) Get(name string) (RegisteredTool, bool) {
	if r == nil {
		return RegisteredTool{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	registered, ok := r.tools[name]
	return registered, ok
}

func (r *ToolRegistry) Specs() []ToolSpec {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	specs := make([]ToolSpec, 0, len(r.tools))
	for _, registered := range r.tools {
		specs = append(specs, registered.Spec)
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	return specs
}

func compileInputSchema(spec ToolSpec) (inputValidator, error) {
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(spec.InputSchema))
	if err != nil {
		return nil, fmt.Errorf("decode JSON schema: %w", err)
	}
	compiler := jsonschema.NewCompiler()
	location := "mem://agent-tool/" + spec.Name + ".json"
	if err := compiler.AddResource(location, value); err != nil {
		return nil, fmt.Errorf("add JSON schema resource: %w", err)
	}
	compiled, err := compiler.Compile(location)
	if err != nil {
		return nil, fmt.Errorf("compile JSON schema: %w", err)
	}
	return func(inputs map[string]interface{}) error {
		return compiled.Validate(inputs)
	}, nil
}

// ToolNode adapts a governed registered tool to WorkflowNode.
type ToolNode struct {
	id       string
	toolName string
	executor *Executor
}

func NewToolNode(id, toolName string, executor *Executor) *ToolNode {
	return &ToolNode{id: id, toolName: toolName, executor: executor}
}

func (tn *ToolNode) ID() string {
	return tn.id
}

func (tn *ToolNode) Type() string {
	return "tool"
}

func (tn *ToolNode) Execute(ctx context.Context, state engine.StateView, inputs map[string]interface{}) (map[string]interface{}, error) {
	if tn.executor == nil {
		return nil, fmt.Errorf("tool %s executor is not configured", tn.toolName)
	}
	userID, _ := guardrails.AuthenticatedUserID(ctx)
	metadata := ExecutionMetadataFromContext(ctx)
	outputs, err := tn.executor.ExecuteRegistered(ctx, ExecutionRequest{
		ToolName:       tn.toolName,
		Inputs:         inputs,
		Identity:       CallerIdentity{UserID: userID},
		RunID:          metadata.RunID,
		StepID:         tn.id,
		Source:         firstExecutionSource(metadata.Source, SourceWorkflow),
		IdempotencyKey: workflowIdempotencyKey(metadata.RunID, tn.id, tn.toolName),
	})
	if err == nil {
		return outputs, nil
	}
	var pending *ApprovalPendingError
	if errors.As(err, &pending) {
		return nil, engine.NewSuspensionErrorWithCause(tn.id, "tool approval required", "", map[string]interface{}{
			"approval_request_id": pending.ApprovalID,
			"tool_name":           tn.toolName,
			"idempotency_key":     workflowIdempotencyKey(metadata.RunID, tn.id, tn.toolName),
		}, err)
	}
	return nil, err
}

func firstExecutionSource(value, fallback ExecutionSource) ExecutionSource {
	if value != "" {
		return value
	}
	return fallback
}

func workflowIdempotencyKey(runID, stepID, toolName string) string {
	if runID == "" || stepID == "" || toolName == "" {
		return ""
	}
	return runID + ":" + stepID + ":" + toolName
}
