package projection

import (
	"context"
	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	Events  *prometheus.CounterVec
	Latency *prometheus.HistogramVec
	Backlog *prometheus.GaugeVec
	Age     *prometheus.GaugeVec
}

func NewMetrics(reg prometheus.Registerer) (*Metrics, error) {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	m := &Metrics{Events: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "follow_projection_events_total", Help: "Bounded follow projection outcomes."}, []string{"kind", "result"}), Latency: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "follow_projection_page_seconds", Help: "Follow projection page latency."}, []string{"kind"}), Backlog: prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "follow_projection_backlog", Help: "Persistent active follow tasks by state."}, []string{"kind", "state"}), Age: prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "follow_projection_oldest_seconds", Help: "Oldest persistent active follow task age."}, []string{"kind", "state"})}
	for _, c := range []prometheus.Collector{m.Events, m.Latency, m.Backlog, m.Age} {
		if err := reg.Register(c); err != nil {
			return nil, err
		}
	}
	return m, nil
}
func metricKind(kind string) string {
	switch kind {
	case PairTask, AuthorTask, "ingress", "worker":
		return kind
	default:
		return "unknown"
	}
}
func (m *Metrics) Observe(kind, result string) {
	switch result {
	case "failed", "completed", "yielded", "waiting", "cancelled", "interrupted", "registered", "retried", "dlq", "ack_uncertain", "confirm_failed", "recover_failed", "claim_failed", "finalize_failed", "backlog_failed":
	default:
		result = "unknown"
	}
	m.Events.WithLabelValues(metricKind(kind), result).Inc()
}
func (m *Metrics) Duration(kind string, seconds float64) {
	m.Latency.WithLabelValues(metricKind(kind)).Observe(seconds)
}

type BacklogRow struct {
	TaskType, State string
	Count, OldestAt int64
}

func (s *SQLStore) Backlog(ctx context.Context) ([]BacklogRow, error) {
	rows := []BacklogRow{}
	err := s.DB.WithContext(ctx).Raw("SELECT /*+ MAX_EXECUTION_TIME(2000) */ task_type,CASE WHEN status=3 THEN 'processing' WHEN status=2 AND retries>=max_retries THEN 'exhausted' WHEN status=2 THEN 'failed' ELSE 'pending' END state,COUNT(*) count,MIN(created_at) oldest_at FROM outbox_tasks WHERE task_type IN ('follow_pair_v1','follow_author_v1') AND status IN (0,2,3) GROUP BY task_type,state").Scan(&rows).Error
	return rows, err
}
func (m *Metrics) SetBacklog(rows []BacklogRow, now int64) {
	for _, kind := range []string{PairTask, AuthorTask} {
		for _, state := range []string{"pending", "processing", "failed", "exhausted"} {
			m.Backlog.WithLabelValues(kind, state).Set(0)
			m.Age.WithLabelValues(kind, state).Set(0)
		}
	}
	for _, row := range rows {
		if row.TaskType != PairTask && row.TaskType != AuthorTask {
			continue
		}
		switch row.State {
		case "pending", "processing", "failed", "exhausted":
		default:
			continue
		}
		age := float64(now-row.OldestAt) / 1000
		if age < 0 {
			age = 0
		}
		m.Backlog.WithLabelValues(row.TaskType, row.State).Set(float64(row.Count))
		m.Age.WithLabelValues(row.TaskType, row.State).Set(age)
	}
}
