package grpc

import (
	"testing"
	"time"

	"twitter-clone/internal/module/agent/service"
	agentStrategy "twitter-clone/internal/module/agent/strategy"
)

func TestUnifiedAgentResultToProtoPreservesStructuredEvidence(t *testing.T) {
	t.Parallel()

	response := unifiedAgentResultToProto(&service.UnifiedAgentResult{
		ChatResult: service.ChatResult{
			DialogueID: "dialogue-1",
			RunID:      "run-1",
			Response:   "answer",
		},
		ExecutionProfile:     "runtime.research_draft",
		CapabilityIDs:        []string{"platform.search", "content.draft"},
		SelectedSkillID:      "workflow.66aa00000000000000000001",
		SelectedSkillVersion: "v1-7cf9",
		RunStatus:            service.UnifiedAgentRunStatusCompleted,
		PublishableDraft:     true,
		ToolActivities: []service.AgentToolActivity{{
			StepIndex: 2, ToolName: "hybrid_search_tweets",
			Status: service.AgentToolActivitySucceeded, ResultCount: 1,
		}},
		Citations: []service.AgentCitation{{
			CitationID: "platform_tweet:9007199254740993",
			SourceType: service.AgentCitationPlatformTweet,
			SourceID:   "9007199254740993",
			URL:        "/tweets/9007199254740993",
			Snippet:    "source",
		}},
		Artifacts: []service.AgentArtifact{{
			ArtifactID: "content.draft:run-1", Type: service.AgentArtifactTypeContentDraft,
			Status: service.AgentArtifactStatusReady, ContentType: service.AgentArtifactContentMarkdown,
			Content: "answer", SourceRunID: "run-1", RequiresConfirmation: true,
		}},
		ApprovalState: service.AgentApprovalState{
			Status: service.AgentApprovalStatusNotRequired, RunID: "run-1",
		},
		ExecutionStrategyPlan: agentStrategy.Plan{
			Version: agentStrategy.PlanVersionV1, CandidateStrategy: agentStrategy.KindMultiAgent,
			SelectedStrategy: agentStrategy.KindSingleAgent, Decision: agentStrategy.DecisionFallback,
			ReasonCode: agentStrategy.ReasonMultiExecutorUnavailable, PlanDigest: "digest-1",
			Roles: []agentStrategy.RolePlan{{RoleID: "researcher", AllowedTools: []string{"hybrid_search_tweets"}}},
		},
	})

	if response == nil || len(response.ToolActivities) != 1 || len(response.Citations) != 1 ||
		len(response.Artifacts) != 1 || response.ApprovalState == nil {
		t.Fatalf("response = %+v", response)
	}
	if response.ToolActivities[0].ToolName != "hybrid_search_tweets" ||
		response.ToolActivities[0].ResultCount != 1 {
		t.Fatalf("tool activity = %+v", response.ToolActivities[0])
	}
	if response.Citations[0].SourceId != "9007199254740993" ||
		response.Citations[0].Url != "/tweets/9007199254740993" {
		t.Fatalf("citation = %+v", response.Citations[0])
	}
	if response.RunStatus != service.UnifiedAgentRunStatusCompleted ||
		response.SelectedSkillId != "workflow.66aa00000000000000000001" ||
		response.SelectedSkillVersion != "v1-7cf9" ||
		response.Artifacts[0].SourceRunId != "run-1" ||
		!response.Artifacts[0].RequiresConfirmation ||
		response.ApprovalState.Status != service.AgentApprovalStatusNotRequired ||
		response.ApprovalState.RunId != "run-1" {
		t.Fatalf("run projection = status %q artifact %+v approval %+v", response.RunStatus, response.Artifacts[0], response.ApprovalState)
	}
	if response.ExecutionStrategyPlan == nil ||
		response.ExecutionStrategyPlan.CandidateStrategy != string(agentStrategy.KindMultiAgent) ||
		response.ExecutionStrategyPlan.SelectedStrategy != string(agentStrategy.KindSingleAgent) ||
		response.ExecutionStrategyPlan.PlanDigest != "digest-1" ||
		len(response.ExecutionStrategyPlan.Roles) != 1 {
		t.Fatalf("execution strategy projection = %+v", response.ExecutionStrategyPlan)
	}
}

func TestAgentExecutionRunViewToProtoExcludesRecoveryInternals(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	response := agentExecutionRunViewToProto(&service.AgentExecutionRunView{
		RunID: "run-1", DialogueID: "dialogue-1", Status: "awaiting_human",
		SkillID: "workflow.66aa00000000000000000001", SkillVersion: "v1-7cf9",
		Revision: 2, ResumeSupported: true, PendingActionType: "ask_human",
		StartedAt: now, UpdatedAt: now,
	})
	if response == nil || response.RunId != "run-1" || response.Revision != 2 ||
		response.SkillId != "workflow.66aa00000000000000000001" ||
		response.SkillVersion != "v1-7cf9" ||
		!response.ResumeSupported || response.StartedAt != now.UnixMilli() {
		t.Fatalf("agent run projection = %+v", response)
	}
}
