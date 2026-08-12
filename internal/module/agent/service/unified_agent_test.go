package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	externalmcp "twitter-clone/internal/module/agent/mcp/remote"
	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestRunAgentConversationUsesRuntimeChatWithoutTools(t *testing.T) {
	repo := &assistRuntimeRepository{}
	runner := &capturingRuntimeRunner{result: agentRuntime.RunResult{
		Status:      agentRuntime.RunStatusCompleted,
		FinalAnswer: "direct runtime answer",
		Steps:       []agentRuntime.Step{{Index: 1}},
	}}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		repo, nil, nil,
		WithAgentRunner(runner),
		WithRuntimeToolCatalog(staticRuntimeToolCatalog{tools: []agentRuntime.ToolDefinition{
			{Name: "hybrid_search_tweets", Category: agentRuntime.ToolCategoryRead},
			{Name: "create_tweet", Category: agentRuntime.ToolCategoryWrite},
		}}),
	)
	defer service.Close()

	result, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID:  42,
		Content: "hello, continue our conversation",
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if result.ExecutionProfile != ExecutionProfileRuntimeChat ||
		result.Response != "direct runtime answer" ||
		result.RunID == "" ||
		result.PublishableDraft {
		t.Fatalf("RunAgent() result = %+v", result)
	}
	assertCapabilityIDs(t, result.CapabilityIDs, []string{CapabilityConversationReply})
	if runner.calls != 1 ||
		runner.request.Context.Mode != agentRuntime.ModeChat ||
		runner.request.Context.AgentProfileID != profileConversationReply {
		t.Fatalf("runtime request = %+v, calls = %d", runner.request, runner.calls)
	}
	if len(runner.request.Tools) != 0 {
		t.Fatalf("runtime tools = %+v, want none", runner.request.Tools)
	}
	if len(result.ToolActivities) != 0 || len(result.Citations) != 0 {
		t.Fatalf("unexpected evidence = tools %+v citations %+v", result.ToolActivities, result.Citations)
	}
	if result.RunStatus != UnifiedAgentRunStatusCompleted ||
		result.ApprovalState.Status != AgentApprovalStatusNotRequired ||
		result.ApprovalState.RunID != result.RunID ||
		len(result.Artifacts) != 0 {
		t.Fatalf("run projection = status %q approval %+v artifacts %+v", result.RunStatus, result.ApprovalState, result.Artifacts)
	}
	if len(repo.saved) != 2 {
		t.Fatalf("saved messages = %d, want 2", len(repo.saved))
	}
}

func TestRunAgentContentDraftReturnsTraceableArtifact(t *testing.T) {
	repo := &assistRuntimeRepository{}
	runner := &capturingRuntimeRunner{result: agentRuntime.RunResult{
		Status:      agentRuntime.RunStatusCompleted,
		FinalAnswer: "three complete drafts",
		Steps:       []agentRuntime.Step{{Index: 1}},
	}}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		repo, nil, nil,
		WithAgentRunner(runner),
		WithRuntimeToolCatalog(staticRuntimeToolCatalog{tools: []agentRuntime.ToolDefinition{
			{Name: "hybrid_search_tweets", Category: agentRuntime.ToolCategoryRead},
			{Name: "create_tweet", Category: agentRuntime.ToolCategoryWrite},
		}}),
	)
	defer service.Close()

	result, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID:                 42,
		Content:                "write a tweet about Go agents",
		PreferredCapabilityIDs: []string{CapabilityContentDraft},
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if result.ExecutionProfile != ExecutionProfileRuntimeDraft ||
		result.RunStatus != UnifiedAgentRunStatusCompleted ||
		!result.PublishableDraft ||
		result.RunID == "" {
		t.Fatalf("RunAgent() result = %+v", result)
	}
	if runner.calls != 1 ||
		runner.request.Context.Mode != agentRuntime.ModeAssist ||
		runner.request.Context.AgentProfileID != profileAssistDraft {
		t.Fatalf("runtime request = %+v, calls = %d", runner.request, runner.calls)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("Artifacts = %+v, want one draft artifact", result.Artifacts)
	}
	artifact := result.Artifacts[0]
	if artifact.ArtifactID != AgentArtifactTypeContentDraft+":"+result.RunID ||
		artifact.Type != AgentArtifactTypeContentDraft ||
		artifact.Status != AgentArtifactStatusReady ||
		artifact.ContentType != AgentArtifactContentMarkdown ||
		artifact.Content != result.Response ||
		artifact.SourceRunID != result.RunID ||
		!artifact.RequiresConfirmation {
		t.Fatalf("artifact = %+v", artifact)
	}
	if result.ApprovalState.Status != AgentApprovalStatusNotRequired ||
		result.ApprovalState.RunID != result.RunID ||
		result.ApprovalState.ApprovalID != "" {
		t.Fatalf("approval state = %+v", result.ApprovalState)
	}
	if len(repo.saved) != 2 ||
		repo.saved[1].Metadata["execution_profile"] != ExecutionProfileRuntimeDraft {
		t.Fatalf("saved messages = %+v", repo.saved)
	}
}

