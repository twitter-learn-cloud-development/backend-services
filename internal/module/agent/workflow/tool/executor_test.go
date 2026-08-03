package tool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type auditRecorder struct {
	mu     sync.Mutex
	events []AuditEvent
}

func (r *auditRecorder) Record(ctx context.Context, event AuditEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *auditRecorder) last() AuditEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.events[len(r.events)-1]
}

type retryableTestError struct{}

func (retryableTestError) Error() string   { return "temporary" }
func (retryableTestError) Timeout() bool   { return false }
func (retryableTestError) Temporary() bool { return true }

type suspendedExecutionTestError struct{}

func (suspendedExecutionTestError) Error() string            { return "suspended" }
func (suspendedExecutionTestError) ExecutionSuspended() bool { return true }

type lifecycleApprovalGate struct {
	completed   int
	released    int
	completeErr error
	releaseErr  error
}

func (g *lifecycleApprovalGate) Authorize(context.Context, ApprovalCheck) (ApprovalGrant, error) {
	return ApprovalGrant{ApprovalID: "approval-1", AttemptID: "approval-attempt-1"}, nil
}

func (g *lifecycleApprovalGate) Complete(context.Context, ApprovalGrant) error {
	g.completed++
	return g.completeErr
}

func (g *lifecycleApprovalGate) Release(context.Context, ApprovalGrant, error) error {
	g.released++
	return g.releaseErr
}

type replayIdempotencyStore struct {
	claim       IdempotencyClaim
	claimErr    error
	completeErr error
	complete    int
	failed      int
}

func (s *replayIdempotencyStore) Claim(context.Context, IdempotencyCheck) (IdempotencyClaim, error) {
	return s.claim, s.claimErr
}

func (s *replayIdempotencyStore) Complete(context.Context, IdempotencyClaim, ResultSnapshot) error {
	s.complete++
	return s.completeErr
}

func (s *replayIdempotencyStore) Fail(context.Context, IdempotencyClaim, error) error {
	s.failed++
	return nil
}

type resultArchiverFake struct {
	reference *ResultReference
	err       error
	requests  []ResultArchiveRequest
	discarded []ResultReference
}

func (a *resultArchiverFake) Archive(_ context.Context, request ResultArchiveRequest) (*ResultReference, error) {
	a.requests = append(a.requests, request)
	return a.reference, a.err
}

func (a *resultArchiverFake) Discard(_ context.Context, reference ResultReference) error {
	a.discarded = append(a.discarded, reference)
	return nil
}

func readSpec() ToolSpec {
	return ToolSpec{
		Name: "ReadTool", Description: "read",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
		Category:    CategoryRead, Permission: PermissionAuthenticated,
		Approval: ApprovalNever, Timeout: time.Second,
		Retry: RetryPolicy{MaxAttempts: 1},
	}
}

func TestExecutorValidatesInputSchema(t *testing.T) {
	executor := NewExecutor(NewRegistry(), WithAuditSink(&auditRecorder{}))
	called := false
	_, err := executor.ExecuteAdHoc(context.Background(), ExecutionRequest{
		ToolName: "ReadTool", Inputs: map[string]interface{}{},
		Identity: CallerIdentity{UserID: 7}, Source: SourceRuntime,
	}, readSpec(), HandlerFunc(func(context.Context, map[string]interface{}) (map[string]interface{}, error) {
		called = true
		return nil, nil
	}))

	require.ErrorIs(t, err, ErrInvalidInput)
	require.False(t, called)
}

