package service

import (
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"twitter-clone/internal/module/agent/workflow/rag"
)

const (
	sessionSummaryOutcomeCompleted = "completed"
	sessionSummaryOutcomeNoop      = "noop"
	sessionSummaryOutcomeFailed    = "failed"

	semanticContextOutcomeCompleted   = "completed"
	semanticContextOutcomePersonaOnly = "persona_only"
	semanticContextOutcomeDisabled    = "disabled"
	semanticContextOutcomeFailed      = "failed"
)

type SessionSummaryObservation struct {
	Trigger  string
	Outcome  string
	Duration time.Duration
}

type SemanticContextObservation struct {
	Intent     rag.Intent
	Outcome    string
	ChunkCount int
	Duration   time.Duration
}

type SessionSummaryObserver interface {
	ObserveSessionSummary(SessionSummaryObservation)
	ObserveSemanticContext(SemanticContextObservation)
}

type noopSessionSummaryObserver struct{}

func (noopSessionSummaryObserver) ObserveSessionSummary(SessionSummaryObservation)   {}
func (noopSessionSummaryObserver) ObserveSemanticContext(SemanticContextObservation) {}

type PrometheusSessionSummaryObserver struct {
	summaryAttempts  *prometheus.CounterVec
	summaryDuration  *prometheus.HistogramVec
	retrievals       *prometheus.CounterVec
	retrievedChunks  *prometheus.HistogramVec
	retrievalLatency *prometheus.HistogramVec
}

func NewPrometheusSessionSummaryObserver(registerer prometheus.Registerer) (*PrometheusSessionSummaryObserver, error) {
	observer := &PrometheusSessionSummaryObserver{
		summaryAttempts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_session_summary_attempts_total",
			Help: "Session summary attempts by bounded trigger and outcome.",
		}, []string{"trigger", "outcome"}),
		summaryDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "agent_session_summary_duration_seconds",
			Help:    "Session summary attempt duration in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{"trigger", "outcome"}),
		retrievals: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_semantic_context_retrievals_total",
			Help: "Semantic context retrievals by bounded intent and outcome.",
		}, []string{"intent", "outcome"}),
		retrievedChunks: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "agent_semantic_context_retrieved_chunks",
			Help:    "Number of bounded semantic context chunks returned per retrieval.",
			Buckets: []float64{0, 1, 2, 3, 5, 8, 13},
		}, []string{"intent", "outcome"}),
		retrievalLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "agent_semantic_context_retrieval_duration_seconds",
			Help:    "Semantic context retrieval duration in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{"intent", "outcome"}),
	}
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}
	for _, collector := range []prometheus.Collector{
		observer.summaryAttempts,
		observer.summaryDuration,
		observer.retrievals,
		observer.retrievedChunks,
		observer.retrievalLatency,
	} {
		if err := registerer.Register(collector); err != nil {
			return nil, err
		}
	}
	return observer, nil
}

func (o *PrometheusSessionSummaryObserver) ObserveSessionSummary(observation SessionSummaryObservation) {
	if o == nil {
		return
	}
	trigger := boundedSessionSummaryTrigger(observation.Trigger)
	outcome := boundedSessionSummaryOutcome(observation.Outcome)
	o.summaryAttempts.WithLabelValues(trigger, outcome).Inc()
	o.summaryDuration.WithLabelValues(trigger, outcome).Observe(nonNegativeDuration(observation.Duration).Seconds())
}

func (o *PrometheusSessionSummaryObserver) ObserveSemanticContext(observation SemanticContextObservation) {
	if o == nil {
		return
	}
	intent := boundedSemanticIntent(observation.Intent)
	outcome := boundedSemanticContextOutcome(observation.Outcome)
	chunks := observation.ChunkCount
	if chunks < 0 {
		chunks = 0
	}
	o.retrievals.WithLabelValues(intent, outcome).Inc()
	o.retrievedChunks.WithLabelValues(intent, outcome).Observe(float64(chunks))
	o.retrievalLatency.WithLabelValues(intent, outcome).Observe(nonNegativeDuration(observation.Duration).Seconds())
}

func (s *AgentService) observeSessionSummary(trigger, outcome string, duration time.Duration) {
	if s == nil || s.summaryObserver == nil {
		return
	}
	s.summaryObserver.ObserveSessionSummary(SessionSummaryObservation{
		Trigger: trigger, Outcome: outcome, Duration: duration,
	})
}

func (s *AgentService) observeSemanticContext(intent rag.Intent, outcome string, chunks int, duration time.Duration) {
	if s == nil || s.summaryObserver == nil {
		return
	}
	s.summaryObserver.ObserveSemanticContext(SemanticContextObservation{
		Intent: intent, Outcome: outcome, ChunkCount: chunks, Duration: duration,
	})
}

func boundedSessionSummaryTrigger(value string) string {
	switch strings.TrimSpace(value) {
	case sessionSummaryTriggerThreshold, sessionSummaryTriggerIdle,
		sessionSummaryTriggerRecovery, sessionSummaryTriggerSessionEnd:
		return strings.TrimSpace(value)
	default:
		return "unknown"
	}
}

func boundedSessionSummaryOutcome(value string) string {
	switch strings.TrimSpace(value) {
	case sessionSummaryOutcomeCompleted, sessionSummaryOutcomeNoop, sessionSummaryOutcomeFailed:
		return strings.TrimSpace(value)
	default:
		return sessionSummaryOutcomeFailed
	}
}

func boundedSemanticIntent(value rag.Intent) string {
	switch value {
	case rag.IntentPersonaMemory, rag.IntentEpisodicMemory, rag.IntentGlobalKnowledge, rag.IntentDirectAction:
		return string(value)
	default:
		return "unknown"
	}
}

func boundedSemanticContextOutcome(value string) string {
	switch strings.TrimSpace(value) {
	case semanticContextOutcomeCompleted, semanticContextOutcomePersonaOnly,
		semanticContextOutcomeDisabled, semanticContextOutcomeFailed:
		return strings.TrimSpace(value)
	default:
		return semanticContextOutcomeFailed
	}
}

func nonNegativeDuration(value time.Duration) time.Duration {
	if value < 0 {
		return 0
	}
	return value
}
