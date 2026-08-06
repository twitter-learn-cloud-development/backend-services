package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"twitter-clone/internal/module/agent/repository"
)

type reconcileRepositoryFake struct {
	result repository.ToolGovernanceReconcileResult
	err    error
}

func (r *reconcileRepositoryFake) ReconcileToolGovernance(context.Context, time.Time) (repository.ToolGovernanceReconcileResult, error) {
	return r.result, r.err
}

type reconcileMetricsRecorder struct {
	mu      sync.Mutex
	result  string
	actions map[string]int64
}

func (m *reconcileMetricsRecorder) RecordReconciliation(result string, actions map[string]int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.result = result
	m.actions = actions
}

func TestToolGovernanceReconcilerRecordsRepairedActions(t *testing.T) {
	repo := &reconcileRepositoryFake{result: repository.ToolGovernanceReconcileResult{
		ExpiredApprovals: 2, ReleasedApprovalLeases: 1, FailedExecutionLeases: 3, FailedSuspendedRuns: 2, FailedAgentRuns: 1,
	}}
	metrics := &reconcileMetricsRecorder{}
	reconciler := NewToolGovernanceReconciler(repo, time.Minute, metrics)

	reconciler.reconcile(context.Background())

	require.Equal(t, "succeeded", metrics.result)
	require.EqualValues(t, 2, metrics.actions["expired_approval"])
	require.EqualValues(t, 1, metrics.actions["released_approval_lease"])
	require.EqualValues(t, 3, metrics.actions["failed_execution_lease"])
	require.EqualValues(t, 2, metrics.actions["failed_suspended_run"])
	require.EqualValues(t, 1, metrics.actions["failed_agent_run"])
}

func TestToolGovernanceReconcilerRecordsFailure(t *testing.T) {
	metrics := &reconcileMetricsRecorder{}
	reconciler := NewToolGovernanceReconciler(&reconcileRepositoryFake{err: errors.New("mongo unavailable")}, time.Minute, metrics)

	reconciler.reconcile(context.Background())

	require.Equal(t, "failed", metrics.result)
	require.Nil(t, metrics.actions)
}