func TestExecutorRecordsExpectedSuspensionWithoutRetry(t *testing.T) {
	audit := &auditRecorder{}
	executor := NewExecutor(NewRegistry(), WithAuditSink(audit))
	attempts := 0
	_, err := executor.ExecuteAdHoc(context.Background(), ExecutionRequest{
		ToolName: "ReadTool", Inputs: map[string]interface{}{"query": "scope"},
		Identity: CallerIdentity{UserID: 7}, Source: SourceRuntime,
	}, readSpec(), HandlerFunc(func(context.Context, map[string]interface{}) (map[string]interface{}, error) {
		attempts++
		return nil, suspendedExecutionTestError{}
	}))

	var executionErr *ExecutionError
	require.ErrorAs(t, err, &executionErr)
	require.Equal(t, CodeSuspended, executionErr.Code)
	require.Equal(t, 1, attempts)
	require.Equal(t, "suspended", audit.last().Decision)
	require.Equal(t, CodeSuspended, audit.last().ErrorCode)
}

func TestExecutorFailsClosedForWriteWithoutApproval(t *testing.T) {
	spec := newRecordingPublishTool().Spec()
	executor := NewExecutor(NewRegistry(), WithAuditSink(&auditRecorder{}))
	called := false
	_, err := executor.ExecuteAdHoc(context.Background(), ExecutionRequest{
		ToolName: spec.Name, Inputs: map[string]interface{}{"content": "draft"},
		Identity: CallerIdentity{UserID: 7}, RunID: "run-1", StepID: "step-1", Source: SourceWorkflow,
		IdempotencyKey: "run-1:step-1:PublishTweet",
	}, spec, HandlerFunc(func(context.Context, map[string]interface{}) (map[string]interface{}, error) {
		called = true
		return nil, nil
	}))

	require.ErrorIs(t, err, ErrApprovalRequired)
	require.False(t, called)
}

func TestExecutorRequiresIdempotencyAfterApproval(t *testing.T) {
	spec := newRecordingPublishTool().Spec()
	executor := NewExecutor(NewRegistry(),
		WithAuditSink(&auditRecorder{}),
		WithApprovalGate(approvalGateFunc(func(context.Context, ApprovalCheck) (ApprovalGrant, error) {
			return ApprovalGrant{ApprovalID: "approval-1"}, nil
		})),
	)
	_, err := executor.ExecuteAdHoc(context.Background(), ExecutionRequest{
		ToolName: spec.Name, Inputs: map[string]interface{}{"content": "draft"},
		Identity: CallerIdentity{UserID: 7}, Source: SourceWorkflow,
	}, spec, HandlerFunc(func(context.Context, map[string]interface{}) (map[string]interface{}, error) {
		return nil, nil
	}))

	require.ErrorIs(t, err, ErrIdempotencyRequired)
}

func TestExecutorRejectsAnonymousApprovalGrant(t *testing.T) {
	spec := newRecordingPublishTool().Spec()
	executor := NewExecutor(NewRegistry(),
		WithAuditSink(&auditRecorder{}),
		WithApprovalGate(approvalGateFunc(func(context.Context, ApprovalCheck) (ApprovalGrant, error) {
			return ApprovalGrant{}, nil
		})),
	)
	_, err := executor.ExecuteAdHoc(context.Background(), ExecutionRequest{
		ToolName: spec.Name, Inputs: map[string]interface{}{"content": "draft"},
		Identity: CallerIdentity{UserID: 7}, Source: SourceWorkflow,
		IdempotencyKey: "run:step:PublishTweet",
	}, spec, HandlerFunc(func(context.Context, map[string]interface{}) (map[string]interface{}, error) {
		return nil, errors.New("must not execute")
	}))

	require.ErrorIs(t, err, ErrApprovalRequired)
}

func TestExecutorRetriesOnlyRetryableReadFailure(t *testing.T) {
	spec := readSpec()
	spec.Retry = RetryPolicy{MaxAttempts: 2, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond}
	attempts := 0
	executor := NewExecutor(NewRegistry(), WithAuditSink(&auditRecorder{}))
	result, err := executor.ExecuteAdHoc(context.Background(), ExecutionRequest{
		ToolName: spec.Name, Inputs: map[string]interface{}{"query": "cloud"},
		Identity: CallerIdentity{UserID: 7}, Source: SourceRuntime,
	}, spec, HandlerFunc(func(context.Context, map[string]interface{}) (map[string]interface{}, error) {
		attempts++
		if attempts == 1 {
			return nil, retryableTestError{}
		}
		return map[string]interface{}{"ok": true}, nil
	}))

	require.NoError(t, err)
	require.Equal(t, 2, attempts)
	require.Equal(t, true, result["ok"])
}

