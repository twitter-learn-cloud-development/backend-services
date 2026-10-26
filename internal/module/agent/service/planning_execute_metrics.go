package service

import (
	"strings"
	"time"

	agentRuntime "twitter-clone/internal/module/agent/runtime"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	planningExecutePhaseInitial = "initial"
	planningExecutePhaseResume  = "resume"
)

type PlanningExecuteObservation struct {
	Phase            string
	ExecutionProfile string
	Outcome          string
	Verification     string
	PlanningCalls    int
	AdmissionRepairs int
	RecoveryAttempts int
	Duration         time.Duration
}

type PlanningExecuteObserver interface {
	ObservePlanningExecute(PlanningExecuteObservation)
}

type noopPlanningExecuteObserver struct{}

func (noopPlanningExecuteObserver) ObservePlanningExecute(PlanningExecuteObservation) {}

type PrometheusPlanningExecuteObserver struct {
	runs      *prometheus.CounterVec
	calls     *prometheus.CounterVec
	repairs   *prometheus.CounterVec
	durations *prometheus.HistogramVec
}

func NewPrometheusPlanningExecuteObserver(
	registerer prometheus.Registerer,
) (*PrometheusPlanningExecuteObserver, error) {
	observer := &PrometheusPlanningExecuteObserver{
		runs: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_planning_execute_runs_total",
			Help: "Planning-Execute runs by bounded lifecycle outcome.",
		}, []string{"phase", "execution_profile", "outcome", "verification"}),
		calls: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_planning_execute_planning_calls_total",
			Help: "Model planning calls made by Planning-Execute runs.",
		}, []string{"phase", "execution_profile"}),
		repairs: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_planning_execute_repairs_total",
			Help: "Planning-Execute admission and recovery repairs.",
		}, []string{"execution_profile", "kind"}),
		durations: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "agent_planning_execute_duration_seconds",
			Help:    "Planning-Execute lifecycle duration in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{"phase", "execution_profile", "outcome"}),
	}
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}
	for _, collector := range []prometheus.Collector{observer.runs, observer.calls, observer.repairs, observer.durations} {
		if err := registerer.Register(collector); err != nil {
			return nil, err
		}
	}
	return observer, nil
}

func (observer *PrometheusPlanningExecuteObserver) ObservePlanningExecute(observation PlanningExecuteObservation) {
	if observer == nil {
		return
	}
	phase := boundedPlanningPhase(observation.Phase)
	profile := boundedPlanningProfile(observation.ExecutionProfile)
	outcome := boundedPlanningOutcome(observation.Outcome)
	verification := boundedPlanningVerification(observation.Verification)
	observer.runs.WithLabelValues(phase, profile, outcome, verification).Inc()
	if observation.PlanningCalls > 0 {
		observer.calls.WithLabelValues(phase, profile).Add(float64(observation.PlanningCalls))
	}
	if observation.AdmissionRepairs > 0 {
		observer.repairs.WithLabelValues(profile, "admission").Add(float64(observation.AdmissionRepairs))
	}
	if observation.RecoveryAttempts > 0 {
		observer.repairs.WithLabelValues(profile, "recovery").Add(float64(observation.RecoveryAttempts))
	}
	if observation.Duration >= 0 {
		observer.durations.WithLabelValues(phase, profile, outcome).Observe(observation.Duration.Seconds())
	}
}

func WithPlanningExecuteObserver(observer PlanningExecuteObserver) Option {
	return func(service *AgentService) {
		if observer != nil {
			service.planningExecuteObserver = observer
		}
	}
}

func (s *AgentService) observePlanningExecuteResult(
	phase string,
	executionProfile string,
	planningCalls int,
	admissionRepairs int,
	recoveryAttempts int,
	verified agentRuntime.VerifiedRunResult,
	err error,
	started time.Time,
) {
	if s == nil || s.planningExecuteObserver == nil {
		return
	}
	outcome := "error"
	if err == nil {
		switch verified.Status {
		case agentRuntime.GoalRunVerified:
			outcome = "verified"
		case agentRuntime.GoalRunBlocked:
			outcome = "blocked"
		case agentRuntime.GoalRunSuspended:
			outcome = "suspended"
		}
	}
	s.planningExecuteObserver.ObservePlanningExecute(PlanningExecuteObservation{
		Phase: phase, ExecutionProfile: executionProfile, Outcome: outcome,
		Verification: string(verified.Verification.Status), PlanningCalls: planningCalls,
		AdmissionRepairs: admissionRepairs, RecoveryAttempts: recoveryAttempts,
		Duration: time.Since(started),
	})
}

func boundedPlanningPhase(value string) string {
	switch strings.TrimSpace(value) {
	case planningExecutePhaseInitial, planningExecutePhaseResume:
		return strings.TrimSpace(value)
	default:
		return "unknown"
	}
}

func boundedPlanningProfile(value string) string {
	switch strings.TrimSpace(value) {
	case ExecutionProfileRuntimePlatformSearch, ExecutionProfileRuntimeWebSearch,
		ExecutionProfileRuntimeExternalMCP, ExecutionProfileRuntimeWorkflow:
		return strings.TrimSpace(value)
	default:
		return "other"
	}
}

func boundedPlanningOutcome(value string) string {
	switch strings.TrimSpace(value) {
	case "verified", "blocked", "suspended", "error":
		return strings.TrimSpace(value)
	default:
		return "error"
	}
}

func boundedPlanningVerification(value string) string {
	switch agentRuntime.VerificationStatus(strings.TrimSpace(value)) {
	case agentRuntime.VerificationPassed, agentRuntime.VerificationFailed,
		agentRuntime.VerificationInconclusive:
		return strings.TrimSpace(value)
	default:
		return "unknown"
	}
}
