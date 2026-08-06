package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"twitter-clone/internal/module/agent/profile"
	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	"twitter-clone/internal/module/agent/workflow/guardrails"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type capturingRuntimeRunner struct {
	request agentRuntime.RunRequest
	result  agentRuntime.RunResult
	err     error
	calls   int
}

func (r *capturingRuntimeRunner) Run(_ context.Context, request agentRuntime.RunRequest) (agentRuntime.RunResult, error) {
	r.calls++
	r.request = request
	result := r.result
	result.Context = request.Context
	return result, r.err
}

type staticRuntimeToolCatalog struct {
	tools []agentRuntime.ToolDefinition
	err   error
}

func (c staticRuntimeToolCatalog) ListTools(context.Context) ([]agentRuntime.ToolDefinition, error) {
	return append([]agentRuntime.ToolDefinition(nil), c.tools...), c.err
}

type assistRuntimeRepository struct {
	repository.AgentRepository
	dialogue *repository.Dialogue
	recent   []*repository.DialogueMessage
	saved    []*repository.DialogueMessage
	saveErr  error
	touched  bool
	created  int
}

func (r *assistRuntimeRepository) CreateDialogue(_ context.Context, userID uint64, title string, mode repository.DialogueMode) (*repository.Dialogue, error) {
	r.created++
	dialogue := &repository.Dialogue{
		ID: primitive.NewObjectID(), UserID: userID, Title: title, Mode: mode, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	r.dialogue = dialogue
	return dialogue, nil
}

func (r *assistRuntimeRepository) GetDialogue(_ context.Context, id primitive.ObjectID) (*repository.Dialogue, error) {
	if r.dialogue != nil && r.dialogue.ID == id {
		return r.dialogue, nil
	}
	return nil, errors.New("dialogue not found")
}

func (r *assistRuntimeRepository) GetRecentMessages(context.Context, primitive.ObjectID, int) ([]*repository.DialogueMessage, error) {
	return r.recent, nil
}

func (r *assistRuntimeRepository) SaveMessages(_ context.Context, messages []*repository.DialogueMessage) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	r.saved = append(r.saved, messages...)
	return nil
}

func (r *assistRuntimeRepository) SaveMessage(_ context.Context, message *repository.DialogueMessage) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	r.saved = append(r.saved, message)
	return nil
}

func (r *assistRuntimeRepository) TouchDialogue(context.Context, primitive.ObjectID) error {
	r.touched = true
	return nil
}

func TestCallApiOfAiRuntimeUsesToolFreeProfileAndHistory(t *testing.T) {
	rollout, err := agentRuntime.ParseRollout("chat")
	if err != nil {
		t.Fatalf("ParseRollout() error = %v", err)
	}
	repo := &assistRuntimeRepository{
		recent: []*repository.DialogueMessage{
			{Role: repository.RoleUser, Content: "earlier question"},
			{Role: repository.RoleAssistant, Content: "earlier answer"},
		},
	}
	runner := &capturingRuntimeRunner{result: agentRuntime.RunResult{
		Status: agentRuntime.RunStatusCompleted, FinalAnswer: "runtime reply",
		Steps: []agentRuntime.Step{{Index: 1}},
	}}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		repo, nil, nil,
		WithRuntimeRollout(rollout),
		WithAgentRunner(runner),
		WithRuntimeToolCatalog(staticRuntimeToolCatalog{tools: []agentRuntime.ToolDefinition{
			{Name: "hybrid_search_tweets", Category: agentRuntime.ToolCategoryRead},
			{Name: "create_tweet", Category: agentRuntime.ToolCategoryWrite},
		}}),
	)
	defer service.Close()

	result, err := service.CallApiOfAi(context.Background(), 42, 0, "", "continue")
	if err != nil {
		t.Fatalf("CallApiOfAi() error = %v", err)
	}
	if result.Response != "runtime reply" || result.DialogueID == "" || result.RunID == "" {
		t.Fatalf("CallApiOfAi() result = %+v", result)
	}
	if runner.calls != 1 {
		t.Fatalf("runtime calls = %d, want 1", runner.calls)
	}
	if runner.request.Context.Mode != agentRuntime.ModeChat ||
		runner.request.Context.AgentProfileID != profileConversationReply ||
		runner.request.Context.AgentProfileVersion != "v1" {
		t.Fatalf("chat runtime context = %+v", runner.request.Context)
	}
	if runner.request.Context.PromptTemplateID != "conversation.reply.system" ||
		runner.request.Context.PromptTemplateVersion != "v1" {
		t.Fatalf(
			"chat prompt identity = %s@%s",
			runner.request.Context.PromptTemplateID,
			runner.request.Context.PromptTemplateVersion,
		)
	}
	if runner.request.Context.Budget.MaxSteps != 1 {
		t.Fatalf("chat budget = %+v", runner.request.Context.Budget)
	}
	if len(runner.request.Tools) != 0 {
		t.Fatalf("chat runtime tools = %+v, want none", runner.request.Tools)
	}
	if len(runner.request.Messages) != 4 ||
		runner.request.Messages[1].Content != "earlier question" ||
		runner.request.Messages[2].Content != "earlier answer" ||
		runner.request.Messages[3].Content != "continue" {
		t.Fatalf("chat runtime messages = %+v", runner.request.Messages)
	}
	if len(repo.saved) != 2 || !repo.touched {
		t.Fatalf("saved/touched = %d/%t", len(repo.saved), repo.touched)
	}
	metadata := repo.saved[1].Metadata
	if metadata["agent_profile"] != profileConversationReply ||
		metadata["execution_profile"] != ExecutionProfileRuntimeChat ||
		metadata["runtime_run_id"] != result.RunID {
		t.Fatalf("assistant metadata = %+v", metadata)
	}
}