func TestExecutorPropagatesTimeout(t *testing.T) {
	spec := readSpec()
	spec.Timeout = 10 * time.Millisecond
	executor := NewExecutor(NewRegistry(), WithAuditSink(&auditRecorder{}))
	_, err := executor.ExecuteAdHoc(context.Background(), ExecutionRequest{
		ToolName: spec.Name, Inputs: map[string]interface{}{"query": "cloud"},
		Identity: CallerIdentity{UserID: 7}, Source: SourceRuntime,
	}, spec, HandlerFunc(func(ctx context.Context, inputs map[string]interface{}) (map[string]interface{}, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}))

	var executionErr *ExecutionError
	require.ErrorAs(t, err, &executionErr)
	require.Equal(t, CodeTimeout, executionErr.Code)
}

func TestExecutorRedactsSensitiveAuditInputs(t *testing.T) {
	spec := readSpec()
	spec.SensitiveFields = []string{"content"}
	spec.InputSchema = json.RawMessage(`{"type":"object"}`)
	audit := &auditRecorder{}
	executor := NewExecutor(NewRegistry(), WithAuditSink(audit))
	var handlerInputs map[string]interface{}
	_, err := executor.ExecuteAdHoc(context.Background(), ExecutionRequest{
		ToolName: spec.Name,
		Inputs: map[string]interface{}{
			"query": "cloud", "api_key": "secret-key", "content": "private draft",
			"nested": map[string]interface{}{"token": "secret-token"},
		},
		Identity: CallerIdentity{UserID: 7}, Source: SourceRuntime,
	}, spec, HandlerFunc(func(_ context.Context, inputs map[string]interface{}) (map[string]interface{}, error) {
		handlerInputs = inputs
		return map[string]interface{}{"ok": true}, nil
	}))

	require.NoError(t, err)
	require.Equal(t, "secret-key", handlerInputs["api_key"])
	require.Equal(t, "private draft", handlerInputs["content"])
	event := audit.last()
	require.NotEmpty(t, event.InputDigest)
	require.Positive(t, event.InputLength)
	require.NotEmpty(t, event.OutputDigest)
	require.Positive(t, event.OutputLength)
	require.Equal(t, "[REDACTED]", event.Inputs["api_key"])
	require.Equal(t, "[REDACTED]", event.Inputs["content"])
	nested := event.Inputs["nested"].(map[string]interface{})
	require.Equal(t, "[REDACTED]", nested["token"])
}

func TestExecutorRejectsInternalToolFromUserRuntime(t *testing.T) {
	spec := readSpec()
	spec.Name = "InternalTool"
	spec.Category = CategoryInternal
	spec.Permission = PermissionInternal
	executor := NewExecutor(NewRegistry(), WithAuditSink(&auditRecorder{}))
	_, err := executor.ExecuteAdHoc(context.Background(), ExecutionRequest{
		ToolName: spec.Name, Inputs: map[string]interface{}{"query": "cloud"},
		Identity: CallerIdentity{UserID: 7}, Source: SourceRuntime,
	}, spec, HandlerFunc(func(context.Context, map[string]interface{}) (map[string]interface{}, error) {
		return nil, errors.New("must not execute")
	}))

	require.ErrorIs(t, err, ErrForbidden)
}

