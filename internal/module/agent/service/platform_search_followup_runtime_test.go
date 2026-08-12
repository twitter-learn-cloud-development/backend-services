package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestE2E06PlatformSearchFollowUpReadsSelectedPriorResult(t *testing.T) {
	dialogue := &repository.Dialogue{
		ID: primitive.NewObjectID(), UserID: 42, Mode: repository.ModeConsult,
	}
	repo := &assistRuntimeRepository{
		dialogue: dialogue,
		recent: []*repository.DialogueMessage{{
			Role: repository.RoleAssistant,
			Metadata: map[string]any{
				"capability_ids": []any{CapabilityPlatformSearch},
				platformTweetReferencesMetadataKey: []any{
					"/tweets/9007199254740993",
					"/tweets/9007199254740994",
				},
			},
		}},
	}
	runner := &capturingRuntimeRunner{result: agentRuntime.RunResult{
		Status: agentRuntime.RunStatusCompleted, FinalAnswer: "authoritative second tweet content",
		Steps: []agentRuntime.Step{{
			Actions: []agentRuntime.Action{{
				ID: "detail-1", Type: agentRuntime.ActionToolCall, Name: "get_tweets_by_ids",
				Arguments: json.RawMessage(`{"tweet_ids":"9007199254740994"}`),
			}},
			Observations: []agentRuntime.Observation{{
				ActionID: "detail-1", Name: "get_tweets_by_ids",
				StructuredContent: json.RawMessage(`{"schema":"platform.tweet_detail.v1","items":[{"tweet_id":"9007199254740994","content":"authoritative second tweet content"}]}`),
			}},
		}},
	}}
	service := newPlatformFollowUpTestService(repo, runner)
	defer service.Close()

	result, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, DialogueKey: dialogue.ID.Hex(), Content: "第二条的详细内容呢",
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if runner.calls != 1 || len(runner.request.Tools) != 1 ||
		runner.request.Tools[0].Name != "get_tweets_by_ids" {
		t.Fatalf("runtime calls = %d tools = %+v", runner.calls, runner.request.Tools)
	}
	if len(runner.request.Messages) == 0 ||
		!strings.Contains(runner.request.Messages[0].Content, `tweet_ids "9007199254740994"`) {
		t.Fatalf("trusted detail binding is missing from system prompt: %+v", runner.request.Messages)
	}
	if len(result.Citations) != 1 || result.Citations[0].URL != "/tweets/9007199254740994" ||
		result.Citations[0].Snippet != "authoritative second tweet content" {
		t.Fatalf("citations = %+v", result.Citations)
	}
	if len(repo.saved) != 2 {
		t.Fatalf("saved messages = %d, want 2", len(repo.saved))
	}
	refs := metadataStringSlice(repo.saved[1].Metadata, platformTweetReferencesMetadataKey)
	if len(refs) != 1 || refs[0] != "/tweets/9007199254740994" {
		t.Fatalf("persisted references = %+v", refs)
	}
}

func TestE2E06PlatformSearchFollowUpRejectsAmbiguousSelection(t *testing.T) {
	dialogue, repo := platformFollowUpRepository([]any{
		"/tweets/9007199254740993", "/tweets/9007199254740994",
	})
	runner := &capturingRuntimeRunner{}
	service := newPlatformFollowUpTestService(repo, runner)
	defer service.Close()

	_, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, DialogueKey: dialogue.ID.Hex(), Content: "能否给我详细内容呢",
	})
	if !errors.Is(err, ErrPlatformTweetReferenceAmbiguous) {
		t.Fatalf("RunAgent() error = %v, want ErrPlatformTweetReferenceAmbiguous", err)
	}
	if runner.calls != 0 || len(repo.saved) != 0 {
		t.Fatalf("ambiguous follow-up executed runtime or persisted output: calls=%d saved=%d", runner.calls, len(repo.saved))
	}
}

