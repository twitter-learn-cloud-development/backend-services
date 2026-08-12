package environment

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

type staticToolCatalog struct {
	tools []agentRuntime.ToolDefinition
	err   error
	calls int
}

func (catalog *staticToolCatalog) ListTools(context.Context) ([]agentRuntime.ToolDefinition, error) {
	catalog.calls++
	return append([]agentRuntime.ToolDefinition(nil), catalog.tools...), catalog.err
}

func TestTwitterReadEnvironmentFiltersCatalogByTaskAndPolicy(t *testing.T) {
	catalog := &staticToolCatalog{tools: []agentRuntime.ToolDefinition{
		{Name: "web_search", Category: agentRuntime.ToolCategoryRead},
		{Name: "search_users", Category: agentRuntime.ToolCategoryRead, InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "create_tweet", Category: agentRuntime.ToolCategoryWrite},
		{Name: "hybrid_search_tweets", Category: agentRuntime.ToolCategoryRead, InputSchema: json.RawMessage(`{"type":"object"}`)},
	}}
	environment, err := NewTwitterReadEnvironment(catalog)
	if err != nil {
		t.Fatalf("NewTwitterReadEnvironment() error = %v", err)
	}

	tools, err := environment.Tools(context.Background(), twitterReadTask(
		"web_search", "create_tweet", "search_users", "hybrid_search_tweets",
	))
	if err != nil {
		t.Fatalf("Tools() error = %v", err)
	}
	if len(tools) != 2 || tools[0].Name != "hybrid_search_tweets" || tools[1].Name != "search_users" {
		t.Fatalf("Tools() = %+v, want sorted task and policy intersection", tools)
	}
	tools[0].InputSchema[0] = '['
	if catalog.tools[3].InputSchema[0] != '{' {
		t.Fatal("Tools() exposed mutable catalog input schema")
	}
}

func TestTwitterReadEnvironmentFailsClosedForUnsafeClassification(t *testing.T) {
	tests := []agentRuntime.ToolDefinition{
		{Name: "hybrid_search_tweets", Category: agentRuntime.ToolCategoryWrite},
		{Name: "hybrid_search_tweets", Category: agentRuntime.ToolCategoryRead, RequiresApproval: true},
	}
	for _, definition := range tests {
		t.Run(string(definition.Category), func(t *testing.T) {
			environment, err := NewTwitterReadEnvironment(&staticToolCatalog{tools: []agentRuntime.ToolDefinition{definition}})
			if err != nil {
				t.Fatalf("NewTwitterReadEnvironment() error = %v", err)
			}
			_, err = environment.Tools(context.Background(), twitterReadTask("hybrid_search_tweets"))
			if err == nil || !strings.Contains(err.Error(), "not safely classified") {
				t.Fatalf("Tools() error = %v, want safe classification failure", err)
			}
		})
	}
}

