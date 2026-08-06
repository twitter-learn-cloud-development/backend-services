package service

import (
	"crypto/sha256"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	agentStrategy "twitter-clone/internal/module/agent/strategy"
)

// UnifiedAgentTaskStartedObservation represents an accepted task. It is
// emitted only after the authoritative Run has been created successfully.
type UnifiedAgentTaskStartedObservation struct {
	ExecutionProfile string
	Strategy         agentStrategy.Kind
}

// UnifiedAgentTaskCommittedObservation represents a successfully committed
// lifecycle transition. Content, tenant identifiers, Run IDs and model names
// are intentionally absent so metrics cannot acquire unbounded labels.
type UnifiedAgentTaskCommittedObservation struct {
	ExecutionProfile string
	Strategy         agentStrategy.Kind
	Status           repository.AgentExecutionRunStatus
	Duration         time.Duration
	StepCount        int
	Usage            agentRuntime.TokenUsage
	ToolActivities   []AgentToolActivity
	Citations        []AgentCitation
	PublishableDraft bool
}

type UnifiedAgentDraftPublishedObservation struct {
	ExecutionProfile string
	Strategy         agentStrategy.Kind
}

type UnifiedAgentProductObserver interface {
	ObserveTaskStarted(UnifiedAgentTaskStartedObservation)
	ObserveTaskCommitted(UnifiedAgentTaskCommittedObservation)
}

type UnifiedAgentDraftProductObserver interface {
	ObserveDraftPublished(UnifiedAgentDraftPublishedObservation)
}

type noopUnifiedAgentProductObserver struct{}

func (noopUnifiedAgentProductObserver) ObserveTaskStarted(UnifiedAgentTaskStartedObservation)     {}
func (noopUnifiedAgentProductObserver) ObserveTaskCommitted(UnifiedAgentTaskCommittedObservation) {}

// PrometheusUnifiedAgentProductObserver exposes product-level measurements
// over authoritative Unified Agent tasks. Tool-selection accuracy remains an
// offline labelled-evaluation metric; production telemetry cannot infer the
// user's intended tool without ground truth.
type PrometheusUnifiedAgentProductObserver struct {
	tasksStarted *prometheus.CounterVec
	outcomes     *prometheus.CounterVec
	duration     *prometheus.HistogramVec
	steps        *prometheus.HistogramVec
	tokens       *prometheus.CounterVec
	cost         *prometheus.CounterVec
	toolCalls    *prometheus.CounterVec
	citations    *prometheus.CounterVec
	draftEvents  *prometheus.CounterVec
}

