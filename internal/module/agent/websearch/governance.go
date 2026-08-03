package websearch

import (
	"context"
	"errors"
)

type AccessOperation string

const (
	AccessOperationSearch            AccessOperation = "search"
	AccessOperationPageRead          AccessOperation = "page_read"
	InternalUserIDArgument                           = "__web_access_user_id"
	InternalRunIDArgument                            = "__web_access_run_id"
	InternalProviderConfigIDArgument                 = "__web_provider_config_id"
)

var (
	ErrAccessIdentityRequired = errors.New("web access identity is required")
	ErrAccessRateLimited      = errors.New("web access rate limit exceeded")
	ErrAccessBudgetExceeded   = errors.New("web access run budget exceeded")
	ErrAccessGovernor         = errors.New("web access governor failed")
)

type AccessSubject struct {
	UserID uint64
	RunID  string
}

type AdmissionRequest struct {
	Subject    AccessSubject
	Operation  AccessOperation
	CostMicros int64
}

type AccessGovernor interface {
	Admit(context.Context, AdmissionRequest) error
}
