package deletion

import (
	"context"
	"errors"
	"strings"
	"time"
)

var ErrExternalDeletionOperationBusy = errors.New("external data deletion operation is already running")

// ExternalDeletionResource is the smallest storage-specific boundary needed
// by the governed adapter. Implementations must keep both operations scoped to
// exactly one subject and make DeleteSubject safe to repeat.
type ExternalDeletionResource interface {
	CountSubject(context.Context, uint64) (int64, error)
	DeleteSubject(context.Context, uint64) error
}

// ExternalDeletionClaim is durable coordination state. BeforeState is stored
// before the external mutation so a retry can reconstruct the same receipt
// after a process crash.
type ExternalDeletionClaim struct {
	LeaseToken  string
	BeforeState PostState
	Receipt     *ExecutionReceipt
	Replayed    bool
}

// ExternalDeletionJournal owns governance revalidation, exclusive operation
// leasing and durable receipt persistence.
type ExternalDeletionJournal interface {
	Claim(context.Context, AdapterRequest, PostState, time.Time) (ExternalDeletionClaim, error)
	Complete(context.Context, AdapterRequest, string, ExecutionReceipt, time.Time) error
}

type ExternalDeletionAdapter struct {
	descriptor  CapabilityDescriptor
	scopeDigest string
	resource    ExternalDeletionResource
	journal     ExternalDeletionJournal
	now         func() time.Time
}

func NewExternalDeletionAdapter(
	descriptor CapabilityDescriptor,
	scopeDigest string,
	resource ExternalDeletionResource,
	journal ExternalDeletionJournal,
) (*ExternalDeletionAdapter, error) {
	descriptor = normalizeCapabilityDescriptor(descriptor)
	if err := validateCapabilityDescriptor(descriptor); err != nil {
		return nil, err
	}
	if descriptor.ID != CapabilityMinIOSubjectDelete && descriptor.ID != CapabilityQdrantSubjectDelete {
		return nil, errors.New("external data deletion adapter capability is not supported")
	}
	if !isSHA256(strings.ToLower(strings.TrimSpace(scopeDigest))) || resource == nil || journal == nil {
		return nil, errors.New("external data deletion adapter configuration is invalid")
	}
	return &ExternalDeletionAdapter{
		descriptor: descriptor, scopeDigest: strings.ToLower(strings.TrimSpace(scopeDigest)),
		resource: resource, journal: journal, now: time.Now,
	}, nil
}

func (a *ExternalDeletionAdapter) DeletionCapability() CapabilityDescriptor {
	if a == nil {
		return CapabilityDescriptor{}
	}
	return a.descriptor
}

func (a *ExternalDeletionAdapter) Observe(ctx context.Context, request ObservationRequest) (PostState, error) {
	if err := a.validateObservation(request); err != nil {
		return PostState{}, err
	}
	count, err := a.resource.CountSubject(ctx, request.UserID)
	if err != nil {
		return PostState{}, err
	}
	return NewPostState(request.Capability, request.SubjectDigest, request.ScopeDigest, count, a.now().UTC())
}