func NewPrometheusUnifiedAgentProductObserver(
	registerer prometheus.Registerer,
) (*PrometheusUnifiedAgentProductObserver, error) {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}
	observer := &PrometheusUnifiedAgentProductObserver{
		tasksStarted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_unified_tasks_started_total",
			Help: "Accepted Unified Agent tasks by bounded execution profile and strategy.",
		}, []string{"execution_profile", "strategy"}),
		outcomes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_unified_task_outcomes_total",
			Help: "Committed Unified Agent lifecycle outcomes by bounded execution dimensions.",
		}, []string{"execution_profile", "strategy", "outcome"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "agent_unified_task_duration_seconds",
			Help:    "End-to-end duration from task acceptance to each committed lifecycle outcome.",
			Buckets: prometheus.DefBuckets,
		}, []string{"execution_profile", "strategy", "outcome"}),
		steps: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "agent_unified_task_steps",
			Help:    "Runtime step count at each committed Unified Agent lifecycle outcome.",
			Buckets: []float64{0, 1, 2, 3, 5, 8, 13, 21, 34, 55},
		}, []string{"execution_profile", "strategy", "outcome"}),
		tokens: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_unified_task_tokens_total",
			Help: "Unified Agent task token usage by outcome, direction and estimation state.",
		}, []string{"execution_profile", "strategy", "outcome", "direction", "estimated"}),
		cost: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_unified_task_estimated_cost_micros_total",
			Help: "Unified Agent task cost in integer micro-units by outcome and estimation state.",
		}, []string{"execution_profile", "strategy", "outcome", "estimated"}),
		toolCalls: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_unified_task_tool_calls_total",
			Help: "Tool calls projected onto committed Unified Agent outcomes without tool-name labels.",
		}, []string{"execution_profile", "strategy", "outcome", "result"}),
		citations: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_unified_task_citations_total",
			Help: "User-visible citations by bounded source type and structural validity.",
		}, []string{"execution_profile", "strategy", "outcome", "source_type", "validity"}),
		draftEvents: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_unified_draft_events_total",
			Help: "Durably attributed publishable-draft funnel transitions.",
		}, []string{"execution_profile", "strategy", "event"}),
	}

	collectors := []prometheus.Collector{
		observer.tasksStarted,
		observer.outcomes,
		observer.duration,
		observer.steps,
		observer.tokens,
		observer.cost,
		observer.toolCalls,
		observer.citations,
		observer.draftEvents,
	}
	registered := make([]prometheus.Collector, 0, len(collectors))
	for _, collector := range collectors {
		if err := registerer.Register(collector); err != nil {
			for _, previous := range registered {
				registerer.Unregister(previous)
			}
			return nil, fmt.Errorf("register Unified Agent product metrics: %w", err)
		}
		registered = append(registered, collector)
	}
	return observer, nil
}

func (o *PrometheusUnifiedAgentProductObserver) ObserveTaskStarted(
	observation UnifiedAgentTaskStartedObservation,
) {
	if o == nil {
		return
	}
	o.tasksStarted.WithLabelValues(
		boundedUnifiedExecutionProfile(observation.ExecutionProfile),
		boundedUnifiedStrategy(observation.Strategy),
	).Inc()
}

func (o *PrometheusUnifiedAgentProductObserver) ObserveTaskCommitted(
	observation UnifiedAgentTaskCommittedObservation,
) {
	if o == nil {
		return
	}
	profile := boundedUnifiedExecutionProfile(observation.ExecutionProfile)
	strategy := boundedUnifiedStrategy(observation.Strategy)
	outcome := boundedUnifiedOutcome(observation.Status)
	baseLabels := []string{profile, strategy, outcome}

	o.outcomes.WithLabelValues(baseLabels...).Inc()
	if observation.Duration >= 0 {
		o.duration.WithLabelValues(baseLabels...).Observe(observation.Duration.Seconds())
	}
	if observation.StepCount >= 0 {
		o.steps.WithLabelValues(baseLabels...).Observe(float64(observation.StepCount))
	}

	estimatedUsage := strconv.FormatBool(observation.Usage.Estimated)
	if observation.Usage.InputTokens > 0 {
		o.tokens.WithLabelValues(profile, strategy, outcome, "input", estimatedUsage).
			Add(float64(observation.Usage.InputTokens))
	}
	if observation.Usage.OutputTokens > 0 {
		o.tokens.WithLabelValues(profile, strategy, outcome, "output", estimatedUsage).
			Add(float64(observation.Usage.OutputTokens))
	}
	if observation.Usage.TotalTokens > 0 {
		o.tokens.WithLabelValues(profile, strategy, outcome, "total", estimatedUsage).
			Add(float64(observation.Usage.TotalTokens))
	}
	if observation.Usage.EstimatedCostMicros > 0 {
		o.cost.WithLabelValues(
			profile,
			strategy,
			outcome,
			strconv.FormatBool(observation.Usage.CostEstimated),
		).Add(float64(observation.Usage.EstimatedCostMicros))
	}

	for _, activity := range observation.ToolActivities {
		o.toolCalls.WithLabelValues(
			profile,
			strategy,
			outcome,
			boundedUnifiedToolResult(activity.Status),
		).Inc()
	}
	for _, citation := range observation.Citations {
		validity := "invalid"
		if validUnifiedMetricCitation(citation) {
			validity = "valid"
		}
		o.citations.WithLabelValues(
			profile,
			strategy,
			outcome,
			boundedUnifiedCitationSource(citation.SourceType),
			validity,
		).Inc()
	}
	if observation.Status == repository.AgentExecutionRunCompleted && observation.PublishableDraft {
		o.draftEvents.WithLabelValues(profile, strategy, "ready").Inc()
	}
}

