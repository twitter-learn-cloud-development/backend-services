package environment

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func TestWebReadEnvironmentFiltersByRegistrationTaskAndPolicy(t *testing.T) {
	catalog := &staticToolCatalog{tools: []agentRuntime.ToolDefinition{
		{Name: "hybrid_search_tweets", Category: agentRuntime.ToolCategoryRead},
		{Name: "web_search", Category: agentRuntime.ToolCategoryRead, InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "create_tweet", Category: agentRuntime.ToolCategoryWrite},
		{Name: "page_read", Category: agentRuntime.ToolCategoryRead, InputSchema: json.RawMessage(`{"type":"object"}`)},
	}}
	environment, err := NewWebReadEnvironment(catalog)
	if err != nil {
		t.Fatalf("NewWebReadEnvironment() error = %v", err)
	}

	tools, err := environment.Tools(context.Background(), webReadTask(
		"hybrid_search_tweets", "create_tweet", "web_search", "page_read",
	))
	if err != nil {
		t.Fatalf("Tools() error = %v", err)
	}
	if len(tools) != 2 || tools[0].Name != "page_read" || tools[1].Name != "web_search" {
		t.Fatalf("Tools() = %+v, want sorted registered task and policy intersection", tools)
	}
	tools[0].InputSchema[0] = '['
	if catalog.tools[3].InputSchema[0] != '{' {
		t.Fatal("Tools() exposed mutable catalog input schema")
	}
}

func TestWebReadEnvironmentReflectsProviderRegistration(t *testing.T) {
	environment, err := NewWebReadEnvironment(&staticToolCatalog{tools: []agentRuntime.ToolDefinition{{
		Name: "page_read", Category: agentRuntime.ToolCategoryRead,
	}}})
	if err != nil {
		t.Fatalf("NewWebReadEnvironment() error = %v", err)
	}
	tools, err := environment.Tools(context.Background(), webReadTask("web_search", "page_read"))
	if err != nil {
		t.Fatalf("Tools() error = %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "page_read" {
		t.Fatalf("Tools() = %+v, want only the registered page reader", tools)
	}
}

func TestWebReadEnvironmentFailsClosedForUnsafeCatalog(t *testing.T) {
	tests := []agentRuntime.ToolDefinition{
		{Name: "web_search", Category: agentRuntime.ToolCategoryRisky},
		{Name: "page_read", Category: agentRuntime.ToolCategoryRead, RequiresApproval: true},
		{Name: "web_search", Category: agentRuntime.ToolCategoryRead, InputSchema: json.RawMessage(`{"type":`)},
	}
	for _, definition := range tests {
		t.Run(definition.Name+string(definition.Category), func(t *testing.T) {
			environment, err := NewWebReadEnvironment(&staticToolCatalog{tools: []agentRuntime.ToolDefinition{definition}})
			if err != nil {
				t.Fatalf("NewWebReadEnvironment() error = %v", err)
			}
			_, err = environment.Tools(context.Background(), webReadTask(definition.Name))
			if err == nil {
				t.Fatal("Tools() error = nil, want unsafe catalog rejection")
			}
		})
	}
}

func TestWebReadSnapshotIsDeterministicAndLowSensitivity(t *testing.T) {
	now := time.Date(2026, 8, 9, 11, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	secret := "web-provider-secret-must-not-leak"
	catalog := &staticToolCatalog{tools: []agentRuntime.ToolDefinition{
		{
			Name: "web_search", Category: agentRuntime.ToolCategoryRead,
			Description: "search using " + secret,
			InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"default":"` + secret + `"}}}`),
		},
		{Name: "page_read", Category: agentRuntime.ToolCategoryRead, InputSchema: json.RawMessage(`{"type":"object"}`)},
	}}
	environment, err := NewWebReadEnvironment(catalog, WithWebReadClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewWebReadEnvironment() error = %v", err)
	}
	task := webReadTask("web_search", "page_read")

	before, err := environment.Snapshot(context.Background(), agentRuntime.SnapshotRequest{
		Task: task, Phase: agentRuntime.SnapshotPhaseBefore,
	})
	if err != nil {
		t.Fatalf("Snapshot(before) error = %v", err)
	}
	catalog.tools[0].Description = "changed " + secret
	catalog.tools[0].InputSchema = json.RawMessage(`{"type":"object","required":["different-secret-field"]}`)
	after, err := environment.Snapshot(context.Background(), agentRuntime.SnapshotRequest{
		Task: task, Phase: agentRuntime.SnapshotPhaseAfter,
	})
	if err != nil {
		t.Fatalf("Snapshot(after) error = %v", err)
	}
	if before.Digest != after.Digest || before.Reference != after.Reference || before.ID != after.ID {
		t.Fatalf("stable web catalog identity changed with descriptions or schemas: before=%+v after=%+v", before, after)
	}
	if before.Environment != WebReadEnvironmentName || !before.CapturedAt.Equal(now.UTC()) {
		t.Fatalf("Snapshot() identity/time = %+v", before)
	}
	serialized := string(before.Metadata) + before.Reference + before.Digest + before.ID
	if strings.Contains(serialized, secret) || strings.Contains(serialized, "query") || strings.Contains(serialized, "properties") {
		t.Fatalf("Snapshot() leaked catalog content: %s", serialized)
	}
	if !strings.Contains(string(before.Metadata), `"phase":"before"`) || !strings.Contains(string(after.Metadata), `"phase":"after"`) {
		t.Fatalf("Snapshot() metadata did not preserve phase: before=%s after=%s", before.Metadata, after.Metadata)
	}
}

func TestWebReadEnvironmentRejectsInvalidConstructionAndSnapshotScope(t *testing.T) {
	if _, err := NewWebReadEnvironment(nil); err == nil {
		t.Fatal("NewWebReadEnvironment(nil) error = nil")
	}
	if _, err := NewWebReadEnvironment(&staticToolCatalog{}, WithWebReadClock(nil)); err == nil {
		t.Fatal("NewWebReadEnvironment(nil clock) error = nil")
	}
	environment, err := NewWebReadEnvironment(&staticToolCatalog{})
	if err != nil {
		t.Fatalf("NewWebReadEnvironment() error = %v", err)
	}
	if _, err = environment.Snapshot(context.Background(), agentRuntime.SnapshotRequest{
		Task: webReadTask(), Phase: agentRuntime.SnapshotPhaseBefore, Scope: []string{"https://example.com"},
	}); err == nil || !strings.Contains(err.Error(), "does not support resource scope") {
		t.Fatalf("Snapshot(scope) error = %v", err)
	}
}

func TestWebReadToolNamesReturnsCopy(t *testing.T) {
	first := WebReadToolNames()
	first[0] = "mutated"
	second := WebReadToolNames()
	if len(second) != 2 || second[0] != "page_read" || second[1] != "web_search" {
		t.Fatalf("WebReadToolNames() = %v", second)
	}
}

func webReadTask(tools ...string) agentRuntime.TaskSpec {
	return agentRuntime.TaskSpec{
		ID: "task-web-read", Goal: "Find relevant public web evidence.", AllowedTools: tools,
		CompletionCriteria: []agentRuntime.CompletionCriterion{{
			ID: "web-result", Description: "A public web result was observed.", Required: true,
		}},
	}
}
