package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
	agentRuntime "twitter-clone/internal/module/agent/runtime"

	"github.com/prometheus/client_golang/prometheus"
)

func TestEvaluatePlatformSearchGoalShadowUsesExistingStructuredResult(t *testing.T) {
	result := platformSearchShadowRun(t, true)

	observation := evaluatePlatformSearchGoalShadow(context.Background(), "搜索 Go", result, nil)

	if observation.LegacyOutcome != GoalShadowLegacyCompleted ||
		observation.GoalOutcome != agentRuntime.VerificationPassed ||
		observation.EvidenceComparison != GoalShadowComparisonConsistent {
		t.Fatalf("observation = %+v", observation)
	}
}

func TestEvaluatePlatformSearchGoalShadowDetectsLegacyTextOnlyEvidence(t *testing.T) {
	result := platformSearchShadowRun(t, false)

	observation := evaluatePlatformSearchGoalShadow(context.Background(), "搜索 Go", result, nil)

	if observation.LegacyOutcome != GoalShadowLegacyCompleted ||
		observation.GoalOutcome != agentRuntime.VerificationFailed ||
		observation.EvidenceComparison != GoalShadowComparisonLegacyOnly {
		t.Fatalf("observation = %+v", observation)
	}
}

func TestEvaluatePlatformSearchGoalShadowDoesNotVerifyFailedExecution(t *testing.T) {
	result := platformSearchShadowRun(t, true)
	result.Status = agentRuntime.RunStatusFailed

	observation := evaluatePlatformSearchGoalShadow(
		context.Background(), "搜索 Go", result, errors.New("runtime failed"),
	)

	if observation.LegacyOutcome != GoalShadowLegacyFailed ||
		observation.GoalOutcome != agentRuntime.VerificationInconclusive ||
		observation.EvidenceComparison != GoalShadowComparisonExecutionIncomplete {
		t.Fatalf("observation = %+v", observation)
	}
}

func TestObservePlatformSearchGoalShadowHonorsBothFlags(t *testing.T) {
	observer := &goalRuntimeShadowObserverFake{}
	service := &AgentService{goalRuntimeShadowObserver: observer}
	result := platformSearchShadowRun(t, true)

	service.observePlatformSearchGoalShadow(context.Background(), "搜索 Go", result, nil)
	service.goalRuntimeShadow = GoalRuntimeShadowConfig{Enabled: true}
	service.observePlatformSearchGoalShadow(context.Background(), "搜索 Go", result, nil)
	if len(observer.observations) != 0 {
		t.Fatalf("disabled shadow emitted %d observations", len(observer.observations))
	}

	service.goalRuntimeShadow.PlatformSearchEnabled = true
	service.observePlatformSearchGoalShadow(context.Background(), "搜索 Go", result, nil)
	if len(observer.observations) != 1 {
		t.Fatalf("enabled shadow emitted %d observations", len(observer.observations))
	}
}

func TestPrometheusGoalRuntimeShadowObserverBoundsLabels(t *testing.T) {
	registry := prometheus.NewRegistry()
	observer, err := NewPrometheusGoalRuntimeShadowObserver(registry)
	if err != nil {
		t.Fatalf("NewPrometheusGoalRuntimeShadowObserver() error = %v", err)
	}
	observer.ObserveGoalRuntimeShadow(GoalRuntimeShadowObservation{
		Capability:         "tenant-controlled",
		LegacyOutcome:      "tenant-controlled",
		GoalOutcome:        "tenant-controlled",
		EvidenceComparison: "tenant-controlled",
	})

	if got := prometheusCounterValue(t, observer.evaluations.WithLabelValues(
		"unknown", "unknown", "unknown", "unknown",
	)); got != 1 {
		t.Fatalf("bounded counter = %v, want 1", got)
	}
}

type goalRuntimeShadowObserverFake struct {
	observations []GoalRuntimeShadowObservation
}

func (o *goalRuntimeShadowObserverFake) ObserveGoalRuntimeShadow(observation GoalRuntimeShadowObservation) {
	o.observations = append(o.observations, observation)
}

func platformSearchShadowRun(t *testing.T, structured bool) agentRuntime.RunResult {
	t.Helper()
	var structuredContent json.RawMessage
	if structured {
		encoded, err := json.Marshal(agentEvidence.PlatformTweetSearchResult{
			Schema: agentEvidence.PlatformTweetSearchSchema,
			Items: []agentEvidence.PlatformTweetSearchEvidence{{
				TweetID: "2084827196752420864",
				Content: "Go runtime evidence",
			}},
		})
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
		structuredContent = encoded
	}
	return agentRuntime.RunResult{
		Context: agentRuntime.RunContext{RunID: "run-shadow-1", UserID: 42},
		Status:  agentRuntime.RunStatusCompleted,
		Steps: []agentRuntime.Step{{
			Index:   0,
			Actions: []agentRuntime.Action{{ID: "search-1", Type: agentRuntime.ActionToolCall, Name: "hybrid_search_tweets"}},
			Observations: []agentRuntime.Observation{{
				ActionID: "search-1", Name: "hybrid_search_tweets", Content: "search result",
				StructuredContent: structuredContent,
			}},
		}},
	}
}
