package service

import (
	"context"
	"testing"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func TestE2E11ResearchThenDraftDualRecordsSingleExecution(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		web          bool
		capabilities []string
		run          agentRuntime.RunResult
	}{
		{
			name:         "platform research draft",
			capabilities: []string{CapabilityPlatformSearch, CapabilityContentDraft},
			run: withResearchDraftFinalStep(groundedDraftPlatformShadowRun(
				"Go 的并发模型适合组织云原生任务。 [/tweets/2084827196752420864]",
			)),
		},
		{
			name:         "web research draft",
			web:          true,
			capabilities: []string{CapabilityWebSearch, CapabilityContentDraft},
			run: withResearchDraftFinalStep(groundedDraftWebShadowRun(
				"Go 官方发布页持续记录版本演进。 [https://go.dev/doc/devel/release]",
			)),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repo := &assistRuntimeRepository{}
			runner := &capturingRuntimeRunner{result: test.run}
			observer := &goalRuntimeShadowObserverFake{}
			service := newGroundedDraftShadowService(t, repo, runner, observer, test.web)
			service.goalRuntimeShadow = GoalRuntimeShadowConfig{
				Enabled: true, ResearchDraftEnabled: true,
			}
			defer service.Close()

			result, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
				UserID: 42, Content: "research first, then draft",
				PreferredCapabilityIDs: test.capabilities,
			})
			if err != nil {
				t.Fatalf("RunAgent() error = %v", err)
			}
			if result.Response != runner.result.FinalAnswer || runner.calls != 1 ||
				len(repo.saved) != 2 || len(observer.observations) != 1 {
				t.Fatalf(
					"response/calls/saved/observations = %q/%d/%d/%d",
					result.Response, runner.calls, len(repo.saved), len(observer.observations),
				)
			}
			assertResearchThenDraftObservation(t, observer.observations[0], true)
		})
	}
}

func TestE2E11ResearchAfterDraftIsObservedWithoutChangingLegacyResponse(t *testing.T) {
	t.Parallel()
	run := groundedDraftPlatformShadowRun(
		"Go 的并发模型适合组织云原生任务。 [/tweets/2084827196752420864]",
	)
	answer := run.FinalAnswer
	run.Steps[0].Index = 2
	run.Steps = append([]agentRuntime.Step{{
		Index: 1,
		Actions: []agentRuntime.Action{{
			ID: "final-1", Type: agentRuntime.ActionFinalAnswer, Content: answer,
		}},
	}}, run.Steps...)
	repo := &assistRuntimeRepository{}
	runner := &capturingRuntimeRunner{result: run}
	observer := &goalRuntimeShadowObserverFake{}
	service := newGroundedDraftShadowService(t, repo, runner, observer, false)
	service.goalRuntimeShadow = GoalRuntimeShadowConfig{
		Enabled: true, ResearchDraftEnabled: true,
	}
	defer service.Close()

	result, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, Content: "draft with platform research",
		PreferredCapabilityIDs: []string{CapabilityPlatformSearch, CapabilityContentDraft},
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if result.Response != answer || runner.calls != 1 || len(repo.saved) != 2 ||
		len(observer.observations) != 1 {
		t.Fatalf("response/calls/saved/observations = %q/%d/%d/%d", result.Response, runner.calls, len(repo.saved), len(observer.observations))
	}
	assertResearchThenDraftObservation(t, observer.observations[0], false)
}

func TestObserveResearchThenDraftGoalShadowHonorsDedicatedFlag(t *testing.T) {
	t.Parallel()
	observer := &goalRuntimeShadowObserverFake{}
	service := &AgentService{goalRuntimeShadowObserver: observer}
	run := withResearchDraftFinalStep(groundedDraftPlatformShadowRun(
		"Go 的并发模型适合组织云原生任务。 [/tweets/2084827196752420864]",
	))

	service.goalRuntimeShadow = GoalRuntimeShadowConfig{Enabled: true, GroundedDraftEnabled: true}
	service.observeResearchThenDraftGoalShadow(
		context.Background(), "draft", agentEvidence.GroundedDraftSourcePlatform, run, nil,
	)
	if len(observer.observations) != 0 {
		t.Fatalf("disabled research draft shadow emitted %d observations", len(observer.observations))
	}
	service.goalRuntimeShadow.ResearchDraftEnabled = true
	service.observeResearchThenDraftGoalShadow(
		context.Background(), "draft", agentEvidence.GroundedDraftSourcePlatform, run, nil,
	)
	if len(observer.observations) != 1 {
		t.Fatalf("enabled research draft shadow emitted %d observations", len(observer.observations))
	}
}

func withResearchDraftFinalStep(run agentRuntime.RunResult) agentRuntime.RunResult {
	maxIndex := 0
	for _, step := range run.Steps {
		if step.Index > maxIndex {
			maxIndex = step.Index
		}
	}
	run.Steps = append(run.Steps, agentRuntime.Step{
		Index: maxIndex + 1,
		Actions: []agentRuntime.Action{{
			ID: "final-1", Type: agentRuntime.ActionFinalAnswer, Content: run.FinalAnswer,
		}},
	})
	return run
}

func assertResearchThenDraftObservation(
	t *testing.T,
	observation GoalRuntimeShadowObservation,
	wantPass bool,
) {
	t.Helper()
	if observation.Capability != GoalShadowCapabilityResearchDraft ||
		observation.LegacyOutcome != GoalShadowLegacyCompleted ||
		observation.TaskOutcome == nil ||
		observation.TaskOutcome.ExecutionSource != agentRuntime.TaskOutcomeExecutionObserved {
		t.Fatalf("observation = %+v", observation)
	}
	if wantPass {
		if observation.GoalOutcome != agentRuntime.VerificationPassed ||
			observation.EvidenceComparison != GoalShadowComparisonConsistent ||
			observation.TaskOutcome.Status != agentRuntime.GoalRunVerified {
			t.Fatalf("observation = %+v", observation)
		}
		assertGroundedDraftShadowCheck(
			t, observation, agentEvidence.ResearchThenDraftOrderCriterion,
			agentEvidence.ResearchThenDraftOrderVerifiedCode,
		)
		return
	}
	if observation.GoalOutcome != agentRuntime.VerificationFailed ||
		observation.EvidenceComparison != GoalShadowComparisonLegacyOnly ||
		observation.TaskOutcome.Status != agentRuntime.GoalRunBlocked {
		t.Fatalf("observation = %+v", observation)
	}
	assertGroundedDraftShadowCheck(
		t, observation, agentEvidence.ResearchThenDraftOrderCriterion,
		agentEvidence.ResearchThenDraftOrderMissingCode,
	)
}
