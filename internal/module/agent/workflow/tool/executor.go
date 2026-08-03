package tool

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"twitter-clone/internal/module/agent/workflow/guardrails"
)

type ErrorCode string

const (
	CodeUnknownTool         ErrorCode = "unknown_tool"
	CodeInvalidInput        ErrorCode = "invalid_input"
	CodeUnauthenticated     ErrorCode = "unauthenticated"
	CodeForbidden           ErrorCode = "forbidden"
	CodeApprovalRequired    ErrorCode = "approval_required"
	CodeIdempotencyRequired ErrorCode = "idempotency_required"
	CodeIdempotencyConflict ErrorCode = "idempotency_conflict"
	CodeAlreadyExecuting    ErrorCode = "already_executing"
	CodeCircuitOpen         ErrorCode = "circuit_open"
	CodeResultTooLarge      ErrorCode = "result_too_large"
	CodeTimeout             ErrorCode = "timeout"
	CodeCanceled            ErrorCode = "canceled"
	CodeSuspended           ErrorCode = "suspended"
	CodeExecutionFailed     ErrorCode = "execution_failed"
)

var (
	ErrUnknownTool         = errors.New("unknown tool")
	ErrInvalidInput        = errors.New("invalid tool input")
	ErrUnauthenticated     = errors.New("tool caller is unauthenticated")
	ErrForbidden           = errors.New("tool execution is forbidden")
	ErrApprovalRequired    = errors.New("tool approval is required")
	ErrIdempotencyRequired = errors.New("tool idempotency key is required")
	ErrIdempotencyConflict = errors.New("tool idempotency key conflicts with another input")
	ErrAlreadyExecuting    = errors.New("tool execution is already in progress")
	ErrCircuitOpen         = errors.New("tool circuit breaker is open")
	ErrResultTooLarge      = errors.New("tool result exceeds the configured size limit")
)

const DefaultMaxResultBytes = 1 << 20

type ExecutionError struct {
	Code      ErrorCode
	ToolName  string
	Attempt   int
	Retryable bool
	Cause     error
}

func (e *ExecutionError) Error() string {
	if e == nil {
		return "tool execution error"
	}
	if e.Cause != nil {
		return fmt.Sprintf("tool %s %s: %v", e.ToolName, e.Code, e.Cause)
	}
	return fmt.Sprintf("tool %s %s", e.ToolName, e.Code)
}

func (e *ExecutionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *ExecutionError) IsRetryable() bool {
	return e != nil && e.Retryable
}

type ExecutionSource string

const (
	SourceWorkflow ExecutionSource = "workflow"
	SourceRuntime  ExecutionSource = "runtime"
	SourceLegacy   ExecutionSource = "legacy_agent"
	SourceInternal ExecutionSource = "internal"
)

type CallerIdentity struct {
	UserID   uint64
	Internal bool
}

type ExecutionRequest struct {
	ToolName       string
	Inputs         map[string]interface{}
	Identity       CallerIdentity
	RunID          string
	StepID         string
	Source         ExecutionSource
	IdempotencyKey string
}

type ApprovalCheck struct {
	Tool           ToolSpec
	Identity       CallerIdentity
	RunID          string
	StepID         string
	Source         ExecutionSource
	Inputs         map[string]interface{}
	IdempotencyKey string
}

type ApprovalGrant struct {
	ApprovalID     string
	AttemptID      string
	IdempotencyKey string
}

type ApprovalPendingError struct {
	ApprovalID string
}

func (e *ApprovalPendingError) Error() string {
	if e == nil || e.ApprovalID == "" {
		return "tool approval is pending"
	}
	return fmt.Sprintf("tool approval %s is pending", e.ApprovalID)
}

type ApprovalGate interface {
	Authorize(ctx context.Context, check ApprovalCheck) (ApprovalGrant, error)
}

type ApprovalLifecycleGate interface {
	ApprovalGate
	Complete(ctx context.Context, grant ApprovalGrant) error
	Release(ctx context.Context, grant ApprovalGrant, cause error) error
}

