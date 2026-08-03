package consumer

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	tweetCreatedStageFanout = "fanout"
	tweetCreatedStageTrends = "trends"
	tweetCreatedStageOutbox = "sync_es_outbox"
	tweetCreatedStageAck    = "ack"
)

type TweetCreatedObserver interface {
	ObserveStage(stage, result string)
}

type noopTweetCreatedObserver struct{}

func (noopTweetCreatedObserver) ObserveStage(string, string) {}

type PrometheusTweetCreatedObserver struct {
	stages *prometheus.CounterVec
}

func NewPrometheusTweetCreatedObserver(registerer prometheus.Registerer) (*PrometheusTweetCreatedObserver, error) {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}
	observer := &PrometheusTweetCreatedObserver{
		stages: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "timeline_tweet_created_stage_total",
			Help: "Tweet-created processing stages by bounded result.",
		}, []string{"stage", "result"}),
	}
	if err := registerer.Register(observer.stages); err != nil {
		return nil, fmt.Errorf("register tweet-created metrics: %w", err)
	}
	return observer, nil
}

func (o *PrometheusTweetCreatedObserver) ObserveStage(stage, result string) {
	if o == nil {
		return
	}
	o.stages.WithLabelValues(boundedTweetCreatedStage(stage), boundedTweetCreatedResult(result)).Inc()
}

func boundedTweetCreatedStage(stage string) string {
	switch stage {
	case tweetCreatedStageFanout, tweetCreatedStageTrends, tweetCreatedStageOutbox, tweetCreatedStageAck:
		return stage
	default:
		return "unknown"
	}
}

func boundedTweetCreatedResult(result string) string {
	switch result {
	case "applied", "duplicate", "failed", "completed", "retried", "dlq", "requeued", "acknowledgement_uncertain":
		return result
	default:
		return "unknown"
	}
}
