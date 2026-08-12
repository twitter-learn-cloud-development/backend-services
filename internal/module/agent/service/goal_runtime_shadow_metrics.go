package service

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
)

type PrometheusGoalRuntimeShadowObserver struct {
	evaluations *prometheus.CounterVec
}

func NewPrometheusGoalRuntimeShadowObserver(
	registerer prometheus.Registerer,
) (*PrometheusGoalRuntimeShadowObserver, error) {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}
	observer := &PrometheusGoalRuntimeShadowObserver{
		evaluations: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_goal_runtime_shadow_evaluations_total",
			Help: "Goal Runtime shadow comparisons over already executed legacy results.",
		}, []string{"capability", "legacy_outcome", "goal_outcome", "evidence_comparison"}),
	}
	if err := registerer.Register(observer.evaluations); err != nil {
		return nil, fmt.Errorf("register Goal Runtime shadow metrics: %w", err)
	}
	return observer, nil
}

func (o *PrometheusGoalRuntimeShadowObserver) ObserveGoalRuntimeShadow(
	observation GoalRuntimeShadowObservation,
) {
	if o == nil {
		return
	}
	o.evaluations.WithLabelValues(
		boundedGoalShadowCapability(observation.Capability),
		boundedGoalShadowLegacyOutcome(observation.LegacyOutcome),
		boundedGoalShadowGoalOutcome(string(observation.GoalOutcome)),
		boundedGoalShadowEvidenceComparison(observation.EvidenceComparison),
	).Inc()
}

func boundedGoalShadowCapability(value string) string {
	if value == CapabilityPlatformSearch || value == CapabilityWebSearch ||
		value == CapabilityContentDraft || value == GoalShadowCapabilityResearchDraft {
		return value
	}
	return "unknown"
}

func boundedGoalShadowLegacyOutcome(value string) string {
	switch value {
	case GoalShadowLegacyCompleted,
		GoalShadowLegacyEvidenceMissing,
		GoalShadowLegacyFailed,
		GoalShadowLegacySuspended:
		return value
	default:
		return GoalShadowLegacyUnknown
	}
}

func boundedGoalShadowGoalOutcome(value string) string {
	switch value {
	case "passed", "failed", "inconclusive":
		return value
	default:
		return "unknown"
	}
}

func boundedGoalShadowEvidenceComparison(value string) string {
	switch value {
	case GoalShadowComparisonConsistent,
		GoalShadowComparisonLegacyOnly,
		GoalShadowComparisonGoalOnly,
		GoalShadowComparisonMissingBoth,
		GoalShadowComparisonExecutionIncomplete,
		GoalShadowComparisonEvaluatorError:
		return value
	default:
		return "unknown"
	}
}