func TestE2E06PlatformSearchFollowUpRejectsForgedTweetID(t *testing.T) {
	dialogue, repo := platformFollowUpRepository([]any{"/tweets/9007199254740993"})
	runner := &capturingRuntimeRunner{}
	service := newPlatformFollowUpTestService(repo, runner)
	defer service.Close()

	_, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, DialogueKey: dialogue.ID.Hex(),
		Content: "请给我推文 9007199254740999 的详细内容",
	})
	if !errors.Is(err, ErrPlatformTweetReferenceUntrusted) {
		t.Fatalf("RunAgent() error = %v, want ErrPlatformTweetReferenceUntrusted", err)
	}
	if runner.calls != 0 {
		t.Fatalf("forged reference reached runtime: calls=%d", runner.calls)
	}
}

func TestE2E06PlatformSearchFollowUpRejectsTextOnlyDetailClaim(t *testing.T) {
	dialogue, repo := platformFollowUpRepository([]any{"/tweets/9007199254740993"})
	runner := &capturingRuntimeRunner{result: agentRuntime.RunResult{
		Status: agentRuntime.RunStatusCompleted, FinalAnswer: "claimed full content",
		Steps: []agentRuntime.Step{{
			Actions: []agentRuntime.Action{{
				ID: "detail-1", Type: agentRuntime.ActionToolCall, Name: "get_tweets_by_ids",
				Arguments: json.RawMessage(`{"tweet_ids":"9007199254740993"}`),
			}},
			Observations: []agentRuntime.Observation{{
				ActionID: "detail-1", Name: "get_tweets_by_ids", Content: "untrusted display text only",
			}},
		}},
	}}
	service := newPlatformFollowUpTestService(repo, runner)
	defer service.Close()

	_, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, DialogueKey: dialogue.ID.Hex(), Content: "请给我详细内容",
	})
	if !errors.Is(err, ErrRequiredCapabilityEvidence) {
		t.Fatalf("RunAgent() error = %v, want ErrRequiredCapabilityEvidence", err)
	}
	if len(repo.saved) != 0 {
		t.Fatalf("text-only claim was persisted: %+v", repo.saved)
	}
}

func TestPlatformSearchPersistsTrustedResultReferences(t *testing.T) {
	repo := &assistRuntimeRepository{}
	runner := &capturingRuntimeRunner{result: agentRuntime.RunResult{
		Status: agentRuntime.RunStatusCompleted, FinalAnswer: "grounded result",
		Steps: []agentRuntime.Step{{
			Actions: []agentRuntime.Action{{
				ID: "search-1", Type: agentRuntime.ActionToolCall, Name: "hybrid_search_tweets",
			}},
			Observations: []agentRuntime.Observation{{
				ActionID: "search-1", Name: "hybrid_search_tweets",
				StructuredContent: json.RawMessage(`{"schema":"platform.tweet_search.v1","items":[{"tweet_id":"9007199254740993","content":"grounded"}]}`),
			}},
		}},
	}}
	service := newPlatformFollowUpTestService(repo, runner)
	defer service.Close()

	_, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, Content: "search platform posts",
		PreferredCapabilityIDs: []string{CapabilityPlatformSearch},
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	refs := metadataStringSlice(repo.saved[1].Metadata, platformTweetReferencesMetadataKey)
	if len(refs) != 1 || refs[0] != "/tweets/9007199254740993" {
		t.Fatalf("persisted references = %+v", refs)
	}
}

func newPlatformFollowUpTestService(
	repo *assistRuntimeRepository,
	runner *capturingRuntimeRunner,
) *AgentService {
	return NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		repo, nil, nil,
		WithAgentRunner(runner),
		WithRuntimeToolCatalog(staticRuntimeToolCatalog{tools: []agentRuntime.ToolDefinition{
			{Name: "hybrid_search_tweets", Category: agentRuntime.ToolCategoryRead},
			{Name: "get_tweets_by_ids", Category: agentRuntime.ToolCategoryRead},
		}}),
	)
}

func platformFollowUpRepository(references []any) (*repository.Dialogue, *assistRuntimeRepository) {
	dialogue := &repository.Dialogue{
		ID: primitive.NewObjectID(), UserID: 42, Mode: repository.ModeConsult,
	}
	return dialogue, &assistRuntimeRepository{
		dialogue: dialogue,
		recent: []*repository.DialogueMessage{{
			Role: repository.RoleAssistant,
			Metadata: map[string]any{
				"capability_ids":                   []any{CapabilityPlatformSearch},
				platformTweetReferencesMetadataKey: references,
			},
		}},
	}
}
