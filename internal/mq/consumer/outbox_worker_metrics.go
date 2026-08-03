package consumer

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	outboxOperationClaim    = "claim"
	outboxOperationRecover  = "recover"
	outboxOperationExecute  = "execute"
	outboxOperationFinalize = "finalize"
	outboxOperationCleanup  = "cleanup"
)

type OutboxWorkerObserver interface {
	ObserveOutbox(operation, result string, count int)
}

type noopOutboxWorkerObserver struct{}

func (noopOutboxWorkerObserver) ObserveOutbox(string, string, int) {}

type PrometheusOutboxWorkerObserver struct {
	operations *prometheus.CounterVec
}

func NewPrometheusOutboxWorkerObserver(registerer prometheus.Registerer) (*PrometheusOutboxWorkerObserver, error) {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}
	observer := &PrometheusOutboxWorkerObserver{
		operations: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "timeline_outbox_worker_operations_total",
			Help: "Timeline Outbox worker operations by bounded result.",
		}, []string{"operation", "result"}),
	}
	if err := registerer.Register(observer.operations); err != nil {
		return nil, fmt.Errorf("register timeline outbox worker metrics: %w", err)
	}
	return observer, nil
}

func (o *PrometheusOutboxWorkerObserver) ObserveOutbox(operation, result string, count int) {
	if o == nil || count <= 0 {
		return
	}
	o.operations.WithLabelValues(boundedOutboxOperation(operation), boundedOutboxResult(result)).Add(float64(count))
}

func boundedOutboxOperation(operation string) string {
	switch operation {
	case outboxOperationClaim, outboxOperationRecover, outboxOperationExecute, outboxOperationFinalize, outboxOperationCleanup:
		return operation
	default:
		return "unknown"
	}
}

func boundedOutboxResult(result string) string {
	switch result {
	case "claimed", "empty", "failed", "retryable", "exhausted", "succeeded", "released", "stale", "deleted":
		return result
	default:
		return "unknown"
	}
}
