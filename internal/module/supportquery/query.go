package supportquery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const (
	CodeInvalidArguments            = "invalid_arguments"
	CodeOperationForbidden          = "operation_forbidden"
	CodeOperationNotFound           = "operation_not_found"
	CodeResourceNotFound            = "resource_not_found"
	CodeToolCallConflict            = "tool_call_conflict"
	CodeOperationUnprocessable      = "operation_unprocessable"
	CodeQueryRateLimited            = "query_rate_limited"
	CodeUpstreamInvalidResponse     = "upstream_invalid_response"
	CodeQueryTemporarilyUnavailable = "query_temporarily_unavailable"
	CodeQueryTimeout                = "query_timeout"
)

var ErrToolCallConflict = errors.New(CodeToolCallConflict)

type Error struct {
	Status int
	Code   string
	Title  string
	Detail string
}

func (e *Error) Error() string {
	return e.Code
}

func NewError(status int, code, title, detail string) *Error {
	return &Error{Status: status, Code: code, Title: title, Detail: detail}
}

type RequestContext struct {
	CaseID     string `json:"case_id"`
	TaskID     string `json:"task_id"`
	ToolCallID string `json:"tool_call_id"`
}

type Request struct {
	Operation      string          `json:"operation"`
	Arguments      json.RawMessage `json:"arguments"`
	RequestContext RequestContext  `json:"request_context"`
}

type Evidence struct {
	EvidenceID  string         `json:"evidence_id"`
	SourceType  string         `json:"source_type"`
	SourceRef   string         `json:"source_ref"`
	ObservedAt  time.Time      `json:"observed_at"`
	SafeSummary string         `json:"safe_summary"`
	Attributes  map[string]any `json:"attributes"`
}

type Result struct {
	Data          map[string]any
	Evidence      []Evidence
	DataFreshness string
}

type Operation interface {
	Name() string
	Execute(ctx context.Context, actorUserID uint64, arguments json.RawMessage) (Result, error)
}

type Registry struct {
	operations map[string]Operation
}

func NewRegistry(operations ...Operation) (*Registry, error) {
	registered := make(map[string]Operation, len(operations))
	for _, operation := range operations {
		if operation == nil || operation.Name() == "" {
			return nil, fmt.Errorf("support query operation must have a name")
		}
		if _, exists := registered[operation.Name()]; exists {
			return nil, fmt.Errorf("duplicate support query operation %q", operation.Name())
		}
		registered[operation.Name()] = operation
	}
	return &Registry{operations: registered}, nil
}

func (r *Registry) Execute(
	ctx context.Context,
	actorUserID uint64,
	request Request,
) (Result, error) {
	operation, exists := r.operations[request.Operation]
	if !exists {
		return Result{}, NewError(
			http.StatusNotFound,
			CodeOperationNotFound,
			"Operation not found",
			"The requested support operation is not registered.",
		)
	}
	return operation.Execute(ctx, actorUserID, request.Arguments)
}

type IdempotencyGuard interface {
	Check(
		ctx context.Context,
		actorUserID uint64,
		toolCallID string,
		fingerprint string,
		ttl time.Duration,
	) error
}

type MemoryIdempotencyGuard struct {
	mu      sync.Mutex
	records map[string]idempotencyRecord
	now     func() time.Time
}

type idempotencyRecord struct {
	fingerprint string
	expiresAt   time.Time
}

func NewMemoryIdempotencyGuard() *MemoryIdempotencyGuard {
	return &MemoryIdempotencyGuard{
		records: make(map[string]idempotencyRecord),
		now:     time.Now,
	}
}

func (g *MemoryIdempotencyGuard) Check(
	ctx context.Context,
	actorUserID uint64,
	toolCallID string,
	fingerprint string,
	ttl time.Duration,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := g.now()
	key := fmt.Sprintf("%d:%s", actorUserID, toolCallID)
	g.mu.Lock()
	defer g.mu.Unlock()
	for candidate, record := range g.records {
		if !record.expiresAt.After(now) {
			delete(g.records, candidate)
		}
	}
	if record, exists := g.records[key]; exists {
		if record.fingerprint != fingerprint {
			return ErrToolCallConflict
		}
		return nil
	}
	g.records[key] = idempotencyRecord{fingerprint: fingerprint, expiresAt: now.Add(ttl)}
	return nil
}