func (o *PrometheusUnifiedAgentProductObserver) ObserveDraftPublished(
	observation UnifiedAgentDraftPublishedObservation,
) {
	if o == nil {
		return
	}
	o.draftEvents.WithLabelValues(
		boundedUnifiedExecutionProfile(observation.ExecutionProfile),
		boundedUnifiedStrategy(observation.Strategy),
		"published",
	).Inc()
}

func boundedUnifiedExecutionProfile(value string) string {
	switch strings.TrimSpace(value) {
	case ExecutionProfileRuntimeChat,
		ExecutionProfileRuntimeDraft,
		ExecutionProfileCompatChat,
		ExecutionProfileCompatConsult,
		ExecutionProfileCompatAssist,
		ExecutionProfileRuntimeResearchDraft,
		ExecutionProfileRuntimeWebSearch,
		ExecutionProfileRuntimeWebDraft,
		ExecutionProfileRuntimeExternalMCP,
		ExecutionProfileRuntimeWorkflow,
		ExecutionProfileRuntimeSkill:
		return strings.TrimSpace(value)
	default:
		return "unknown"
	}
}

func boundedUnifiedStrategy(value agentStrategy.Kind) string {
	switch value {
	case agentStrategy.KindSingleAgent, agentStrategy.KindMultiAgent:
		return string(value)
	default:
		return "unknown"
	}
}

func boundedUnifiedOutcome(value repository.AgentExecutionRunStatus) string {
	switch value {
	case repository.AgentExecutionRunCompleted,
		repository.AgentExecutionRunAwaitingHuman,
		repository.AgentExecutionRunApprovalRequired,
		repository.AgentExecutionRunFailed,
		repository.AgentExecutionRunCanceled:
		return string(value)
	default:
		return "unknown"
	}
}

func boundedUnifiedToolResult(value string) string {
	switch strings.TrimSpace(value) {
	case AgentToolActivitySucceeded, AgentToolActivityFailed, AgentToolActivityPending:
		return strings.TrimSpace(value)
	default:
		return "unknown"
	}
}

func boundedUnifiedCitationSource(value string) string {
	switch strings.TrimSpace(value) {
	case AgentCitationPlatformTweet, AgentCitationWebPage:
		return strings.TrimSpace(value)
	default:
		return "unknown"
	}
}

func validUnifiedMetricCitation(citation AgentCitation) bool {
	sourceID := strings.TrimSpace(citation.SourceID)
	citationID := strings.TrimSpace(citation.CitationID)
	resourceURL := strings.TrimSpace(citation.URL)
	if sourceID == "" || citationID == "" || resourceURL == "" {
		return false
	}

	switch strings.TrimSpace(citation.SourceType) {
	case AgentCitationPlatformTweet:
		tweetID, err := strconv.ParseUint(sourceID, 10, 64)
		return err == nil && tweetID > 0 &&
			citationID == AgentCitationPlatformTweet+":"+sourceID &&
			validPlatformTweetCitationURL(resourceURL, sourceID)
	case AgentCitationWebPage:
		parsed, err := url.Parse(resourceURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
			parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
			return false
		}
		digest := sha256.Sum256([]byte(parsed.String()))
		expectedSourceID := fmt.Sprintf("%x", digest[:12])
		return sourceID == expectedSourceID && citationID == AgentCitationWebPage+":"+sourceID
	default:
		return false
	}
}