func TestExecutorReplaysPersistentIdempotentResultWithoutCallingHandler(t *testing.T) {
	spec := newRecordingPublishTool().Spec()
	gate := &lifecycleApprovalGate{}
	store := &replayIdempotencyStore{claim: IdempotencyClaim{
		ExecutionID: "execution-1", Replayed: true,
		Outputs: map[string]interface{}{"tweet_id": float64(99), "status": "success"},
	}}
	audit := &auditRecorder{}
	executor := NewExecutor(NewRegistry(), WithApprovalGate(gate), WithIdempotencyStore(store), WithAuditSink(audit))
	called := false
	result, err := executor.ExecuteAdHoc(context.Background(), ExecutionRequest{
		ToolName: spec.Name, Inputs: map[string]interface{}{"content": "draft"},
		Identity: CallerIdentity{UserID: 7}, RunID: "run", StepID: "publish", Source: SourceWorkflow,
		IdempotencyKey: "run:publish:PublishTweet",
	}, spec, HandlerFunc(func(context.Context, map[string]interface{}) (map[string]interface{}, error) {
		called = true
		return nil, nil
	}))

	require.NoError(t, err)
	require.False(t, called)
	require.Equal(t, float64(99), result["tweet_id"])
	require.Equal(t, 1, gate.completed)
	require.Zero(t, gate.released)
	require.Equal(t, "replayed", audit.last().Decision)
}

func TestExecutorDoesNotReportReplayedSideEffectAsFailedWhenApprovalCompletionFails(t *testing.T) {
	spec := newRecordingPublishTool().Spec()
	gate := &lifecycleApprovalGate{completeErr: errors.New("approval store unavailable")}
	store := &replayIdempotencyStore{claim: IdempotencyClaim{
		ExecutionID: "execution-1", Replayed: true,
		Outputs: map[string]interface{}{"tweet_id": float64(99), "status": "success"},
	}}
	audit := &auditRecorder{}
	executor := NewExecutor(NewRegistry(), WithApprovalGate(gate), WithIdempotencyStore(store), WithAuditSink(audit))

	result, err := executor.ExecuteAdHoc(context.Background(), ExecutionRequest{
		ToolName: spec.Name, Inputs: map[string]interface{}{"content": "draft"},
		Identity: CallerIdentity{UserID: 7}, RunID: "run", StepID: "publish", Source: SourceWorkflow,
		IdempotencyKey: "run:publish:PublishTweet",
	}, spec, HandlerFunc(func(context.Context, map[string]interface{}) (map[string]interface{}, error) {
		return nil, errors.New("must not execute")
	}))

	require.NoError(t, err)
	require.Equal(t, float64(99), result["tweet_id"])
	require.Equal(t, 1, gate.completed)
	require.Equal(t, "replayed", audit.last().Decision)
}

func TestExecutorRejectsIdempotencyInputConflictAndReleasesApproval(t *testing.T) {
	spec := newRecordingPublishTool().Spec()
	gate := &lifecycleApprovalGate{}
	store := &replayIdempotencyStore{claimErr: ErrIdempotencyConflict}
	executor := NewExecutor(NewRegistry(), WithApprovalGate(gate), WithIdempotencyStore(store), WithAuditSink(&auditRecorder{}))
	_, err := executor.ExecuteAdHoc(context.Background(), ExecutionRequest{
		ToolName: spec.Name, Inputs: map[string]interface{}{"content": "different draft"},
		Identity: CallerIdentity{UserID: 7}, RunID: "run", StepID: "publish", Source: SourceWorkflow,
		IdempotencyKey: "run:publish:PublishTweet",
	}, spec, HandlerFunc(func(context.Context, map[string]interface{}) (map[string]interface{}, error) {
		return nil, errors.New("must not execute")
	}))

	var executionErr *ExecutionError
	require.ErrorAs(t, err, &executionErr)
	require.Equal(t, CodeIdempotencyConflict, executionErr.Code)
	require.Equal(t, 1, gate.released)
}