func TestRunAgentWebSearchUsesGovernedReadToolAndCitations(t *testing.T) {
	repo := &assistRuntimeRepository{}
	runner := &capturingRuntimeRunner{result: agentRuntime.RunResult{
		Status:      agentRuntime.RunStatusCompleted,
		FinalAnswer: "Go release answer",
		Steps: []agentRuntime.Step{{
			Index: 1,
			Actions: []agentRuntime.Action{{
				ID: "web-1", Type: agentRuntime.ActionToolCall, Name: "web_search",
			}},
			Observations: []agentRuntime.Observation{{
				ActionID: "web-1", Name: "web_search", Content: "search results",
				StructuredContent: json.RawMessage(`{
					"schema":"web.search.v1",
					"provider":"brave",
					"query":"Go release",
					"items":[{"rank":1,"url":"https://go.dev/doc/devel/release","title":"Go releases","snippet":"Official history"}]
				}`),
			}},
		}},
	}}
	catalog, err := NewBuiltInAgentCapabilityCatalog(WithAvailableWebSearchCapability())
	if err != nil {
		t.Fatalf("NewBuiltInAgentCapabilityCatalog() error = %v", err)
	}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		repo, nil, nil,
		WithAgentRunner(runner),
		WithAgentCapabilityCatalog(catalog),
		WithRuntimeToolCatalog(staticRuntimeToolCatalog{tools: []agentRuntime.ToolDefinition{
			{Name: "web_search", Category: agentRuntime.ToolCategoryRead},
			{Name: "create_tweet", Category: agentRuntime.ToolCategoryWrite},
		}}),
	)
	defer service.Close()

	result, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID:                 42,
		Content:                "search the web for the latest Go release",
		PreferredCapabilityIDs: []string{CapabilityWebSearch},
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if result.ExecutionProfile != ExecutionProfileRuntimeWebSearch ||
		result.Response != "Go release answer" ||
		result.PublishableDraft {
		t.Fatalf("RunAgent() result = %+v", result)
	}
	if runner.request.Context.Mode != agentRuntime.ModeConsult ||
		runner.request.Context.AgentProfileID != profileUnifiedWebSearch ||
		len(runner.request.Tools) != 1 ||
		runner.request.Tools[0].Name != "web_search" ||
		runner.request.Tools[0].ApprovalRequired() {
		t.Fatalf("runtime request = %+v", runner.request)
	}
	if len(result.ToolActivities) != 1 ||
		result.ToolActivities[0].ResultCount != 1 ||
		len(result.Citations) != 1 ||
		result.Citations[0].URL != "https://go.dev/doc/devel/release" {
		t.Fatalf("evidence = activities %+v citations %+v", result.ToolActivities, result.Citations)
	}
	if len(result.Artifacts) != 0 || len(repo.saved) != 2 {
		t.Fatalf("artifacts/saved = %+v/%d", result.Artifacts, len(repo.saved))
	}
}

