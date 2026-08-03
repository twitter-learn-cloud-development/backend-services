package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Category string

const (
	CategoryRead     Category = "read"
	CategoryWrite    Category = "write"
	CategoryRisky    Category = "risky"
	CategoryInternal Category = "internal"
)

type Permission string

const (
	PermissionAuthenticated Permission = "authenticated"
	PermissionInternal      Permission = "internal"
)

type ApprovalPolicy string

const (
	ApprovalNever    ApprovalPolicy = "never"
	ApprovalRequired ApprovalPolicy = "required"
)

type RetryPolicy struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

func (p RetryPolicy) normalized() RetryPolicy {
	if p.MaxAttempts < 1 {
		p.MaxAttempts = 1
	}
	if p.InitialBackoff <= 0 {
		p.InitialBackoff = 100 * time.Millisecond
	}
	if p.MaxBackoff <= 0 {
		p.MaxBackoff = time.Second
	}
	if p.MaxBackoff < p.InitialBackoff {
		p.MaxBackoff = p.InitialBackoff
	}
	return p
}

type IdempotencyPolicy struct {
	Required bool
}

// ToolSpec is immutable registration metadata. Provider clients and concrete
// business dependencies remain in ToolHandler implementations.
type ToolSpec struct {
	Name            string
	Description     string
	InputSchema     json.RawMessage
	Category        Category
	Permission      Permission
	Timeout         time.Duration
	Retry           RetryPolicy
	Idempotency     IdempotencyPolicy
	Approval        ApprovalPolicy
	SensitiveFields []string
}

func (s ToolSpec) Normalize() (ToolSpec, error) {
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" {
		return ToolSpec{}, errors.New("tool name is required")
	}
	if len(s.InputSchema) == 0 {
		s.InputSchema = json.RawMessage(`{"type":"object"}`)
	}
	s.Description = strings.TrimSpace(s.Description)
	if s.Category == "" {
		s.Category = CategoryRisky
	}
	switch s.Category {
	case CategoryRead, CategoryWrite, CategoryRisky, CategoryInternal:
	default:
		return ToolSpec{}, fmt.Errorf("tool %s has invalid category %q", s.Name, s.Category)
	}
	if s.Permission == "" {
		s.Permission = PermissionAuthenticated
	}
	switch s.Permission {
	case PermissionAuthenticated, PermissionInternal:
	default:
		return ToolSpec{}, fmt.Errorf("tool %s has invalid permission %q", s.Name, s.Permission)
	}
	if s.Timeout <= 0 {
		s.Timeout = 30 * time.Second
	}
	s.Retry = s.Retry.normalized()
	if s.Approval == "" {
		if s.Category == CategoryWrite || s.Category == CategoryRisky {
			s.Approval = ApprovalRequired
		} else {
			s.Approval = ApprovalNever
		}
	}
	if (s.Category == CategoryWrite || s.Category == CategoryRisky) && s.Approval != ApprovalRequired {
		return ToolSpec{}, fmt.Errorf("tool %s category %s must require approval", s.Name, s.Category)
	}
	if s.Category == CategoryInternal {
		s.Permission = PermissionInternal
	}
	return s, nil
}

func (s ToolSpec) RequiresApproval() bool {
	return s.Approval == ApprovalRequired || s.Category == CategoryWrite || s.Category == CategoryRisky
}

type ToolHandler interface {
	Execute(ctx context.Context, inputs map[string]interface{}) (map[string]interface{}, error)
}

// AgentTool is the compatibility surface for existing built-in tools. New
// registrations are stored as ToolSpec + ToolHandler inside ToolRegistry.
type AgentTool interface {
	ToolHandler
	Spec() ToolSpec
}

type HandlerFunc func(ctx context.Context, inputs map[string]interface{}) (map[string]interface{}, error)

func (f HandlerFunc) Execute(ctx context.Context, inputs map[string]interface{}) (map[string]interface{}, error) {
	return f(ctx, inputs)
}