func (a *ExternalDeletionAdapter) Delete(ctx context.Context, request AdapterRequest) (AdapterResult, error) {
	if a == nil || a.resource == nil || a.journal == nil || a.now == nil {
		return AdapterResult{}, errors.New("external data deletion adapter is unavailable")
	}
	now := a.now().UTC()
	if err := ValidateAdapterRequest(request, now); err != nil {
		return AdapterResult{}, err
	}
	if err := ValidateAdapterBinding(a, request); err != nil {
		return AdapterResult{}, err
	}
	if request.ScopeDigest != a.scopeDigest {
		return AdapterResult{}, errors.New("external data deletion scope does not match the fixed manifest")
	}

	beforeCount, err := a.resource.CountSubject(ctx, request.UserID)
	if err != nil {
		return AdapterResult{}, err
	}
	beforeState, err := NewPostState(
		request.Capability, request.SubjectDigest, request.ScopeDigest, beforeCount, now,
	)
	if err != nil {
		return AdapterResult{}, err
	}
	claim, err := a.journal.Claim(ctx, request, beforeState, now)
	if err != nil {
		return AdapterResult{}, err
	}
	if claim.Replayed {
		return a.replay(ctx, request, claim)
	}
	if err := validateExternalDeletionClaim(request, claim); err != nil {
		return AdapterResult{}, err
	}
	if err := a.resource.DeleteSubject(ctx, request.UserID); err != nil {
		return AdapterResult{}, err
	}

	completedAt := a.now().UTC()
	afterCount, err := a.resource.CountSubject(ctx, request.UserID)
	if err != nil {
		return AdapterResult{}, err
	}
	if afterCount != 0 {
		return AdapterResult{}, ErrDeletionResidualsRemain
	}
	afterState, err := NewPostState(
		request.Capability, request.SubjectDigest, request.ScopeDigest, afterCount, completedAt,
	)
	if err != nil {
		return AdapterResult{}, err
	}
	receipt, err := NewExecutionReceipt(request, claim.BeforeState, afterState, completedAt)
	if err != nil {
		return AdapterResult{}, err
	}
	if err := a.journal.Complete(ctx, request, claim.LeaseToken, receipt, completedAt); err != nil {
		return AdapterResult{}, err
	}
	return AdapterResult{Receipt: receipt, AuthorizationDigest: request.Governance.Digest}, nil
}

func (a *ExternalDeletionAdapter) replay(
	ctx context.Context,
	request AdapterRequest,
	claim ExternalDeletionClaim,
) (AdapterResult, error) {
	if claim.Receipt == nil || claim.LeaseToken != "" {
		return AdapterResult{}, errors.New("persisted external deletion replay is invalid")
	}
	if err := validateExternalReceiptBinding(*claim.Receipt, request); err != nil {
		return AdapterResult{}, err
	}
	count, err := a.resource.CountSubject(ctx, request.UserID)
	if err != nil {
		return AdapterResult{}, err
	}
	if count != 0 {
		return AdapterResult{}, ErrDeletionResidualsRemain
	}
	return AdapterResult{
		Receipt: *claim.Receipt, Replayed: true,
		AuthorizationDigest: request.Governance.Digest,
	}, nil
}

func (a *ExternalDeletionAdapter) validateObservation(request ObservationRequest) error {
	if a == nil || a.resource == nil || a.now == nil {
		return errors.New("external data deletion adapter is unavailable")
	}
	if err := ValidateObservationRequest(request); err != nil {
		return err
	}
	if request.Capability != a.descriptor || request.ScopeDigest != a.scopeDigest {
		return errors.New("external data deletion observation binding is invalid")
	}
	return nil
}

func validateExternalDeletionClaim(request AdapterRequest, claim ExternalDeletionClaim) error {
	if claim.Replayed || strings.TrimSpace(claim.LeaseToken) == "" || claim.Receipt != nil {
		return errors.New("external data deletion claim is invalid")
	}
	if err := ValidatePostState(claim.BeforeState); err != nil {
		return err
	}
	if claim.BeforeState.Capability != request.Capability ||
		claim.BeforeState.SubjectDigest != request.SubjectDigest ||
		claim.BeforeState.ScopeDigest != request.ScopeDigest {
		return errors.New("external data deletion claim does not match the request")
	}
	return nil
}

func validateExternalReceiptBinding(receipt ExecutionReceipt, request AdapterRequest) error {
	if err := ValidateExecutionReceipt(receipt); err != nil {
		return err
	}
	if receipt.RequestID != request.RequestID || receipt.StageID != request.StageID ||
		receipt.SubjectDigest != request.SubjectDigest || receipt.PlanDigest != request.PlanDigest ||
		receipt.CapabilityDigest != request.CapabilityDigest || receipt.Capability != request.Capability ||
		receipt.ScopeDigest != request.ScopeDigest || receipt.OperationKey != request.OperationKey {
		return errors.New("persisted external deletion receipt does not match the request")
	}
	return nil
}