func TestRunAgentExternalMCPUsesOnlyEnabledSnapshotTools(t *testing.T) {
	const qualifiedName = "mcp_server.lookup"
	store := &externalMCPRuntimeStore{
		connection: externalmcp.Connection{
			ID: "mcpconn_1", UserID: 42, ServerID: "mcp_server",
			Status: externalmcp.ConnectionStatusActive, DiscoveryStatus: externalmcp.DiscoveryStatusReady,
			ActiveSnapshotID: "mcpsnap_1",
			ToolPolicies: []externalmcp.ToolPolicy{{
				SnapshotID: "mcpsnap_1", ToolName: "lookup", QualifiedName: qualifiedName,
				Category: externalmcp.ToolCategoryRead, Enabled: true,
			}},
		},
		snapshot: externalmcp.ToolSchemaSnapshot{
			ID: "mcpsnap_1", ConnectionID: "mcpconn_1", UserID: 42, ServerID: "mcp_server",
			Tools: []externalmcp.ToolSchema{{
				Name: "lookup", QualifiedName: qualifiedName, Description: "look up public documentation",
				InputSchemaJSON:  `{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`,
				DeclaredReadOnly: true,
			}},
		},
	}
	manager := externalmcp.NewManager(
		store,
		nil,
		nil,
		nil,
		externalmcp.WithEnabled(true),
		externalmcp.WithCaller(&externalMCPRuntimeCaller{}),
	)
	repo := &assistRuntimeRepository{}
	runner := &capturingRuntimeRunner{result: agentRuntime.RunResult{
		Status:      agentRuntime.RunStatusCompleted,
		FinalAnswer: "external tool answer",
		Steps: []agentRuntime.Step{{
			Index: 1,
			Actions: []agentRuntime.Action{{
				ID: "external-1", Type: agentRuntime.ActionToolCall, Name: qualifiedName,
			}},
			Observations: []agentRuntime.Observation{{
				ActionID: "external-1", Name: qualifiedName, Content: "verified external result",
			}},
		}},
	}}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		repo, nil, nil,
		WithAgentRunner(runner),
		WithExternalMCPManager(manager),
		WithExternalMCPEnabled(true),
	)
	defer service.Close()

	result, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID:                 42,
		Content:                "use my approved documentation connector",
		PreferredCapabilityIDs: []string{CapabilityExternalMCP},
	})

	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if result.ExecutionProfile != ExecutionProfileRuntimeExternalMCP ||
		result.Response != "external tool answer" ||
		result.PublishableDraft {
		t.Fatalf("RunAgent() result = %+v", result)
	}
	assertCapabilityIDs(t, result.CapabilityIDs, []string{CapabilityExternalMCP})
	if runner.calls != 1 ||
		runner.request.Context.AgentProfileID != profileUnifiedExternalMCP ||
		len(runner.request.Tools) != 1 ||
		runner.request.Tools[0].Name != qualifiedName ||
		runner.request.Tools[0].ApprovalRequired() {
		t.Fatalf("runtime request = %+v, calls = %d", runner.request, runner.calls)
	}
	if len(result.ToolActivities) != 1 ||
		result.ToolActivities[0].ToolName != qualifiedName ||
		result.ToolActivities[0].Status != AgentToolActivitySucceeded ||
		len(result.Citations) != 0 || len(result.Artifacts) != 0 {
		t.Fatalf("execution evidence = activities %+v citations %+v artifacts %+v", result.ToolActivities, result.Citations, result.Artifacts)
	}
	if len(repo.saved) != 2 ||
		repo.saved[1].Metadata["execution_profile"] != ExecutionProfileRuntimeExternalMCP {
		t.Fatalf("saved messages = %+v", repo.saved)
	}
}

func TestRunAgentPlatformSearchUsesGovernedRuntimeRun(t *testing.T) {
	repo := &assistRuntimeRepository{}
	runner := &capturingRuntimeRunner{result: agentRuntime.RunResult{
		Status:      agentRuntime.RunStatusCompleted,
		FinalAnswer: "grounded platform search answer",
		Steps: []agentRuntime.Step{{
			Index: 1,
			Actions: []agentRuntime.Action{{
				ID: "search-1", Type: agentRuntime.ActionToolCall, Name: "hybrid_search_tweets",
			}},
			Observations: []agentRuntime.Observation{{
				ActionID: "search-1", Name: "hybrid_search_tweets",
				StructuredContent: json.RawMessage(`{"schema":"platform.tweet_search.v1","items":[{"tweet_id":"9007199254740993","content":"verified platform evidence"}]}`),
			}},
		}},
	}}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		repo, nil, nil,
		WithAgentRunner(runner),
		WithRuntimeToolCatalog(staticRuntimeToolCatalog{tools: []agentRuntime.ToolDefinition{{
			Name: "hybrid_search_tweets", Category: agentRuntime.ToolCategoryRead,
		}}}),
	)
	defer service.Close()

	result, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID:                 42,
		Content:                "search platform posts about cloud native",
		PreferredCapabilityIDs: []string{CapabilityPlatformSearch},
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if result.ExecutionProfile != ExecutionProfileRuntimePlatformSearch ||
		result.PublishableDraft {
		t.Fatalf("RunAgent() result = %+v", result)
	}
	assertCapabilityIDs(t, result.CapabilityIDs, []string{CapabilityPlatformSearch})
	if runner.calls != 1 ||
		runner.request.Context.AgentProfileID != profileUnifiedPlatformSearch ||
		runner.request.Context.Mode != agentRuntime.ModeConsult {
		t.Fatalf("runtime request = calls %d context %+v", runner.calls, runner.request.Context)
	}
	if len(runner.request.Tools) != 1 ||
		runner.request.Tools[0].Name != "hybrid_search_tweets" {
		t.Fatalf("runtime tools = %+v", runner.request.Tools)
	}
	if len(result.Citations) != 1 ||
		result.Citations[0].URL != "/tweets/9007199254740993" ||
		result.Citations[0].Snippet != "verified platform evidence" {
		t.Fatalf("citations = %+v", result.Citations)
	}
}

