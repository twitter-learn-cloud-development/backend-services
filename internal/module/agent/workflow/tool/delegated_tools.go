package tool

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// DelegatedExecutor keeps workflow tools decoupled from concrete MCP clients
// and AgentService internals.
type DelegatedExecutor func(ctx context.Context, inputs map[string]interface{}) (map[string]interface{}, error)

// DelegatedTool adapts an injected executor to the workflow AgentTool contract.
type DelegatedTool struct {
	spec    ToolSpec
	execute DelegatedExecutor
}

func NewDelegatedTool(name, description, inputSchema string, execute DelegatedExecutor) *DelegatedTool {
	return NewDelegatedToolWithSpec(ToolSpec{
		Name: name, Description: description, InputSchema: json.RawMessage(inputSchema),
		Category: CategoryRead, Permission: PermissionAuthenticated,
		Timeout: 30 * time.Second, Retry: RetryPolicy{MaxAttempts: 1}, Approval: ApprovalNever,
	}, execute)
}

func NewDelegatedToolWithSpec(spec ToolSpec, execute DelegatedExecutor) *DelegatedTool {
	return &DelegatedTool{
		spec: spec, execute: execute,
	}
}

func (t *DelegatedTool) Name() string {
	return t.spec.Name
}

func (t *DelegatedTool) Description() string {
	return t.spec.Description
}

func (t *DelegatedTool) InputSchema() string {
	return string(t.spec.InputSchema)
}

func (t *DelegatedTool) Spec() ToolSpec {
	return t.spec
}

func (t *DelegatedTool) Execute(ctx context.Context, inputs map[string]interface{}) (map[string]interface{}, error) {
	if t.execute == nil {
		return nil, errors.New("delegated tool executor is not configured")
	}
	return t.execute(ctx, inputs)
}
