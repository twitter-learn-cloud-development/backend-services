package tool

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"twitter-clone/internal/module/agent/workflow/engine"
	"twitter-clone/internal/module/agent/workflow/guardrails"
)

type recordingTool struct {
	spec   ToolSpec
	called bool
	inputs map[string]interface{}
}

func (t *recordingTool) Spec() ToolSpec {
	return t.spec
}

func (t *recordingTool) Execute(ctx context.Context, inputs map[string]interface{}) (map[string]interface{}, error) {
	t.called = true
	t.inputs = inputs
	return map[string]interface{}{"ok": true}, nil
}

type approvalGateFunc func(context.Context, ApprovalCheck) (ApprovalGrant, error)

func (f approvalGateFunc) Authorize(ctx context.Context, check ApprovalCheck) (ApprovalGrant, error) {
	return f(ctx, check)
}

func newRecordingPublishTool() *recordingTool {
	return &recordingTool{spec: ToolSpec{
		Name: "PublishTweet", Description: "test write",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"content":{"type":"string"}},"required":["content"]}`),
		Category:    CategoryWrite, Permission: PermissionAuthenticated,
		Approval: ApprovalRequired, Idempotency: IdempotencyPolicy{Required: true},
		Timeout: time.Second,
	}}
}

func TestToolNodeBlocksUnauthenticatedSensitiveTool(t *testing.T) {
	recorder := newRecordingPublishTool()
	registry := NewRegistry()
	require.NoError(t, registry.Register(recorder))
	executor := NewExecutor(registry)

	node := NewToolNode("publish_node", "PublishTweet", executor)
	_, err := node.Execute(context.Background(), engine.NewBlackboard(), map[string]interface{}{"content": "hello"})

	require.ErrorIs(t, err, ErrUnauthenticated)
	require.False(t, recorder.called)
}

func TestToolNodeInjectsAuthenticatedUserID(t *testing.T) {
	recorder := newRecordingPublishTool()
	registry := NewRegistry()
	require.NoError(t, registry.Register(recorder))
	executor := NewExecutor(registry, WithApprovalGate(approvalGateFunc(
		func(context.Context, ApprovalCheck) (ApprovalGrant, error) {
			return ApprovalGrant{ApprovalID: "approval-1"}, nil
		},
	)))

	ctx := guardrails.InjectUserContext(context.Background(), 42)
	ctx = InjectExecutionMetadata(ctx, ExecutionMetadata{RunID: "run-1", Source: SourceWorkflow})
	node := NewToolNode("publish_node", "PublishTweet", executor)
	_, err := node.Execute(ctx, engine.NewBlackboard(), map[string]interface{}{
		"content": "hello", "user_id": uint64(7),
	})

	require.NoError(t, err)
	require.True(t, recorder.called)
	require.Equal(t, uint64(42), recorder.inputs["user_id"])
}

func TestRegistryRejectsDuplicateToolNames(t *testing.T) {
	registry := NewRegistry()
	require.NoError(t, registry.Register(newRecordingPublishTool()))
	require.ErrorContains(t, registry.Register(newRecordingPublishTool()), "duplicate name")
}

func TestRegistriesAreIsolated(t *testing.T) {
	first := NewRegistry()
	second := NewRegistry()
	require.NoError(t, first.Register(newRecordingPublishTool()))
	_, exists := second.Get("PublishTweet")
	require.False(t, exists)
}