func TestRunAgentContextualFollowUpKeepsPlatformSearchCapability(t *testing.T) {
	existingDialogue := &repository.Dialogue{
		ID: primitive.NewObjectID(), UserID: 42, Mode: repository.ModeConsult,
	}
	repo := &assistRuntimeRepository{
		dialogue: existingDialogue,
		recent: []*repository.DialogueMessage{{
			Role: repository.RoleAssistant,
			Metadata: map[string]any{
				"capability_ids": []any{CapabilityPlatformSearch},
			},
		}},
	}
	runner := &capturingRuntimeRunner{result: agentRuntime.RunResult{
		Status:      agentRuntime.RunStatusCompleted,
		FinalAnswer: "more grounded platform detail",
		Steps: []agentRuntime.Step{{
			Actions: []agentRuntime.Action{{
				ID: "search-1", Type: agentRuntime.ActionToolCall, Name: "hybrid_search_tweets",
			}},
			Observations: []agentRuntime.Observation{{
				ActionID: "search-1", Name: "hybrid_search_tweets",
				StructuredContent: json.RawMessage(`{"schema":"platform.tweet_search.v1","items":[{"tweet_id":"9007199254740994","content":"grounded follow-up evidence"}]}`),
			}},
		}},
	}}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		repo, nil, nil,
		WithAgentRunner(runner),
		WithRuntimeToolCatalog(staticRuntimeToolCatalog{tools: []agentRuntime.ToolDefinition{{
			Name: "hybrid_search_tweets", Category: agentRuntime.ToolCategoryRead,
		}}}),
	)
	defer service.Close()

	result, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID:      42,
		DialogueKey: existingDialogue.ID.Hex(),
		Content:     "\u80fd\u5426\u7ed9\u6211\u8be6\u7ec6\u5185\u5bb9\u5462",
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if result.ExecutionProfile != ExecutionProfileRuntimePlatformSearch {
		t.Fatalf("ExecutionProfile = %q", result.ExecutionProfile)
	}
	if result.DialogueID != existingDialogue.ID.Hex() || repo.created != 0 {
		t.Fatalf("dialogue = %q created = %d", result.DialogueID, repo.created)
	}
	assertCapabilityIDs(t, result.CapabilityIDs, []string{CapabilityPlatformSearch})
	if runner.request.Context.AgentProfileID != profileUnifiedPlatformSearch ||
		len(runner.request.Tools) != 1 ||
		runner.request.Tools[0].Name != "hybrid_search_tweets" {
		t.Fatalf("runtime request = context %+v tools %+v", runner.request.Context, runner.request.Tools)
	}
}

