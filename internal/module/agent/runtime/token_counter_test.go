package runtime

import "testing"

func TestHeuristicTokenCounterCountsCJKConservatively(t *testing.T) {
	counter := NewHeuristicTokenCounter()
	if got := counter.CountText("abcd"); got != 1 {
		t.Fatalf("CountText(ascii) = %d, want 1", got)
	}
	if got := counter.CountText("中文测试"); got != 4 {
		t.Fatalf("CountText(CJK) = %d, want 4", got)
	}
}

func TestHeuristicTokenCounterIncludesToolsAndActions(t *testing.T) {
	counter := NewHeuristicTokenCounter()
	plain := counter.EstimateRequest(ModelRequest{Messages: []Message{{Role: RoleUser, Content: "hello"}}})
	withTool := counter.EstimateRequest(ModelRequest{
		Messages: []Message{{
			Role:    RoleAssistant,
			Actions: []Action{{ID: "call", Type: ActionToolCall, Name: "search"}},
		}},
		Tools: []ToolDefinition{{Name: "search", Description: "search content"}},
	})
	if !plain.Estimated || withTool.InputTokens <= plain.InputTokens {
		t.Fatalf("request estimates = plain %+v with tool %+v", plain, withTool)
	}
}
