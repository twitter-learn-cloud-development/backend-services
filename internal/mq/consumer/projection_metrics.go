package consumer

import (
	"context"
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"twitter-clone/internal/domain"
)

type ProjectionObserver interface {
	ObserveProjection(string, string, time.Duration)
	SetProjectionBacklog([]domain.ProjectionBacklog, time.Time)
	ObserveBacklogFailure()
}
type PrometheusProjectionObserver struct {
	operations      *prometheus.CounterVec
	duration        *prometheus.HistogramVec
	backlog, age    *prometheus.GaugeVec
	refreshFailures prometheus.Counter
}

func NewPrometheusProjectionObserver(registerer prometheus.Registerer) (*PrometheusProjectionObserver, error) {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}
	o := &PrometheusProjectionObserver{
		operations:      prometheus.NewCounterVec(prometheus.CounterOpts{Name: "tweet_projection_tasks_total", Help: "Projection executions by fixed kind and result."}, []string{"kind", "result"}),
		duration:        prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "tweet_projection_duration_seconds", Help: "Projection execution duration.", Buckets: []float64{.01, .1, .5, 1, 5, 15, 30, 60}}, []string{"kind"}),
		backlog:         prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "tweet_projection_backlog", Help: "Persistent active and exhausted projection task count."}, []string{"kind", "state"}),
		age:             prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "tweet_projection_oldest_seconds", Help: "Age of oldest unfinished projection task."}, []string{"kind", "state"}),
		refreshFailures: prometheus.NewCounter(prometheus.CounterOpts{Name: "tweet_projection_backlog_refresh_failures_total", Help: "Failed persistent projection backlog refreshes."}),
	}
	for _, collector := range []prometheus.Collector{o.operations, o.duration, o.backlog, o.age, o.refreshFailures} {
		if err := registerer.Register(collector); err != nil {
			return nil, err
		}
	}
	return o, nil
}
func projectionKind(kind string) string {
	switch kind {
	case domain.ProjectionSyncText, domain.ProjectionSyncVector, syncESOutboxTaskType, deleteESOutboxTaskType, deleteVectorOutboxTaskType:
		return kind
	default:
		return "unknown"
	}
}
func projectionState(state string) string {
	switch state {
	case "pending", "processing", "failed", "exhausted":
		return state
	default:
		return "unknown"
	}
}
func (o *PrometheusProjectionObserver) ObserveProjection(kind, result string, duration time.Duration) {
	if result != "succeeded" && result != "cancelled" {
		result = "failed"
	}
	o.operations.WithLabelValues(projectionKind(kind), result).Inc()
	o.duration.WithLabelValues(projectionKind(kind)).Observe(duration.Seconds())
}
func (o *PrometheusProjectionObserver) SetProjectionBacklog(rows []domain.ProjectionBacklog, now time.Time) {
	// Reset known buckets only; collectors retain their concurrency-safe values.
	for _, kind := range []string{syncESOutboxTaskType, domain.ProjectionSyncText, domain.ProjectionSyncVector, deleteESOutboxTaskType, deleteVectorOutboxTaskType, "unknown"} {
		for _, state := range []string{"pending", "processing", "failed", "exhausted", "unknown"} {
			o.backlog.WithLabelValues(kind, state).Set(0)
			o.age.WithLabelValues(kind, state).Set(0)
		}
	}
	for _, row := range rows {
		age := float64(now.UnixMilli()-row.OldestAt) / 1000
		if age < 0 {
			age = 0
		}
		o.backlog.WithLabelValues(projectionKind(row.TaskType), projectionState(row.State)).Set(float64(row.Count))
		o.age.WithLabelValues(projectionKind(row.TaskType), projectionState(row.State)).Set(age)
	}
}
func (o *PrometheusProjectionObserver) ObserveBacklogFailure() { o.refreshFailures.Inc() }
func (c *TimelineConsumer) SetProjectionObserver(observer ProjectionObserver) {
	c.projectionObserver = observer
}
func (c *TimelineConsumer) observeProjection(kind string, err error, duration time.Duration) {
	if c.projectionObserver == nil {
		return
	}
	result := "succeeded"
	if err != nil {
		result = "failed"
	}
	if errors.Is(err, context.Canceled) {
		result = "cancelled"
	}
	c.projectionObserver.ObserveProjection(kind, result, duration)
}
func (c *TimelineConsumer) refreshProjectionBacklog(parent context.Context) {
	if c.projectionObserver == nil {
		return
	}
	repo, ok := c.outboxRepo.(domain.ProjectionBacklogRepository)
	if !ok {
		c.projectionObserver.ObserveBacklogFailure()
		return
	}
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	rows, err := repo.ProjectionBacklog(ctx)
	if err != nil {
		if parent.Err() == nil {
			c.projectionObserver.ObserveBacklogFailure()
		}
		return
	}
	c.projectionObserver.SetProjectionBacklog(rows, time.Now())
}