func TestRunAgentResearchDraftUsesOneGovernedRuntimeRun(t *testing.T) {
	repo := &assistRuntimeRepository{}
	runner := &capturingRuntimeRunner{result: agentRuntime.RunResult{
		Status:      agentRuntime.RunStatusCompleted,
		FinalAnswer: "evidence summary and three complete drafts",
		Steps: []agentRuntime.Step{{
			Index: 1,
			Actions: []agentRuntime.Action{{
				ID: "search-1", Type: agentRuntime.ActionToolCall, Name: "hybrid_search_tweets",
			}},
			Observations: []agentRuntime.Observation{{
				ActionID: "search-1", Name: "hybrid_search_tweets", Content: "search results",
				StructuredContent: json.RawMessage(`{
					"schema":"platform.tweet_search.v1",
					"items":[{"tweet_id":"9007199254740993","content":"  verified platform evidence  "}]
				}`),
			}},
		}},
	}}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		repo, nil, nil,
		WithAgentRunner(runner),
		WithRuntimeToolCatalog(staticRuntimeToolCatalog{tools: []agentRuntime.ToolDefinition{
			{Name: "hybrid_search_tweets", Category: agentRuntime.ToolCategoryRead},
			{Name: "create_tweet", Category: agentRuntime.ToolCategoryWrite},
		}}),
	)
	defer service.Close()

	result, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID:                 42,
		Content:                "搜索 Go Agent 的站内资料并帮我写一条推文",
		PreferredCapabilityIDs: []string{CapabilityPlatformSearch, CapabilityContentDraft},
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("runtime calls = %d, want 1", runner.calls)
	}
	if result.ExecutionProfile != ExecutionProfileRuntimeResearchDraft || !result.PublishableDraft {
		t.Fatalf("RunAgent() result = %+v", result)
	}
	if result.RunStatus != UnifiedAgentRunStatusCompleted ||
		result.ApprovalState.Status != AgentApprovalStatusNotRequired ||
		result.ApprovalState.RunID != result.RunID ||
		len(result.Artifacts) != 1 ||
		result.Artifacts[0].SourceRunID != result.RunID {
		t.Fatalf("run projection = status %q approval %+v artifacts %+v", result.RunStatus, result.ApprovalState, result.Artifacts)
	}
	assertCapabilityIDs(
		t,
		result.CapabilityIDs,
		[]string{CapabilityPlatformSearch, CapabilityContentDraft},
	)
	if len(result.ToolActivities) != 1 ||
		result.ToolActivities[0].ToolName != "hybrid_search_tweets" ||
		result.ToolActivities[0].Status != AgentToolActivitySucceeded ||
		result.ToolActivities[0].ResultCount != 1 {
		t.Fatalf("tool activities = %+v", result.ToolActivities)
	}
	if len(result.Citations) != 1 ||
		result.Citations[0].SourceID != "9007199254740993" ||
		result.Citations[0].URL != "/tweets/9007199254740993" ||
		result.Citations[0].Snippet != "verified platform evidence" {
		t.Fatalf("citations = %+v", result.Citations)
	}
	if runner.request.Context.AgentProfileID != profileUnifiedResearchDraft ||
		runner.request.Context.Mode != agentRuntime.ModeAssist {
		t.Fatalf("runtime context = %+v", runner.request.Context)
	}
	if len(runner.request.Tools) != 1 ||
		runner.request.Tools[0].Name != "hybrid_search_tweets" ||
		runner.request.Tools[0].ApprovalRequired() {
		t.Fatalf("runtime tools = %+v", runner.request.Tools)
	}
	if !strings.Contains(runner.request.Messages[0].Content, "user_id: 42") {
		t.Fatalf("profile user identity was not materialized: %q", runner.request.Messages[0].Content)
	}
	if len(repo.saved) != 2 {
		t.Fatalf("saved messages = %d, want one user/assistant pair", len(repo.saved))
	}
	if repo.saved[1].Metadata["execution_profile"] != ExecutionProfileRuntimeResearchDraft {
		t.Fatalf("assistant metadata = %+v", repo.saved[1].Metadata)
	}
	if repo.saved[1].Metadata["tool_activity_count"] != 1 ||
		repo.saved[1].Metadata["citation_count"] != 1 {
		t.Fatalf("assistant evidence metadata = %+v", repo.saved[1].Metadata)
	}
	metadataCapabilities, ok := repo.saved[1].Metadata["capability_ids"].([]string)
	if !ok {
		t.Fatalf("metadata capability_ids = %#v", repo.saved[1].Metadata["capability_ids"])
	}
	assertCapabilityIDs(
		t,
		metadataCapabilities,
		[]string{CapabilityPlatformSearch, CapabilityContentDraft},
	)
}