func TestCallApiOfAiRuntimeDoesNotReturnUnpersistedAnswer(t *testing.T) {
	rollout, err := agentRuntime.ParseRollout("chat")
	if err != nil {
		t.Fatalf("ParseRollout() error = %v", err)
	}
	repo := &assistRuntimeRepository{saveErr: errors.New("mongo unavailable")}
	runner := &capturingRuntimeRunner{result: agentRuntime.RunResult{
		Status: agentRuntime.RunStatusCompleted, FinalAnswer: "unpersisted reply",
		Steps: []agentRuntime.Step{{Index: 1}},
	}}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		repo, nil, nil,
		WithRuntimeRollout(rollout),
		WithAgentRunner(runner),
	)
	defer service.Close()

	result, err := service.CallApiOfAi(context.Background(), 42, 0, "", "hello")
	if err == nil || !strings.Contains(err.Error(), "persist chat conversation failed") {
		t.Fatalf("CallApiOfAi() result/error = %+v/%v, want persistence failure", result, err)
	}
	if result != nil {
		t.Fatalf("CallApiOfAi() result = %+v, want nil", result)
	}
}

func TestAssistPublishTwitterRuntimeUsesReadOnlyProfile(t *testing.T) {
	rollout, err := agentRuntime.ParseRollout("assist")
	if err != nil {
		t.Fatalf("ParseRollout() error = %v", err)
	}
	repo := &assistRuntimeRepository{}
	runner := &capturingRuntimeRunner{result: agentRuntime.RunResult{
		Status: agentRuntime.RunStatusCompleted, FinalAnswer: "three complete drafts",
		Steps: []agentRuntime.Step{{Index: 1}},
	}}
	profileResolver, err := NewBuiltInProfileResolver([]profile.Release{{
		ProfileID:            profileAssistDraft,
		StableVersion:        "v1",
		CandidateVersion:     "v2",
		CandidateBasisPoints: profile.MaxReleaseBasisPoints,
	}})
	if err != nil {
		t.Fatalf("NewBuiltInProfileResolver() error = %v", err)
	}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		repo, nil, nil,
		WithRuntimeRollout(rollout),
		WithProfileResolver(profileResolver),
		WithAgentRunner(runner),
		WithRuntimeToolCatalog(staticRuntimeToolCatalog{tools: []agentRuntime.ToolDefinition{
			{Name: "hybrid_search_tweets", Category: agentRuntime.ToolCategoryRead},
			{Name: "create_tweet", Category: agentRuntime.ToolCategoryWrite},
			{Name: "unknown", Category: agentRuntime.ToolCategoryRisky},
		}}),
	)
	defer service.Close()

	models := service.GetModelInfo()
	modelCtx, err := service.ContextWithModelKind(context.Background(), models[0].ID)
	if err != nil {
		t.Fatalf("ContextWithModelKind() error = %v", err)
	}
	result, err := service.AssistPublishTwitter(modelCtx, 42, 0, "", "write about Go agents")
	if err != nil {
		t.Fatalf("AssistPublishTwitter() error = %v", err)
	}
	if result.Response != "three complete drafts" || result.DialogueID == "" || result.RunID == "" {
		t.Fatalf("AssistPublishTwitter() result = %+v", result)
	}
	if runner.request.Context.Mode != agentRuntime.ModeAssist || runner.request.Context.Budget.MaxSteps != 5 {
		t.Fatalf("assist runtime context = %+v", runner.request.Context)
	}
	if runner.request.Context.PromptTemplateID != "assist.draft.system" || runner.request.Context.PromptTemplateVersion != "v2" {
		t.Fatalf("assist prompt identity = %s@%s", runner.request.Context.PromptTemplateID, runner.request.Context.PromptTemplateVersion)
	}
	if !strings.Contains(runner.request.Messages[0].Content, "user_id: 42") {
		t.Fatalf("assist profile did not materialize user identity: %q", runner.request.Messages[0].Content)
	}
	if runner.request.Model != models[0].Name {
		t.Fatalf("assist runtime model = %q, want %q", runner.request.Model, models[0].Name)
	}
	if len(runner.request.Tools) != 1 || runner.request.Tools[0].Name != "hybrid_search_tweets" {
		t.Fatalf("assist runtime tools = %+v", runner.request.Tools)
	}
	if !strings.Contains(runner.request.Messages[0].Content, "不调用任何写工具") {
		t.Fatalf("assist profile prompt = %q", runner.request.Messages[0].Content)
	}
	if len(repo.saved) != 2 || !repo.touched {
		t.Fatalf("saved/touched = %d/%t", len(repo.saved), repo.touched)
	}
	if repo.saved[1].Metadata["agent_profile"] != profileAssistDraft {
		t.Fatalf("assistant metadata = %+v", repo.saved[1].Metadata)
	}
	if repo.saved[1].Metadata["agent_profile_version"] != "v2" || repo.saved[1].Metadata["prompt_version"] != "v2" {
		t.Fatalf("assistant version metadata = %+v", repo.saved[1].Metadata)
	}
	if repo.saved[1].Metadata["runtime_run_id"] != result.RunID || repo.saved[1].Metadata["runtime_mode"] != "assist" {
		t.Fatalf("assistant run metadata = %+v", repo.saved[1].Metadata)
	}
	if repo.saved[1].Metadata["execution_profile"] != ExecutionProfileRuntimeDraft {
		t.Fatalf("assistant execution metadata = %+v", repo.saved[1].Metadata)
	}
}

