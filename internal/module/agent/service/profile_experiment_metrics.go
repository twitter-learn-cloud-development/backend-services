package service

import (
	"fmt"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"twitter-clone/internal/module/agent/profile"
)

type ProfileExperimentObserver interface {
	ObserveObservation(arm string, success bool)
	ObserveOutcome(arm string, positive, replay bool)
	ObserveProductOutcome(result string)
	ObserveDecision(status, decision string)
}

type noopProfileExperimentObserver struct{}

func (noopProfileExperimentObserver) ObserveObservation(string, bool)   {}
func (noopProfileExperimentObserver) ObserveOutcome(string, bool, bool) {}
func (noopProfileExperimentObserver) ObserveProductOutcome(string)      {}
func (noopProfileExperimentObserver) ObserveDecision(string, string)    {}

type PrometheusProfileExperimentObserver struct {
	observations *prometheus.CounterVec
	outcomes     *prometheus.CounterVec
	product      *prometheus.CounterVec
	decisions    *prometheus.CounterVec
}

func NewPrometheusProfileExperimentObserver(registerer prometheus.Registerer) (*PrometheusProfileExperimentObserver, error) {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}
	observer := &PrometheusProfileExperimentObserver{
		observations: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_profile_experiment_observation_record_attempts_total",
			Help: "Accepted Profile experiment observation record attempts by bounded arm and outcome.",
		}, []string{"arm", "outcome"}),
		outcomes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_profile_experiment_business_outcome_record_attempts_total",
			Help: "Accepted Profile experiment business outcome records by bounded arm, value and idempotency result.",
		}, []string{"arm", "outcome", "result"}),
		product: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_profile_experiment_product_outcome_attribution_attempts_total",
			Help: "Trusted product outcome attribution attempts by bounded result.",
		}, []string{"result"}),
		decisions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_profile_experiment_decisions_total",
			Help: "Terminal Profile experiment decisions without profile or experiment identifiers.",
		}, []string{"status", "decision"}),
	}
	if err := registerer.Register(observer.observations); err != nil {
		return nil, fmt.Errorf("register profile experiment observation metrics: %w", err)
	}
	if err := registerer.Register(observer.outcomes); err != nil {
		registerer.Unregister(observer.observations)
		return nil, fmt.Errorf("register profile experiment business outcome metrics: %w", err)
	}
	if err := registerer.Register(observer.product); err != nil {
		registerer.Unregister(observer.observations)
		registerer.Unregister(observer.outcomes)
		return nil, fmt.Errorf("register profile experiment product outcome metrics: %w", err)
	}
	if err := registerer.Register(observer.decisions); err != nil {
		registerer.Unregister(observer.observations)
		registerer.Unregister(observer.outcomes)
		registerer.Unregister(observer.product)
		return nil, fmt.Errorf("register profile experiment decision metrics: %w", err)
	}
	return observer, nil
}

func (o *PrometheusProfileExperimentObserver) ObserveProductOutcome(result string) {
	if o == nil {
		return
	}
	o.product.WithLabelValues(boundedProductOutcomeResult(result)).Inc()
}

func (o *PrometheusProfileExperimentObserver) ObserveOutcome(arm string, positive, replay bool) {
	if o == nil {
		return
	}
	outcome := "negative"
	if positive {
		outcome = "positive"
	}
	result := "recorded"
	if replay {
		result = "replayed"
	}
	o.outcomes.WithLabelValues(boundedProfileExperimentArm(arm), outcome, result).Inc()
}

func (o *PrometheusProfileExperimentObserver) ObserveObservation(arm string, success bool) {
	if o == nil {
		return
	}
	outcome := "failed"
	if success {
		outcome = "success"
	}
	o.observations.WithLabelValues(boundedProfileExperimentArm(arm), outcome).Inc()
}

func (o *PrometheusProfileExperimentObserver) ObserveDecision(status, decision string) {
	if o == nil {
		return
	}
	o.decisions.WithLabelValues(boundedProfileExperimentStatus(status), boundedProfileExperimentDecision(decision)).Inc()
}

func boundedProfileExperimentArm(value string) string {
	switch strings.TrimSpace(value) {
	case profile.ExperimentArmStable, profile.ExperimentArmCandidate:
		return strings.TrimSpace(value)
	default:
		return "unknown"
	}
}

func boundedProfileExperimentStatus(value string) string {
	switch strings.TrimSpace(value) {
	case profile.ExperimentStatusPassed, profile.ExperimentStatusRolledBack, profile.ExperimentStatusStopped, profile.ExperimentStatusSuperseded:
		return strings.TrimSpace(value)
	default:
		return "unknown"
	}
}

func boundedProfileExperimentDecision(value string) string {
	switch strings.TrimSpace(value) {
	case profile.ExperimentDecisionPass, profile.ExperimentDecisionRollback, profile.ExperimentDecisionStop, profile.ExperimentDecisionSuperseded:
		return strings.TrimSpace(value)
	default:
		return "unknown"
	}
}

func boundedProductOutcomeResult(value string) string {
	switch strings.TrimSpace(value) {
	case "recorded", "replayed", "not_applicable", "failed":
		return strings.TrimSpace(value)
	default:
		return "unknown"
	}
}
