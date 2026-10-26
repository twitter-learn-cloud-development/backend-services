package admission

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct{ events *prometheus.CounterVec }

func NewMetrics(reg prometheus.Registerer) (*Metrics, error) {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	m := &Metrics{events: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "agent_shared_admission_total", Help: "Shared runtime and provider lease outcomes."}, []string{"kind", "result"})}
	if err := reg.Register(m.events); err != nil {
		return nil, err
	}
	return m, nil
}
func (m *Metrics) Observe(kind, result string) {
	if kind != "run" && kind != "provider" {
		kind = "unknown"
	}
	switch result {
	case "acquired", "released", "limited", "budget_limited", "unavailable", "lease_lost", "release_uncertain":
	default:
		result = "unknown"
	}
	m.events.WithLabelValues(kind, result).Inc()
}