func TestRunAgentResearchDraftFailsClosedWithoutSearchEvidence(t *testing.T) {
	repo := &assistRuntimeRepository{}
	runner := &capturingRuntimeRunner{result: agentRuntime.RunResult{
		Status:      agentRuntime.RunStatusCompleted,
		FinalAnswer: "unsupported draft",
	}}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		repo, nil, nil,
		WithAgentRunner(runner),
		WithRuntimeToolCatalog(staticRuntimeToolCatalog{tools: []agentRuntime.ToolDefinition{{
			Name: "hybrid_search_tweets", Category: agentRuntime.ToolCategoryRead,
		}}}),
	)
	defer service.Close()

	_, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID:                 42,
		Content:                "搜索 Go Agent 的资料并帮我写一条推文",
		PreferredCapabilityIDs: []string{CapabilityPlatformSearch, CapabilityContentDraft},
	})
	if !errors.Is(err, ErrRequiredCapabilityEvidence) {
		t.Fatalf("RunAgent() error = %v, want ErrRequiredCapabilityEvidence", err)
	}
	if len(repo.saved) != 0 {
		t.Fatalf("saved messages = %d, want 0", len(repo.saved))
	}
}

func TestRunAgentResearchDraftReusesExistingDialogueAcrossCapabilities(t *testing.T) {
	existingDialogue := &repository.Dialogue{
		ID: primitive.NewObjectID(), UserID: 42, Mode: repository.ModeChat,
	}
	repo := &assistRuntimeRepository{dialogue: existingDialogue}
	runner := &capturingRuntimeRunner{result: agentRuntime.RunResult{
		Status:      agentRuntime.RunStatusCompleted,
		FinalAnswer: "draft based on platform evidence",
		Steps: []agentRuntime.Step{{
			Actions: []agentRuntime.Action{{
				ID: "search-1", Type: agentRuntime.ActionToolCall, Name: "hybrid_search_tweets",
			}},
			Observations: []agentRuntime.Observation{{
				ActionID: "search-1", Name: "hybrid_search_tweets", Content: `[{"id":"tweet-1"}]`,
			}},
		}},
	}}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		repo, nil, nil,
		WithAgentRunner(runner),
		WithRuntimeToolCatalog(staticRuntimeToolCatalog{tools: []agentRuntime.ToolDefinition{{
			Name: "hybrid_search_tweets", Category: agentRuntime.ToolCategoryRead,
		}}}),
	)
	defer service.Close()

	result, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID:                 42,
		DialogueKey:            existingDialogue.ID.Hex(),
		Content:                "搜索资料并帮我写一条推文",
		PreferredCapabilityIDs: []string{CapabilityPlatformSearch, CapabilityContentDraft},
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if result.DialogueID != existingDialogue.ID.Hex() {
		t.Fatalf("DialogueID = %q, want %q", result.DialogueID, existingDialogue.ID.Hex())
	}
	if repo.created != 0 {
		t.Fatalf("CreateDialogue() calls = %d, want 0", repo.created)
	}
	for _, message := range repo.saved {
		if message.DialogueID != existingDialogue.ID {
			t.Fatalf("saved message dialogue = %s, want %s", message.DialogueID.Hex(), existingDialogue.ID.Hex())
		}
	}
}

func TestRunAgentResearchDraftDoesNotReturnUnpersistedAnswer(t *testing.T) {
	repo := &assistRuntimeRepository{saveErr: errors.New("mongo unavailable")}
	runner := &capturingRuntimeRunner{result: agentRuntime.RunResult{
		Status:      agentRuntime.RunStatusCompleted,
		FinalAnswer: "evidence summary and drafts",
		Steps: []agentRuntime.Step{{
			Actions: []agentRuntime.Action{{
				ID: "search-1", Type: agentRuntime.ActionToolCall, Name: "hybrid_search_tweets",
			}},
			Observations: []agentRuntime.Observation{{
				ActionID: "search-1", Name: "hybrid_search_tweets", Content: `[]`,
			}},
		}},
	}}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		repo, nil, nil,
		WithAgentRunner(runner),
		WithRuntimeToolCatalog(staticRuntimeToolCatalog{tools: []agentRuntime.ToolDefinition{{
			Name: "hybrid_search_tweets", Category: agentRuntime.ToolCategoryRead,
		}}}),
	)
	defer service.Close()

	result, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID:                 42,
		Content:                "搜索资料并帮我写一条推文",
		PreferredCapabilityIDs: []string{CapabilityPlatformSearch, CapabilityContentDraft},
	})
	if err == nil || !strings.Contains(err.Error(), "persist research draft conversation failed") {
		t.Fatalf("RunAgent() result/error = %+v/%v, want persistence failure", result, err)
	}
	if result != nil {
		t.Fatalf("RunAgent() result = %+v, want nil for unpersisted answer", result)
	}
}

func assertCapabilityIDs(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("CapabilityIDs = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("CapabilityIDs = %#v, want %#v", got, want)
		}
	}
}
