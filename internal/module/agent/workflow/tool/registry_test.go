package tool

import (
	"context"
	"testing"

	"twitter-clone/internal/module/agent/workflow/engine"
	"twitter-clone/internal/module/agent/workflow/guardrails"
)

type recordingTool struct {
	name   string
	called bool
	inputs map[string]interface{}
}

func (t *recordingTool) Name() string {
	return t.name
}

func (t *recordingTool) Description() string {
	return "recording test tool"
}

func (t *recordingTool) InputSchema() string {
	return `{}`
}

func (t *recordingTool) Execute(ctx context.Context, inputs map[string]interface{}) (map[string]interface{}, error) {
	t.called = true
	t.inputs = inputs
	return map[string]interface{}{"ok": true}, nil
}

func TestToolNode_BlocksUnauthenticatedSensitiveTool(t *testing.T) {
	recorder := &recordingTool{name: "PublishTweet"}
	GetRegistry().Register(recorder)

	node := NewToolNode("publish_node", "PublishTweet")
	_, err := node.Execute(context.Background(), engine.NewBlackboard(), map[string]interface{}{
		"content": "hello",
	})
	if err == nil {
		t.Fatal("expected guardrail error for unauthenticated PublishTweet")
	}
	if recorder.called {
		t.Fatal("sensitive tool should not be executed when guardrail blocks it")
	}
}

func TestToolNode_InjectsAuthenticatedUserID(t *testing.T) {
	recorder := &recordingTool{name: "PublishTweet"}
	GetRegistry().Register(recorder)

	ctx := guardrails.InjectUserContext(context.Background(), 42)
	node := NewToolNode("publish_node", "PublishTweet")
	_, err := node.Execute(ctx, engine.NewBlackboard(), map[string]interface{}{
		"content": "hello",
		"user_id": uint64(7),
	})
	if err != nil {
		t.Fatalf("unexpected guardrail error: %v", err)
	}
	if !recorder.called {
		t.Fatal("tool should be executed after authenticated user_id injection")
	}
	if got := recorder.inputs["user_id"]; got != uint64(42) {
		t.Fatalf("expected guardrail to inject authenticated user_id 42, got %v", got)
	}
}