type IdempotencyCheck struct {
	ToolName       string
	UserID         uint64
	IdempotencyKey string
	InputDigest    string
	LeaseUntil     time.Time
}

type IdempotencyClaim struct {
	ExecutionID     string
	AttemptID       string
	UserID          uint64
	Replayed        bool
	Outputs         map[string]interface{}
	OutputReference *ResultReference
}

type IdempotencyStore interface {
	Claim(ctx context.Context, check IdempotencyCheck) (IdempotencyClaim, error)
	Complete(ctx context.Context, claim IdempotencyClaim, result ResultSnapshot) error
	Fail(ctx context.Context, claim IdempotencyClaim, cause error) error
}

type ResultReference struct {
	Storage     string `json:"storage"`
	Bucket      string `json:"bucket"`
	Key         string `json:"key"`
	Digest      string `json:"digest"`
	Length      int    `json:"length"`
	ContentType string `json:"content_type"`
}

func (r ResultReference) URI() string {
	if strings.TrimSpace(r.Storage) == "" || strings.TrimSpace(r.Bucket) == "" || strings.TrimSpace(r.Key) == "" {
		return ""
	}
	return r.Storage + "://" + r.Bucket + "/" + strings.TrimLeft(r.Key, "/")
}

type ResultSnapshot struct {
	Outputs   map[string]interface{}
	Payload   []byte
	Digest    string
	Length    int
	Reference *ResultReference
}

type ResultArchiveRequest struct {
	ToolName string
	UserID   uint64
	RunID    string
	StepID   string
	Source   ExecutionSource
	ResultID string
	Snapshot ResultSnapshot
}

type ResultArchiver interface {
	Archive(ctx context.Context, request ResultArchiveRequest) (*ResultReference, error)
	Discard(ctx context.Context, reference ResultReference) error
}

type ResultPolicy struct {
	MaxBytes int
}

func (p ResultPolicy) normalized() ResultPolicy {
	if p.MaxBytes <= 0 {
		p.MaxBytes = DefaultMaxResultBytes
	}
	return p
}

type denyApprovalGate struct{}

func (denyApprovalGate) Authorize(context.Context, ApprovalCheck) (ApprovalGrant, error) {
	return ApprovalGrant{}, ErrApprovalRequired
}

type AuditEvent struct {
	ToolName        string
	Category        Category
	UserID          uint64
	RunID           string
	StepID          string
	Source          ExecutionSource
	Decision        string
	ErrorCode       ErrorCode
	Attempts        int
	Duration        time.Duration
	IdempotencyKey  string
	Inputs          map[string]interface{}
	InputDigest     string
	InputLength     int
	OutputDigest    string
	OutputLength    int
	OutputReference *ResultReference
}

type AuditSink interface {
	Record(ctx context.Context, event AuditEvent)
}

// MetricsSink observes the same bounded decision model as the audit sink.
// Implementations must not promote user, run, input, or error text to labels.
type MetricsSink interface {
	RecordToolExecution(event AuditEvent)
}

type noopMetricsSink struct{}

func (noopMetricsSink) RecordToolExecution(AuditEvent) {}

type SlogAuditSink struct{}

func (SlogAuditSink) Record(ctx context.Context, event AuditEvent) {
	slog.InfoContext(ctx, "agent tool audit",
		"tool", event.ToolName,
		"category", event.Category,
		"user_id", event.UserID,
		"run_id", event.RunID,
		"step_id", event.StepID,
		"source", event.Source,
		"decision", event.Decision,
		"error_code", event.ErrorCode,
		"attempts", event.Attempts,
		"duration_ms", event.Duration.Milliseconds(),
		"inputs", event.Inputs,
		"output_digest", event.OutputDigest,
		"output_length", event.OutputLength,
		"output_reference", resultReferenceURI(event.OutputReference),
	)
}

type InputGuardrail interface {
	ValidateAndInjectToolInputs(ctx context.Context, toolName string, inputs map[string]interface{}) (map[string]interface{}, error)
}

type ExecutorOption func(*Executor)