func TestAssistPublishTwitterRuntimeDoesNotReturnUnpersistedDraft(t *testing.T) {
	rollout, err := agentRuntime.ParseRollout("assist")
	if err != nil {
		t.Fatalf("ParseRollout() error = %v", err)
	}
	repo := &assistRuntimeRepository{saveErr: errors.New("mongo unavailable")}
	runner := &capturingRuntimeRunner{result: agentRuntime.RunResult{
		Status:      agentRuntime.RunStatusCompleted,
		FinalAnswer: "unpersisted draft",
		Steps:       []agentRuntime.Step{{Index: 1}},
	}}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		repo, nil, nil,
		WithRuntimeRollout(rollout),
		WithAgentRunner(runner),
		WithRuntimeToolCatalog(staticRuntimeToolCatalog{}),
	)
	defer service.Close()

	result, err := service.AssistPublishTwitter(context.Background(), 42, 0, "", "write a draft")
	if err == nil || !strings.Contains(err.Error(), "persist assist draft conversation failed") {
		t.Fatalf("AssistPublishTwitter() result/error = %+v/%v, want persistence failure", result, err)
	}
	if result != nil {
		t.Fatalf("AssistPublishTwitter() result = %+v, want nil", result)
	}
}

func TestExecuteWorkflowStrategyRuntimePreservesNodeConfiguration(t *testing.T) {
	rollout, err := agentRuntime.ParseRollout("workflow")
	if err != nil {
		t.Fatalf("ParseRollout() error = %v", err)
	}
	runner := &capturingRuntimeRunner{result: agentRuntime.RunResult{
		Status:      agentRuntime.RunStatusCompleted,
		FinalAnswer: "verified result",
		Steps: []agentRuntime.Step{
			{
				Index: 1,
				Actions: []agentRuntime.Action{{
					ID: "call-1", Type: agentRuntime.ActionToolCall, Name: "hybrid_search_tweets",
					Arguments: json.RawMessage(`{"query":"golang"}`),
				}},
				Observations: []agentRuntime.Observation{{ActionID: "call-1", Name: "hybrid_search_tweets", Content: "result"}},
			},
			{Index: 2, Actions: []agentRuntime.Action{{Type: agentRuntime.ActionFinalAnswer, Content: "verified result"}}},
		},
	}}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		nil, nil, nil,
		WithRuntimeRollout(rollout),
		WithAgentRunner(runner),
		WithRuntimeToolCatalog(staticRuntimeToolCatalog{tools: []agentRuntime.ToolDefinition{
			{Name: "hybrid_search_tweets", Category: agentRuntime.ToolCategoryRead},
			{Name: "search_users", Category: agentRuntime.ToolCategoryRead},
			{Name: "create_tweet", Category: agentRuntime.ToolCategoryWrite},
		}}),
	)
	defer service.Close()

	ctx := guardrails.InjectUserContext(context.Background(), 77)
	output, err := service.ExecuteWorkflowStrategy(ctx, "PlanExecutor", map[string]interface{}{
		"objective":      "research Go agents",
		"plan":           "search and summarize",
		"system_prompt":  "cite tool evidence",
		"allowed_tools":  "hybrid_search_tweets,create_tweet",
		"model":          "workflow-model",
		"max_tokens":     321,
		"max_iterations": 7,
	})
	if err != nil {
		t.Fatalf("ExecuteWorkflowStrategy() error = %v", err)
	}
	if output["text"] != "verified result" || output["iterations"] != 2 {
		t.Fatalf("strategy output = %+v", output)
	}
	if runner.request.Model != "workflow-model" || runner.request.Context.Budget.MaxOutputTokens != 321 || runner.request.Context.Budget.MaxSteps != 7 {
		t.Fatalf("strategy request model/budget = %q/%+v", runner.request.Model, runner.request.Context.Budget)
	}
	if runner.request.Context.PromptTemplateID != "workflow.plan_execute.system" || runner.request.Context.PromptTemplateVersion != "v1" {
		t.Fatalf("workflow prompt identity = %s@%s", runner.request.Context.PromptTemplateID, runner.request.Context.PromptTemplateVersion)
	}
	if len(runner.request.Tools) != 1 || runner.request.Tools[0].Name != "hybrid_search_tweets" {
		t.Fatalf("strategy runtime tools = %+v", runner.request.Tools)
	}
	if !strings.Contains(runner.request.Messages[0].Content, "Plan-Execute") || !strings.Contains(runner.request.Messages[0].Content, "cite tool evidence") {
		t.Fatalf("strategy system prompt = %q", runner.request.Messages[0].Content)
	}
	trace, ok := output["tool_trace"].([]map[string]interface{})
	if !ok || len(trace) != 1 || trace[0]["tool"] != "hybrid_search_tweets" {
		t.Fatalf("strategy tool trace = %#v", output["tool_trace"])
	}
	arguments, ok := trace[0]["arguments"].(map[string]interface{})
	if !ok || arguments["query"] != "[REDACTED]" {
		t.Fatalf("strategy trace arguments = %#v", trace[0]["arguments"])
	}
}

func TestBuiltInProfilesPreserveMultiWriterPromptAndToolRoles(t *testing.T) {
	const previousWriterPrompt = "你是一个严谨的多智能体写作总编。你的第一优先级是产出可直接发布、内容饱满、有观点和细节的候选正文；研究摘要和风格判断只能服务正文，不能喧宾夺主。你必须执行主题隔离：任何与用户当前主题不相关的检索结果、历史推文或参考材料都要丢弃，不得混入正文。"
	if multiWriterSystemPrompt() != previousWriterPrompt {
		t.Fatalf("multi writer prompt changed unexpectedly")
	}
	if multiSearchAgentProfile.PrimaryTool() != "hybrid_search_tweets" || multiStyleAgentProfile.PrimaryTool() != "get_user_tweets" {
		t.Fatalf("multi profile tools = %q/%q", multiSearchAgentProfile.PrimaryTool(), multiStyleAgentProfile.PrimaryTool())
	}
	if multiReviewAgentProfile.ID != profileMultiReview {
		t.Fatalf("review profile ID = %q", multiReviewAgentProfile.ID)
	}
}
