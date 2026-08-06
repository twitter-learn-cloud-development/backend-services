package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	agentStrategy "twitter-clone/internal/module/agent/strategy"
)

func TestPrometheusUnifiedAgentProductObserverRecordsBoundedTaskMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	observer, err := NewPrometheusUnifiedAgentProductObserver(registry)
	if err != nil {
		t.Fatalf("NewPrometheusUnifiedAgentProductObserver() error = %v", err)
	}

	webCitation, valid := webPageCitationForMetricsTest("https://go.dev/doc/devel/release?stable=1")
	if !valid {
		t.Fatal("web citation fixture is invalid")
	}
	observer.ObserveTaskStarted(UnifiedAgentTaskStartedObservation{
		ExecutionProfile: ExecutionProfileRuntimeWebDraft,
		Strategy:         agentStrategy.KindMultiAgent,
	})
	observer.ObserveTaskCommitted(UnifiedAgentTaskCommittedObservation{
		ExecutionProfile: ExecutionProfileRuntimeWebDraft,
		Strategy:         agentStrategy.KindMultiAgent,
		Status:           repository.AgentExecutionRunCompleted,
		PublishableDraft: true,
		Duration:         1500 * time.Millisecond,
		StepCount:        3,
		Usage: agentRuntime.TokenUsage{
			InputTokens: 8, OutputTokens: 4, TotalTokens: 12, Estimated: true,
			EstimatedCostMicros: 9, CostEstimated: true,
		},
		ToolActivities: []AgentToolActivity{
			{ToolName: "tenant-dynamic-tool-a", Status: AgentToolActivitySucceeded},
			{ToolName: "tenant-dynamic-tool-b", Status: AgentToolActivityFailed},
		},
		Citations: []AgentCitation{
			{
				CitationID: AgentCitationPlatformTweet + ":42",
				SourceType: AgentCitationPlatformTweet,
				SourceID:   "42",
				URL:        "/tweets/42",
			},
			webCitation,
			{
				CitationID: "tenant-citation",
				SourceType: "tenant-source-type",
				SourceID:   "tenant-source-id",
				URL:        "https://tenant.example/private",
			},
		},
	})
	observer.ObserveDraftPublished(UnifiedAgentDraftPublishedObservation{
		ExecutionProfile: ExecutionProfileRuntimeWebDraft,
		Strategy:         agentStrategy.KindMultiAgent,
	})

	if got := prometheusCounterValue(t, observer.tasksStarted.WithLabelValues(
		ExecutionProfileRuntimeWebDraft,
		string(agentStrategy.KindMultiAgent),
	)); got != 1 {
		t.Fatalf("tasks started = %v, want 1", got)
	}
	if got := prometheusCounterValue(t, observer.outcomes.WithLabelValues(
		ExecutionProfileRuntimeWebDraft,
		string(agentStrategy.KindMultiAgent),
		string(repository.AgentExecutionRunCompleted),
	)); got != 1 {
		t.Fatalf("completed outcomes = %v, want 1", got)
	}
	if got := prometheusCounterValue(t, observer.tokens.WithLabelValues(
		ExecutionProfileRuntimeWebDraft,
		string(agentStrategy.KindMultiAgent),
		string(repository.AgentExecutionRunCompleted),
		"total",
		"true",
	)); got != 12 {
		t.Fatalf("total tokens = %v, want 12", got)
	}
	if got := prometheusCounterValue(t, observer.toolCalls.WithLabelValues(
		ExecutionProfileRuntimeWebDraft,
		string(agentStrategy.KindMultiAgent),
		string(repository.AgentExecutionRunCompleted),
		AgentToolActivityFailed,
	)); got != 1 {
		t.Fatalf("failed tool calls = %v, want 1", got)
	}
	if got := prometheusCounterValue(t, observer.citations.WithLabelValues(
		ExecutionProfileRuntimeWebDraft,
		string(agentStrategy.KindMultiAgent),
		string(repository.AgentExecutionRunCompleted),
		AgentCitationWebPage,
		"valid",
	)); got != 1 {
		t.Fatalf("valid web citations = %v, want 1", got)
	}
	if got := prometheusCounterValue(t, observer.citations.WithLabelValues(
		ExecutionProfileRuntimeWebDraft,
		string(agentStrategy.KindMultiAgent),
		string(repository.AgentExecutionRunCompleted),
		"unknown",
		"invalid",
	)); got != 1 {
		t.Fatalf("invalid unknown citations = %v, want 1", got)
	}
	if got := prometheusCounterValue(t, observer.draftEvents.WithLabelValues(
		ExecutionProfileRuntimeWebDraft,
		string(agentStrategy.KindMultiAgent),
		"ready",
	)); got != 1 {
		t.Fatalf("draft ready events = %v, want 1", got)
	}
	if got := prometheusCounterValue(t, observer.draftEvents.WithLabelValues(
		ExecutionProfileRuntimeWebDraft,
		string(agentStrategy.KindMultiAgent),
		"published",
	)); got != 1 {
		t.Fatalf("draft published events = %v, want 1", got)
	}

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	for _, family := range families {
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				switch label.GetName() {
				case "user_id", "run_id", "model", "tool_name", "citation_id", "source_id", "url":
					t.Fatalf("metric %s contains forbidden label %q", family.GetName(), label.GetName())
				}
				if value := label.GetValue(); value == "tenant-dynamic-tool-a" ||
					value == "tenant-dynamic-tool-b" || value == "tenant-source-type" ||
					value == "tenant-source-id" || value == "tenant-citation" {
					t.Fatalf("metric %s leaked high-cardinality label value %q", family.GetName(), value)
				}
			}
		}
	}
}