func WithApprovalGate(gate ApprovalGate) ExecutorOption {
	return func(executor *Executor) {
		if gate != nil {
			executor.approvals = gate
		}
	}
}

func WithAuditSink(sink AuditSink) ExecutorOption {
	return func(executor *Executor) {
		if sink != nil {
			executor.audit = sink
		}
	}
}

func WithInputGuardrail(guardrail InputGuardrail) ExecutorOption {
	return func(executor *Executor) {
		if guardrail != nil {
			executor.guardrail = guardrail
		}
	}
}

func WithIdempotencyStore(store IdempotencyStore) ExecutorOption {
	return func(executor *Executor) {
		if store != nil {
			executor.idempotency = store
		}
	}
}

func WithResultArchiver(archiver ResultArchiver) ExecutorOption {
	return func(executor *Executor) {
		if archiver != nil {
			executor.results = archiver
		}
	}
}

func WithResultPolicy(policy ResultPolicy) ExecutorOption {
	return func(executor *Executor) {
		executor.resultPolicy = policy.normalized()
	}
}

func WithMetricsSink(sink MetricsSink) ExecutorOption {
	return func(executor *Executor) {
		if sink != nil {
			executor.metrics = sink
		}
	}
}

func WithCircuitBreaker(breaker CircuitBreaker) ExecutorOption {
	return func(executor *Executor) {
		if breaker != nil {
			executor.breaker = breaker
		}
	}
}

type Executor struct {
	registry     *ToolRegistry
	approvals    ApprovalGate
	audit        AuditSink
	guardrail    InputGuardrail
	idempotency  IdempotencyStore
	results      ResultArchiver
	resultPolicy ResultPolicy
	metrics      MetricsSink
	breaker      CircuitBreaker
	validators   sync.Map
	now          func() time.Time
}

func NewExecutor(registry *ToolRegistry, options ...ExecutorOption) *Executor {
	executor := &Executor{
		registry:     registry,
		approvals:    denyApprovalGate{},
		audit:        SlogAuditSink{},
		guardrail:    guardrails.NewSecurityGuardrail(),
		metrics:      noopMetricsSink{},
		breaker:      noopCircuitBreaker{},
		resultPolicy: ResultPolicy{MaxBytes: DefaultMaxResultBytes},
		now:          time.Now,
	}
	for _, option := range options {
		if option != nil {
			option(executor)
		}
	}
	return executor
}

func (e *Executor) Registry() *ToolRegistry {
	if e == nil {
		return nil
	}
	return e.registry
}

func (e *Executor) ExecuteRegistered(ctx context.Context, request ExecutionRequest) (map[string]interface{}, error) {
	if e == nil || e.registry == nil {
		return nil, &ExecutionError{Code: CodeUnknownTool, ToolName: request.ToolName, Cause: ErrUnknownTool}
	}
	registered, ok := e.registry.Get(request.ToolName)
	if !ok {
		return nil, &ExecutionError{Code: CodeUnknownTool, ToolName: request.ToolName, Cause: ErrUnknownTool}
	}
	return e.execute(ctx, request, registered.Spec, registered.Handler, registered.validate)
}

// ExecuteAdHoc applies the same governance pipeline to provider-discovered
// tools such as MCP tools without creating a second mutable registry.
func (e *Executor) ExecuteAdHoc(ctx context.Context, request ExecutionRequest, spec ToolSpec, handler ToolHandler) (map[string]interface{}, error) {
	if e == nil {
		return nil, &ExecutionError{Code: CodeExecutionFailed, ToolName: request.ToolName, Cause: errors.New("tool executor is not configured")}
	}
	normalized, err := spec.Normalize()
	if err != nil {
		return nil, &ExecutionError{Code: CodeInvalidInput, ToolName: request.ToolName, Cause: err}
	}
	validator, err := e.validator(normalized)
	if err != nil {
		return nil, &ExecutionError{Code: CodeInvalidInput, ToolName: normalized.Name, Cause: err}
	}
	return e.execute(ctx, request, normalized, handler, validator)
}

