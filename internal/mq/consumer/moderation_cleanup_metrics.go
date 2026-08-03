package consumer

import (
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type ModerationCleanupObserver interface {
	ObserveEvent(result string)
	ObservePage(result string, scanned, removed int)
	ObserveDuration(result string, duration time.Duration)
}

type noopModerationCleanupObserver struct{}

func (noopModerationCleanupObserver) ObserveEvent(string)                   {}
func (noopModerationCleanupObserver) ObservePage(string, int, int)          {}
func (noopModerationCleanupObserver) ObserveDuration(string, time.Duration) {}

type PrometheusModerationCleanupObserver struct {
	events    *prometheus.CounterVec
	pages     *prometheus.CounterVec
	followers *prometheus.CounterVec
	duration  *prometheus.HistogramVec
}

func NewPrometheusModerationCleanupObserver(registerer prometheus.Registerer) (*PrometheusModerationCleanupObserver, error) {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}
	observer := &PrometheusModerationCleanupObserver{
		events: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "timeline_moderation_cleanup_events_total",
			Help: "Moderation cleanup events by bounded processing result.",
		}, []string{"result"}),
		pages: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "timeline_moderation_cleanup_pages_total",
			Help: "Moderation cleanup follower pages by bounded processing result.",
		}, []string{"result"}),
		followers: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "timeline_moderation_cleanup_followers_total",
			Help: "Followers scanned and timelines removed during moderation cleanup.",
		}, []string{"kind"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "timeline_moderation_cleanup_duration_seconds",
			Help:    "Moderation cleanup delivery duration by bounded result.",
			Buckets: prometheus.DefBuckets,
		}, []string{"result"}),
	}
	collectors := []prometheus.Collector{observer.events, observer.pages, observer.followers, observer.duration}
	registered := make([]prometheus.Collector, 0, len(collectors))
	for _, collector := range collectors {
		if err := registerer.Register(collector); err != nil {
			for _, previous := range registered {
				registerer.Unregister(previous)
			}
			return nil, fmt.Errorf("register moderation cleanup metrics: %w", err)
		}
		registered = append(registered, collector)
	}
	return observer, nil
}

func (o *PrometheusModerationCleanupObserver) ObserveEvent(result string) {
	if o == nil {
		return
	}
	o.events.WithLabelValues(boundedModerationEventResult(result)).Inc()
}

func (o *PrometheusModerationCleanupObserver) ObservePage(result string, scanned, removed int) {
	if o == nil {
		return
	}
	o.pages.WithLabelValues(boundedModerationPageResult(result)).Inc()
	if scanned > 0 {
		o.followers.WithLabelValues("scanned").Add(float64(scanned))
	}
	if removed > 0 {
		o.followers.WithLabelValues("removed").Add(float64(removed))
	}
}

func (o *PrometheusModerationCleanupObserver) ObserveDuration(result string, duration time.Duration) {
	if o == nil {
		return
	}
	o.duration.WithLabelValues(boundedModerationDurationResult(result)).Observe(duration.Seconds())
}

func boundedModerationEventResult(result string) string {
	switch result {
	case "received", "completed", "duplicate", "malformed", "retried", "dlq", "requeued", "acknowledgement_uncertain":
		return result
	default:
		return "unknown"
	}
}

func boundedModerationPageResult(result string) string {
	switch result {
	case "completed", "failed":
		return result
	default:
		return "unknown"
	}
}

func boundedModerationDurationResult(result string) string {
	switch result {
	case "completed", "duplicate", "failed":
		return result
	default:
		return "unknown"
	}
}