func TestValidUnifiedMetricCitationAcceptsCanonicalAndLegacyTweetPaths(t *testing.T) {
	t.Parallel()

	for _, resourceURL := range []string{"/tweets/42", "/tweet/42"} {
		citation := AgentCitation{
			CitationID: AgentCitationPlatformTweet + ":42",
			SourceType: AgentCitationPlatformTweet,
			SourceID:   "42",
			URL:        resourceURL,
		}
		if !validUnifiedMetricCitation(citation) {
			t.Fatalf("validUnifiedMetricCitation(%q) = false", resourceURL)
		}
	}
}

func TestPrometheusUnifiedAgentProductObserverBoundsUnknownDimensions(t *testing.T) {
	registry := prometheus.NewRegistry()
	observer, err := NewPrometheusUnifiedAgentProductObserver(registry)
	if err != nil {
		t.Fatalf("NewPrometheusUnifiedAgentProductObserver() error = %v", err)
	}
	observer.ObserveTaskStarted(UnifiedAgentTaskStartedObservation{
		ExecutionProfile: "tenant-profile-42",
		Strategy:         agentStrategy.Kind("tenant-strategy-42"),
	})
	if got := prometheusCounterValue(t, observer.tasksStarted.WithLabelValues("unknown", "unknown")); got != 1 {
		t.Fatalf("bounded unknown starts = %v, want 1", got)
	}
}

func prometheusCounterValue(t *testing.T, metric prometheus.Metric) float64 {
	t.Helper()
	value := &dto.Metric{}
	if err := metric.Write(value); err != nil {
		t.Fatalf("write Prometheus metric: %v", err)
	}
	if value.Counter == nil {
		t.Fatal("Prometheus metric is not a counter")
	}
	return value.Counter.GetValue()
}

type recordingUnifiedAgentProductObserver struct {
	started   []UnifiedAgentTaskStartedObservation
	committed []UnifiedAgentTaskCommittedObservation
	published []UnifiedAgentDraftPublishedObservation
}