func (e *Executor) validator(spec ToolSpec) (inputValidator, error) {
	key := spec.Name + "\x00" + string(spec.InputSchema)
	if cached, ok := e.validators.Load(key); ok {
		return cached.(inputValidator), nil
	}
	validator, err := compileInputSchema(spec)
	if err != nil {
		return nil, err
	}
	actual, _ := e.validators.LoadOrStore(key, validator)
	return actual.(inputValidator), nil
}

func (e *Executor) execute(ctx context.Context, request ExecutionRequest, spec ToolSpec, handler ToolHandler, validate inputValidator) (map[string]interface{}, error) {
	startedAt := e.now()
	inputs := cloneInputs(request.Inputs)
	inputDigest, inputLength := digestToolValue(inputs)
	event := AuditEvent{
		ToolName: spec.Name, Category: spec.Category, UserID: request.Identity.UserID,
		RunID: request.RunID, StepID: request.StepID, Source: request.Source,
		IdempotencyKey: request.IdempotencyKey, Inputs: RedactInputs(inputs, spec.SensitiveFields),
		InputDigest: inputDigest, InputLength: inputLength,
	}
	record := func(decision string, code ErrorCode, attempts int) {
		event.Decision = decision
		event.ErrorCode = code
		event.Attempts = attempts
		event.Duration = e.now().Sub(startedAt)
		e.audit.Record(ctx, event)
		e.metrics.RecordToolExecution(event)
	}

	if handler == nil {
		err := &ExecutionError{Code: CodeExecutionFailed, ToolName: spec.Name, Cause: errors.New("tool handler is not configured")}
		record("failed", err.Code, 0)
		return nil, err
	}
	if err := e.authorizeIdentity(ctx, request.Identity, spec); err != nil {
		record("denied", err.Code, 0)
		return nil, err
	}
	if validate != nil {
		if err := validate(inputs); err != nil {
			execErr := &ExecutionError{Code: CodeInvalidInput, ToolName: spec.Name, Cause: fmt.Errorf("%w: %v", ErrInvalidInput, err)}
			record("denied", execErr.Code, 0)
			return nil, execErr
		}
	}

	executionCtx := ctx
	if request.Identity.UserID > 0 {
		if existing, ok := guardrails.AuthenticatedUserID(ctx); ok && existing != request.Identity.UserID {
			err := &ExecutionError{Code: CodeForbidden, ToolName: spec.Name, Cause: fmt.Errorf("%w: authenticated identity mismatch", ErrForbidden)}
			record("denied", err.Code, 0)
			return nil, err
		}
		executionCtx = guardrails.InjectUserContext(ctx, request.Identity.UserID)
	}
	if e.guardrail != nil {
		guarded, err := e.guardrail.ValidateAndInjectToolInputs(executionCtx, spec.Name, inputs)
		if err != nil {
			execErr := &ExecutionError{Code: CodeForbidden, ToolName: spec.Name, Cause: fmt.Errorf("%w: %v", ErrForbidden, err)}
			record("denied", execErr.Code, 0)
			return nil, execErr
		}
		inputs = guarded
		event.Inputs = RedactInputs(inputs, spec.SensitiveFields)
		event.InputDigest, event.InputLength = digestToolValue(inputs)
	}

	idempotencyKey := strings.TrimSpace(request.IdempotencyKey)
	var approvalGrant ApprovalGrant
	if spec.RequiresApproval() {
		grant, err := e.approvals.Authorize(executionCtx, ApprovalCheck{
			Tool: spec, Identity: request.Identity, RunID: request.RunID, StepID: request.StepID,
			Source: request.Source, Inputs: cloneInputs(inputs), IdempotencyKey: idempotencyKey,
		})
		if err != nil {
			execErr := &ExecutionError{Code: CodeApprovalRequired, ToolName: spec.Name, Cause: errors.Join(ErrApprovalRequired, err)}
			record("approval_required", execErr.Code, 0)
			return nil, execErr
		}
		if strings.TrimSpace(grant.ApprovalID) == "" {
			execErr := &ExecutionError{Code: CodeApprovalRequired, ToolName: spec.Name, Cause: fmt.Errorf("%w: approval grant has no identity", ErrApprovalRequired)}
			record("approval_required", execErr.Code, 0)
			return nil, execErr
		}
		if idempotencyKey == "" {
			idempotencyKey = strings.TrimSpace(grant.IdempotencyKey)
		}
		approvalGrant = grant
	}
	if spec.Idempotency.Required && idempotencyKey == "" {
		err := &ExecutionError{Code: CodeIdempotencyRequired, ToolName: spec.Name, Cause: ErrIdempotencyRequired}
		record("denied", err.Code, 0)
		return nil, err
	}
	executionMetadata := ExecutionMetadataFromContext(executionCtx)
	executionMetadata.RunID = request.RunID
	executionMetadata.StepID = request.StepID
	executionMetadata.Source = request.Source
	executionMetadata.IdempotencyKey = idempotencyKey
	executionCtx = InjectExecutionMetadata(executionCtx, executionMetadata)

	policy := spec.Retry.normalized()
	if spec.Category == CategoryWrite && !spec.Idempotency.Required {
		policy.MaxAttempts = 1
	}

	var idempotencyClaim IdempotencyClaim
	if spec.Idempotency.Required && e.idempotency != nil {
		claim, err := e.idempotency.Claim(executionCtx, IdempotencyCheck{
			ToolName: spec.Name, UserID: request.Identity.UserID, IdempotencyKey: idempotencyKey,
			InputDigest: DigestInputs(inputs), LeaseUntil: e.now().Add(spec.Timeout + policy.MaxBackoff),
		})
		if err != nil {
			e.releaseApproval(executionCtx, approvalGrant, err)
			code := CodeExecutionFailed
			if errors.Is(err, ErrIdempotencyConflict) {
				code = CodeIdempotencyConflict
			} else if errors.Is(err, ErrAlreadyExecuting) {
				code = CodeAlreadyExecuting
			}
			execErr := &ExecutionError{Code: code, ToolName: spec.Name, Cause: err}
			record("denied", code, 0)
			return nil, execErr
		}
		idempotencyClaim = claim
		if claim.Replayed {
			if err := e.completeApproval(executionCtx, approvalGrant); err != nil {
				e.logGovernancePersistenceError(executionCtx, spec.Name, approvalGrant, idempotencyClaim, "complete replayed tool approval", err)
			}
			event.IdempotencyKey = idempotencyKey
			event.OutputDigest, event.OutputLength = digestToolValue(claim.Outputs)
			event.OutputReference = claim.OutputReference
			record("replayed", "", 0)
			return cloneInputs(claim.Outputs), nil
		}
	}
	if err := e.breaker.Allow(spec.Name); err != nil {
		if e.idempotency != nil && idempotencyClaim.ExecutionID != "" {
			e.failIdempotency(executionCtx, spec.Name, approvalGrant, idempotencyClaim, err)
		}
		e.releaseApproval(executionCtx, approvalGrant, err)
		execErr := &ExecutionError{Code: CodeCircuitOpen, ToolName: spec.Name, Cause: errors.Join(ErrCircuitOpen, err)}
		record("circuit_open", execErr.Code, 0)
		return nil, execErr
	}
	var lastErr error
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(executionCtx, spec.Timeout)
		outputs, err := handler.Execute(attemptCtx, cloneInputs(inputs))
		cancel()
		if err == nil {
			snapshot, snapshotErr := buildResultSnapshot(outputs, e.resultPolicy.MaxBytes)
			if snapshotErr != nil {
				if e.idempotency != nil && idempotencyClaim.ExecutionID != "" {
					e.failIdempotency(executionCtx, spec.Name, approvalGrant, idempotencyClaim, snapshotErr)
				}
				e.releaseApproval(executionCtx, approvalGrant, snapshotErr)
				code := CodeExecutionFailed
				if errors.Is(snapshotErr, ErrResultTooLarge) {
					code = CodeResultTooLarge
				}
				event.OutputDigest, event.OutputLength = snapshot.Digest, snapshot.Length
				execErr := &ExecutionError{Code: code, ToolName: spec.Name, Attempt: attempt, Cause: snapshotErr}
				record("failed", code, attempt)
				return nil, execErr
			}
			if e.results != nil {
				resultID := idempotencyClaim.ExecutionID
				if resultID == "" {
					resultID = NewAttemptID()
				}
				reference, archiveErr := e.results.Archive(executionCtx, ResultArchiveRequest{
					ToolName: spec.Name, UserID: request.Identity.UserID, RunID: request.RunID,
					StepID: request.StepID, Source: request.Source, ResultID: resultID, Snapshot: snapshot,
				})
				if archiveErr != nil {
					if e.idempotency != nil && idempotencyClaim.ExecutionID != "" {
						e.failIdempotency(executionCtx, spec.Name, approvalGrant, idempotencyClaim, archiveErr)
					}
					e.releaseApproval(executionCtx, approvalGrant, archiveErr)
					execErr := &ExecutionError{Code: CodeExecutionFailed, ToolName: spec.Name, Attempt: attempt, Cause: fmt.Errorf("archive tool result: %w", archiveErr)}
					event.OutputDigest, event.OutputLength = snapshot.Digest, snapshot.Length
					record("failed", execErr.Code, attempt)
					return nil, execErr
				}
				snapshot.Reference = reference
			}
			e.breaker.RecordSuccess(spec.Name)
			if e.idempotency != nil && idempotencyClaim.ExecutionID != "" {
				if completeErr := e.idempotency.Complete(executionCtx, idempotencyClaim, snapshot); completeErr != nil {
					if e.results != nil && snapshot.Reference != nil {
						if discardErr := e.results.Discard(executionCtx, *snapshot.Reference); discardErr != nil {
							e.logGovernancePersistenceError(executionCtx, spec.Name, approvalGrant, idempotencyClaim, "discard uncommitted tool result", discardErr)
						}
					}
					e.releaseApproval(executionCtx, approvalGrant, completeErr)
					execErr := &ExecutionError{Code: CodeExecutionFailed, ToolName: spec.Name, Attempt: attempt, Cause: fmt.Errorf("persist tool result: %w", completeErr)}
					record("failed", execErr.Code, attempt)
					return nil, execErr
				}
			}
			if completeErr := e.completeApproval(executionCtx, approvalGrant); completeErr != nil {
				e.logGovernancePersistenceError(executionCtx, spec.Name, approvalGrant, idempotencyClaim, "complete tool approval", completeErr)
			}
			event.IdempotencyKey = idempotencyKey
			event.OutputDigest, event.OutputLength = snapshot.Digest, snapshot.Length
			event.OutputReference = snapshot.Reference
			record("succeeded", "", attempt)
			return outputs, nil
		}
		lastErr = err
		if executionSuspended(err) {
			if e.idempotency != nil && idempotencyClaim.ExecutionID != "" {
				e.failIdempotency(executionCtx, spec.Name, approvalGrant, idempotencyClaim, err)
			}
			e.releaseApproval(executionCtx, approvalGrant, err)
			execErr := &ExecutionError{
				Code: CodeSuspended, ToolName: spec.Name, Attempt: attempt, Cause: err,
			}
			record("suspended", execErr.Code, attempt)
			return nil, execErr
		}
		if !retryable(err) || attempt == policy.MaxAttempts {
			if breakerFailure(executionCtx, err) {
				e.breaker.RecordFailure(spec.Name)
			}
			if e.idempotency != nil && idempotencyClaim.ExecutionID != "" {
				e.failIdempotency(executionCtx, spec.Name, approvalGrant, idempotencyClaim, err)
			}
			e.releaseApproval(executionCtx, approvalGrant, err)
			code := classifyExecutionError(err)
			execErr := &ExecutionError{Code: code, ToolName: spec.Name, Attempt: attempt, Retryable: retryable(err), Cause: err}
			record("failed", code, attempt)
			return nil, execErr
		}
		if err := waitBackoff(executionCtx, policy, attempt); err != nil {
			if e.idempotency != nil && idempotencyClaim.ExecutionID != "" {
				e.failIdempotency(executionCtx, spec.Name, approvalGrant, idempotencyClaim, err)
			}
			e.releaseApproval(executionCtx, approvalGrant, err)
			code := classifyExecutionError(err)
			execErr := &ExecutionError{Code: code, ToolName: spec.Name, Attempt: attempt, Cause: err}
			record("failed", code, attempt)
			return nil, execErr
		}
	}
	execErr := &ExecutionError{Code: CodeExecutionFailed, ToolName: spec.Name, Attempt: policy.MaxAttempts, Cause: lastErr}
	record("failed", execErr.Code, policy.MaxAttempts)
	return nil, execErr
}

