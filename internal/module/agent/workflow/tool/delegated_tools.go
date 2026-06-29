package tool

import (
	"context"
	"errors"
)

// DelegatedExecutor keeps workflow tools decoupled from concrete MCP clients
// and AgentService internals.
type DelegatedExecutor func(ctx context.Context, inputs map[string]interface{}) (map[string]interface{}, error)

// DelegatedTool adapts an injected executor to the workflow AgentTool contract.
type DelegatedTool struct {
	name        string
	description string
	inputSchema string
	execute     DelegatedExecutor
}

func NewDelegatedTool(name, description, inputSchema string, execute DelegatedExecutor) *DelegatedTool {
	return &DelegatedTool{
		name:        name,
		description: description,
		inputSchema: inputSchema,
		execute:     execute,
	}
}

func (t *DelegatedTool) Name() string {
	return t.name
}

func (t *DelegatedTool) Description() string {
	return t.description
}

func (t *DelegatedTool) InputSchema() string {
	return t.inputSchema
}

func (t *DelegatedTool) Execute(ctx context.Context, inputs map[string]interface{}) (map[string]interface{}, error) {
	if t.execute == nil {
		return nil, errors.New("delegated tool executor is not configured")
	}
	return t.execute(ctx, inputs)
}