func TestTwitterReadEnvironmentRejectsInvalidCatalogAndContext(t *testing.T) {
	if _, err := NewTwitterReadEnvironment(nil); err == nil {
		t.Fatal("NewTwitterReadEnvironment(nil) error = nil")
	}
	catalog := &staticToolCatalog{tools: []agentRuntime.ToolDefinition{
		{Name: "hybrid_search_tweets", Category: agentRuntime.ToolCategoryRead},
		{Name: "hybrid_search_tweets", Category: agentRuntime.ToolCategoryRead},
	}}
	environment, err := NewTwitterReadEnvironment(catalog)
	if err != nil {
		t.Fatalf("NewTwitterReadEnvironment() error = %v", err)
	}
	if _, err = environment.Tools(context.Background(), twitterReadTask("hybrid_search_tweets")); err == nil || !strings.Contains(err.Error(), "duplicate catalog tool") {
		t.Fatalf("Tools() error = %v, want duplicate catalog failure", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	beforeCalls := catalog.calls
	if _, err = environment.Tools(canceled, twitterReadTask("hybrid_search_tweets")); !errors.Is(err, context.Canceled) {
		t.Fatalf("Tools(canceled) error = %v, want context canceled", err)
	}
	if catalog.calls != beforeCalls {
		t.Fatal("Tools(canceled) called catalog")
	}
}

func TestTwitterReadSnapshotIsDeterministicAndLowSensitivity(t *testing.T) {
	now := time.Date(2026, 8, 9, 9, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	secret := "credential-secret-must-not-leak"
	catalog := &staticToolCatalog{tools: []agentRuntime.ToolDefinition{
		{
			Name: "search_users", Category: agentRuntime.ToolCategoryRead,
			Description: "lookup " + secret,
			InputSchema: json.RawMessage(`{"properties":{"token":{"default":"` + secret + `"}},"type":"object"}`),
		},
		{
			Name: "hybrid_search_tweets", Category: agentRuntime.ToolCategoryRead,
			Description: "search posts",
			InputSchema: json.RawMessage(`{"required":["query"],"type":"object"}`),
		},
	}}
	environment, err := NewTwitterReadEnvironment(catalog, WithTwitterReadClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewTwitterReadEnvironment() error = %v", err)
	}
	task := twitterReadTask("search_users", "hybrid_search_tweets")

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
		t.Fatalf("stable catalog identity changed across phases: before=%+v after=%+v", before, after)
	}
	if before.Environment != TwitterReadEnvironmentName || !before.CapturedAt.Equal(now.UTC()) {
		t.Fatalf("Snapshot() identity/time = %+v", before)
	}
	serialized := string(before.Metadata) + before.Reference + before.Digest + before.ID
	if strings.Contains(serialized, secret) || strings.Contains(serialized, "lookup") || strings.Contains(serialized, "properties") {
		t.Fatalf("Snapshot() leaked catalog content: %s", serialized)
	}
	if !strings.Contains(string(before.Metadata), `"phase":"before"`) || !strings.Contains(string(after.Metadata), `"phase":"after"`) {
		t.Fatalf("Snapshot() metadata did not preserve observation phase: before=%s after=%s", before.Metadata, after.Metadata)
	}
}

func TestTwitterReadSnapshotRejectsUnsupportedRequestsAndInvalidSchema(t *testing.T) {
	environment, err := NewTwitterReadEnvironment(&staticToolCatalog{tools: []agentRuntime.ToolDefinition{{
		Name: "hybrid_search_tweets", Category: agentRuntime.ToolCategoryRead, InputSchema: json.RawMessage(`{"type":`),
	}}})
	if err != nil {
		t.Fatalf("NewTwitterReadEnvironment() error = %v", err)
	}
	task := twitterReadTask("hybrid_search_tweets")

	if _, err = environment.Snapshot(context.Background(), agentRuntime.SnapshotRequest{Task: task}); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("Snapshot(invalid phase) error = %v", err)
	}
	if _, err = environment.Snapshot(context.Background(), agentRuntime.SnapshotRequest{
		Task: task, Phase: agentRuntime.SnapshotPhaseBefore, Scope: []string{"tweet:1"},
	}); err == nil || !strings.Contains(err.Error(), "does not support resource scope") {
		t.Fatalf("Snapshot(scope) error = %v", err)
	}
	if _, err = environment.Snapshot(context.Background(), agentRuntime.SnapshotRequest{
		Task: task, Phase: agentRuntime.SnapshotPhaseBefore,
	}); err == nil || !strings.Contains(err.Error(), "input schema") {
		t.Fatalf("Snapshot(invalid schema) error = %v", err)
	}
}

func TestTwitterReadToolNamesReturnsCopy(t *testing.T) {
	first := TwitterReadToolNames()
	first[0] = "mutated"
	second := TwitterReadToolNames()
	if second[0] == "mutated" || len(second) != 5 {
		t.Fatalf("TwitterReadToolNames() = %v", second)
	}
}

func twitterReadTask(tools ...string) agentRuntime.TaskSpec {
	return agentRuntime.TaskSpec{
		ID: "task-twitter-read", Goal: "Find relevant platform data.", AllowedTools: tools,
		CompletionCriteria: []agentRuntime.CompletionCriterion{{
			ID: "platform-result", Description: "A platform result was observed.", Required: true,
		}},
	}
}