type executionSuspension interface {
	error
	ExecutionSuspended() bool
}

func executionSuspended(err error) bool {
	var suspended executionSuspension
	return errors.As(err, &suspended) && suspended.ExecutionSuspended()
}

func (e *Executor) completeApproval(ctx context.Context, grant ApprovalGrant) error {
	if grant.ApprovalID == "" {
		return nil
	}
	lifecycle, ok := e.approvals.(ApprovalLifecycleGate)
	if !ok {
		return nil
	}
	return lifecycle.Complete(ctx, grant)
}

func (e *Executor) releaseApproval(ctx context.Context, grant ApprovalGrant, cause error) {
	if grant.ApprovalID == "" {
		return
	}
	if lifecycle, ok := e.approvals.(ApprovalLifecycleGate); ok {
		if err := lifecycle.Release(ctx, grant, cause); err != nil {
			e.logGovernancePersistenceError(ctx, "", grant, IdempotencyClaim{}, "release tool approval", err)
		}
	}
}

func (e *Executor) failIdempotency(ctx context.Context, toolName string, grant ApprovalGrant, claim IdempotencyClaim, cause error) {
	if err := e.idempotency.Fail(ctx, claim, cause); err != nil {
		e.logGovernancePersistenceError(ctx, toolName, grant, claim, "persist tool execution failure", err)
	}
}