func TestExecutorRejectsOversizedToolResultBeforeReturningIt(t *testing.T) {
	audit := &auditRecorder{}
	executor := NewExecutor(NewRegistry(), WithAuditSink(audit), WithResultPolicy(ResultPolicy{MaxBytes: 64}))

	_, err := executor.ExecuteAdHoc(context.Background(), ExecutionRequest{
		ToolName: "ReadTool", Inputs: map[string]interface{}{"query": "cloud"},
		Identity: CallerIdentity{UserID: 7}, RunID: "run", StepID: "search", Source: SourceRuntime,
	}, readSpec(), HandlerFunc(func(context.Context, map[string]interface{}) (map[string]interface{}, error) {
		return map[string]interface{}{"content": strings.Repeat("x", 128)}, nil
	}))

	var executionErr *ExecutionError
	require.ErrorAs(t, err, &executionErr)
	require.Equal(t, CodeResultTooLarge, executionErr.Code)
	require.ErrorIs(t, err, ErrResultTooLarge)
	require.Greater(t, audit.last().OutputLength, 64)
	require.Equal(t, "failed", audit.last().Decision)
}

func TestExecutorArchivesLargeToolResultAndRecordsReference(t *testing.T) {
	reference := &ResultReference{
		Storage: "minio", Bucket: "agent-tool-results", Key: "tool-results/a/b/c.json",
		Digest: strings.Repeat("a", 64), Length: 128, ContentType: "application/json",
	}
	archiver := &resultArchiverFake{reference: reference}
	audit := &auditRecorder{}
	executor := NewExecutor(NewRegistry(), WithAuditSink(audit), WithResultArchiver(archiver))

	result, err := executor.ExecuteAdHoc(context.Background(), ExecutionRequest{
		ToolName: "ReadTool", Inputs: map[string]interface{}{"query": "cloud"},
		Identity: CallerIdentity{UserID: 7}, RunID: "run", StepID: "search", Source: SourceRuntime,
	}, readSpec(), HandlerFunc(func(context.Context, map[string]interface{}) (map[string]interface{}, error) {
		return map[string]interface{}{"content": strings.Repeat("x", 128)}, nil
	}))

	require.NoError(t, err)
	require.NotEmpty(t, result["content"])
	require.Len(t, archiver.requests, 1)
	require.Equal(t, uint64(7), archiver.requests[0].UserID)
	require.Equal(t, reference.URI(), audit.last().OutputReference.URI())
}

func TestExecutorDiscardsArchivedResultWhenIdempotencyCommitFails(t *testing.T) {
	spec := newRecordingPublishTool().Spec()
	reference := &ResultReference{
		Storage: "minio", Bucket: "agent-tool-results", Key: "tool-results/a/b/c.json",
		Digest: strings.Repeat("a", 64), Length: 128, ContentType: "application/json",
	}
	archiver := &resultArchiverFake{reference: reference}
	store := &replayIdempotencyStore{
		claim:       IdempotencyClaim{ExecutionID: "execution-1", UserID: 7},
		completeErr: errors.New("mongo unavailable"),
	}
	executor := NewExecutor(NewRegistry(),
		WithApprovalGate(&lifecycleApprovalGate{}), WithIdempotencyStore(store),
		WithResultArchiver(archiver), WithAuditSink(&auditRecorder{}),
	)

	_, err := executor.ExecuteAdHoc(context.Background(), ExecutionRequest{
		ToolName: spec.Name, Inputs: map[string]interface{}{"content": "draft"},
		Identity: CallerIdentity{UserID: 7}, RunID: "run", StepID: "publish", Source: SourceWorkflow,
		IdempotencyKey: "run:publish:PublishTweet",
	}, spec, HandlerFunc(func(context.Context, map[string]interface{}) (map[string]interface{}, error) {
		return map[string]interface{}{"tweet_id": 42, "content": strings.Repeat("x", 128)}, nil
	}))

	require.ErrorContains(t, err, "persist tool result")
	require.Len(t, archiver.discarded, 1)
	require.Equal(t, reference.URI(), archiver.discarded[0].URI())
}
