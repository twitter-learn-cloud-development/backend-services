package tool

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

type circuitObserver struct {
	states []CircuitState
}

func (o *circuitObserver) SetCircuitState(_ string, state CircuitState) {
	o.states = append(o.states, state)
}

func TestToolCircuitBreakerOpensAndUsesSingleHalfOpenProbe(t *testing.T) {
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	observer := &circuitObserver{}
	breaker := NewToolCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 2,
		OpenTimeout:      time.Minute,
		Now:              func() time.Time { return now },
		Observer:         observer,
	})

	breaker.RecordFailure("WebSearch")
	require.NoError(t, breaker.Allow("WebSearch"))
	breaker.RecordFailure("WebSearch")
	require.ErrorIs(t, breaker.Allow("WebSearch"), ErrCircuitOpen)

	now = now.Add(time.Minute)
	require.NoError(t, breaker.Allow("WebSearch"))
	require.ErrorIs(t, breaker.Allow("WebSearch"), ErrCircuitOpen)

	breaker.RecordSuccess("WebSearch")
	require.NoError(t, breaker.Allow("WebSearch"))
	require.Equal(t, []CircuitState{CircuitClosed, CircuitOpen, CircuitHalfOpen, CircuitClosed}, observer.states)
}

func TestExecutorRejectsNewExecutionWhenCircuitIsOpen(t *testing.T) {
	breaker := NewToolCircuitBreaker(CircuitBreakerConfig{FailureThreshold: 1, OpenTimeout: time.Hour})
	breaker.RecordFailure("ReadTool")
	called := false
	executor := NewExecutor(NewRegistry(), WithCircuitBreaker(breaker), WithAuditSink(&auditRecorder{}))

	_, err := executor.ExecuteAdHoc(context.Background(), ExecutionRequest{
		ToolName: "ReadTool", Inputs: map[string]interface{}{"query": "cloud"},
		Identity: CallerIdentity{UserID: 7}, Source: SourceRuntime,
	}, readSpec(), HandlerFunc(func(context.Context, map[string]interface{}) (map[string]interface{}, error) {
		called = true
		return nil, nil
	}))

	require.ErrorIs(t, err, ErrCircuitOpen)
	require.False(t, called)
	var executionErr *ExecutionError
	require.ErrorAs(t, err, &executionErr)
	require.Equal(t, CodeCircuitOpen, executionErr.Code)
}

func TestExecutorReplaysIdempotentResultWhileCircuitIsOpen(t *testing.T) {
	spec := newRecordingPublishTool().Spec()
	breaker := NewToolCircuitBreaker(CircuitBreakerConfig{FailureThreshold: 1, OpenTimeout: time.Hour})
	breaker.RecordFailure(spec.Name)
	store := &replayIdempotencyStore{claim: IdempotencyClaim{
		ExecutionID: "execution-1", Replayed: true, Outputs: map[string]interface{}{"tweet_id": float64(42)},
	}}
	executor := NewExecutor(NewRegistry(),
		WithApprovalGate(&lifecycleApprovalGate{}),
		WithIdempotencyStore(store),
		WithCircuitBreaker(breaker),
		WithAuditSink(&auditRecorder{}),
	)

	result, err := executor.ExecuteAdHoc(context.Background(), ExecutionRequest{
		ToolName: spec.Name, Inputs: map[string]interface{}{"content": "draft"},
		Identity: CallerIdentity{UserID: 7}, Source: SourceWorkflow,
		IdempotencyKey: "run:publish:PublishTweet",
	}, spec, HandlerFunc(func(context.Context, map[string]interface{}) (map[string]interface{}, error) {
		return nil, errors.New("must not execute")
	}))

	require.NoError(t, err)
	require.Equal(t, float64(42), result["tweet_id"])
}

func TestPrometheusMetricsUseOnlyBoundedGovernanceLabels(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, err := NewPrometheusMetrics(registry)
	require.NoError(t, err)
	metrics.RecordToolExecution(AuditEvent{
		ToolName: "WebSearch", Category: CategoryRead, Source: SourceRuntime,
		Decision: "succeeded", Attempts: 1, Duration: 10 * time.Millisecond,
		UserID: 99, RunID: "run-high-cardinality", StepID: "step-high-cardinality",
	})
	metrics.SetCircuitState("WebSearch", CircuitClosed)

	families, err := registry.Gather()
	require.NoError(t, err)
	require.NotEmpty(t, families)
	for _, family := range families {
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				require.NotContains(t, []string{"user_id", "run_id", "step_id", "error"}, label.GetName())
			}
		}
	}
}