func (e *Executor) logGovernancePersistenceError(ctx context.Context, toolName string, grant ApprovalGrant, claim IdempotencyClaim, operation string, err error) {
	slog.ErrorContext(ctx, "agent tool governance persistence failed",
		"operation", operation,
		"tool", toolName,
		"approval_id", grant.ApprovalID,
		"execution_id", claim.ExecutionID,
		"error", err,
	)
}

func DigestInputs(inputs map[string]interface{}) string {
	payload, err := json.Marshal(inputs)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func digestToolValue(value interface{}) (string, int) {
	payload, err := json.Marshal(value)
	if err != nil || len(payload) == 0 {
		return "", 0
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), len(payload)
}

func buildResultSnapshot(outputs map[string]interface{}, maxBytes int) (ResultSnapshot, error) {
	payload, err := json.Marshal(outputs)
	if err != nil {
		return ResultSnapshot{}, fmt.Errorf("encode tool result: %w", err)
	}
	sum := sha256.Sum256(payload)
	snapshot := ResultSnapshot{
		Outputs: cloneInputs(outputs), Payload: payload,
		Digest: hex.EncodeToString(sum[:]), Length: len(payload),
	}
	if maxBytes > 0 && len(payload) > maxBytes {
		return snapshot, fmt.Errorf("%w: got %d bytes, limit %d", ErrResultTooLarge, len(payload), maxBytes)
	}
	return snapshot, nil
}

func resultReferenceURI(reference *ResultReference) string {
	if reference == nil {
		return ""
	}
	return reference.URI()
}

func NewAttemptID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return fmt.Sprintf("attempt-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(value[:])
}

func (e *Executor) authorizeIdentity(ctx context.Context, identity CallerIdentity, spec ToolSpec) *ExecutionError {
	if spec.Permission == PermissionInternal && !identity.Internal {
		return &ExecutionError{Code: CodeForbidden, ToolName: spec.Name, Cause: ErrForbidden}
	}
	if spec.Permission == PermissionAuthenticated && identity.UserID == 0 {
		return &ExecutionError{Code: CodeUnauthenticated, ToolName: spec.Name, Cause: ErrUnauthenticated}
	}
	if err := ctx.Err(); err != nil {
		return &ExecutionError{Code: classifyExecutionError(err), ToolName: spec.Name, Cause: err}
	}
	return nil
}

func waitBackoff(ctx context.Context, policy RetryPolicy, attempt int) error {
	delay := policy.InitialBackoff << (attempt - 1)
	if delay > policy.MaxBackoff {
		delay = policy.MaxBackoff
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func retryable(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var retryable interface{ Retryable() bool }
	if errors.As(err, &retryable) {
		return retryable.Retryable()
	}
	var networkError net.Error
	return errors.As(err, &networkError) && (networkError.Timeout() || networkError.Temporary())
}

func classifyExecutionError(err error) ErrorCode {
	switch {
	case errors.Is(err, context.Canceled):
		return CodeCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return CodeTimeout
	default:
		return CodeExecutionFailed
	}
}

func breakerFailure(ctx context.Context, err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || ctx.Err() != nil {
		return false
	}
	return true
}

var defaultSensitiveFields = map[string]struct{}{
	"api_key": {}, "apikey": {}, "authorization": {}, "cookie": {}, "credential": {},
	"password": {}, "secret": {}, "token": {}, "access_token": {}, "refresh_token": {},
	"content": {}, "message": {}, "prompt": {}, "query": {}, "system_prompt": {}, "text": {},
}

func RedactInputs(inputs map[string]interface{}, sensitiveFields []string) map[string]interface{} {
	sensitive := make(map[string]struct{}, len(defaultSensitiveFields)+len(sensitiveFields))
	for field := range defaultSensitiveFields {
		sensitive[field] = struct{}{}
	}
	for _, field := range sensitiveFields {
		sensitive[strings.ToLower(strings.TrimSpace(field))] = struct{}{}
	}
	return redactMap(inputs, sensitive)
}

func redactMap(inputs map[string]interface{}, sensitive map[string]struct{}) map[string]interface{} {
	redacted := make(map[string]interface{}, len(inputs))
	for key, value := range inputs {
		if _, ok := sensitive[strings.ToLower(strings.TrimSpace(key))]; ok {
			redacted[key] = "[REDACTED]"
			continue
		}
		switch nested := value.(type) {
		case map[string]interface{}:
			redacted[key] = redactMap(nested, sensitive)
		case []interface{}:
			items := make([]interface{}, len(nested))
			for i, item := range nested {
				if object, ok := item.(map[string]interface{}); ok {
					items[i] = redactMap(object, sensitive)
				} else {
					items[i] = item
				}
			}
			redacted[key] = items
		default:
			redacted[key] = value
		}
	}
	return redacted
}

type executionContextKey struct{}

type ExecutionMetadata struct {
	RunID                  string
	WorkflowID             string
	WorkflowRevisionID     string
	WorkflowRevisionNumber int64
	StepID                 string
	Source                 ExecutionSource
	IdempotencyKey         string
}

func InjectExecutionMetadata(ctx context.Context, metadata ExecutionMetadata) context.Context {
	return context.WithValue(ctx, executionContextKey{}, metadata)
}

func ExecutionMetadataFromContext(ctx context.Context) ExecutionMetadata {
	metadata, _ := ctx.Value(executionContextKey{}).(ExecutionMetadata)
	return metadata
}

func cloneInputs(inputs map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(inputs))
	for key, value := range inputs {
		cloned[key] = cloneValue(value)
	}
	return cloned
}

func cloneValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		return cloneInputs(typed)
	case []interface{}:
		cloned := make([]interface{}, len(typed))
		for i, item := range typed {
			cloned[i] = cloneValue(item)
		}
		return cloned
	default:
		return value
	}
}