func (o *recordingUnifiedAgentProductObserver) ObserveDraftPublished(
	observation UnifiedAgentDraftPublishedObservation,
) {
	o.published = append(o.published, observation)
}

func (o *recordingUnifiedAgentProductObserver) ObserveTaskStarted(
	observation UnifiedAgentTaskStartedObservation,
) {
	o.started = append(o.started, observation)
}

func (o *recordingUnifiedAgentProductObserver) ObserveTaskCommitted(
	observation UnifiedAgentTaskCommittedObservation,
) {
	o.committed = append(o.committed, observation)
}

func TestUnifiedAgentProductObserverFollowsAuthoritativeCommit(t *testing.T) {
	observer := &recordingUnifiedAgentProductObserver{}
	store := &memoryAgentExecutionRunStore{}
	runner := &capturingRuntimeRunner{result: agentRuntime.RunResult{
		Status:      agentRuntime.RunStatusCompleted,
		FinalAnswer: "measured reply",
		Steps: []agentRuntime.Step{{
			Index: 1,
			Actions: []agentRuntime.Action{{
				ID: "tool-1", Type: agentRuntime.ActionToolCall, Name: "dynamic.tool",
			}},
			Observations: []agentRuntime.Observation{{ActionID: "tool-1", IsError: true}},
		}},
		Usage: agentRuntime.TokenUsage{
			InputTokens: 5, OutputTokens: 3, TotalTokens: 8,
			EstimatedCostMicros: 6, CostEstimated: true,
		},
	}}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		&assistRuntimeRepository{}, nil, nil,
		WithAgentRunner(runner),
		WithRuntimeToolCatalog(staticRuntimeToolCatalog{}),
		WithAgentExecutionRunStore(store),
		WithRecoverableAgentRuns(true),
		WithUnifiedAgentProductObserver(observer),
	)
	defer service.Close()

	if _, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, Content: "measure this task",
		PreferredCapabilityIDs: []string{CapabilityContentDraft},
	}); err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if len(observer.started) != 1 || len(observer.committed) != 1 {
		t.Fatalf("observations started=%d committed=%d, want 1/1", len(observer.started), len(observer.committed))
	}
	committed := observer.committed[0]
	if committed.Status != repository.AgentExecutionRunCompleted || committed.StepCount != 1 ||
		committed.Usage.TotalTokens != 8 || committed.Strategy != agentStrategy.KindSingleAgent ||
		!committed.PublishableDraft || !store.run.PublishableDraft {
		t.Fatalf("committed observation = %+v", committed)
	}
	if len(committed.ToolActivities) != 1 || committed.ToolActivities[0].Status != AgentToolActivityFailed {
		t.Fatalf("tool activities = %+v", committed.ToolActivities)
	}
}

func TestUnifiedAgentProductObserverDoesNotReportUncommittedOutcome(t *testing.T) {
	observer := &recordingUnifiedAgentProductObserver{}
	store := &memoryAgentExecutionRunStore{commitErr: errors.New("commit unavailable")}
	runner := &capturingRuntimeRunner{result: agentRuntime.RunResult{
		Status: agentRuntime.RunStatusCompleted, FinalAnswer: "must not be counted",
	}}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		&assistRuntimeRepository{}, nil, nil,
		WithAgentRunner(runner),
		WithAgentExecutionRunStore(store),
		WithRecoverableAgentRuns(true),
		WithUnifiedAgentProductObserver(observer),
	)
	defer service.Close()

	if _, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, Content: "uncommitted task",
	}); err == nil {
		t.Fatal("RunAgent() error = nil, want commit failure")
	}
	if len(observer.started) != 1 || len(observer.committed) != 0 {
		t.Fatalf("observations started=%d committed=%d, want 1/0", len(observer.started), len(observer.committed))
	}
}

func webPageCitationForMetricsTest(resourceURL string) (AgentCitation, bool) {
	return webPageCitation(agentEvidence.WebSearchEvidence{URL: resourceURL})
}
