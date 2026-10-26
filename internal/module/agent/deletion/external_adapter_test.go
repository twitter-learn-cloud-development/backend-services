package deletion

import (
	"context"
	"errors"
	"testing"
	"time"
)

type externalDeletionResourceFake struct {
	count       int64
	deleteCalls int
	residual    int64
	deleteErr   error
}

func (f *externalDeletionResourceFake) CountSubject(context.Context, uint64) (int64, error) {
	return f.count, nil
}

func (f *externalDeletionResourceFake) DeleteSubject(context.Context, uint64) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleteCalls++
	f.count = f.residual
	return nil
}

type externalDeletionJournalFake struct {
	before        PostState
	receipt       *ExecutionReceipt
	claimErr      error
	completeErr   error
	claimCalls    int
	completeCalls int
}

func (f *externalDeletionJournalFake) Claim(
	_ context.Context,
	_ AdapterRequest,
	before PostState,
	_ time.Time,
) (ExternalDeletionClaim, error) {
	f.claimCalls++
	if f.claimErr != nil {
		return ExternalDeletionClaim{}, f.claimErr
	}
	if f.receipt != nil {
		return ExternalDeletionClaim{Receipt: f.receipt, Replayed: true}, nil
	}
	if f.before.SchemaVersion == "" {
		f.before = before
	}
	return ExternalDeletionClaim{LeaseToken: "lease-1", BeforeState: f.before}, nil
}

func (f *externalDeletionJournalFake) Complete(
	_ context.Context,
	_ AdapterRequest,
	_ string,
	receipt ExecutionReceipt,
	_ time.Time,
) error {
	f.completeCalls++
	if f.completeErr != nil {
		return f.completeErr
	}
	f.receipt = &receipt
	return nil
}

func newExternalAdapterFixture(t *testing.T) (*ExternalDeletionAdapter, AdapterRequest, *externalDeletionResourceFake, *externalDeletionJournalFake, time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 25, 18, 0, 0, 0, time.UTC)
	descriptor := CapabilityDescriptor{ID: CapabilityMinIOSubjectDelete, Adapter: "minio/tool-results", Version: "v1"}
	scope := DeletionScopeDigest("minio", "tenant-prefix")
	decision, err := NewGovernanceDecision(42, GovernanceClear, 3, sha256Hex("governance-3"), now, now.Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewAdapterRequest(AdapterRequestInput{
		RequestID: "507f1f77bcf86cd799439011", StageID: StageDeleteMinIO, UserID: 42,
		PlanDigest: sha256Hex("plan"), CapabilityDigest: sha256Hex("capabilities"),
		Capability: descriptor, ScopeDigest: scope, Governance: decision,
		Now: now, Deadline: now.Add(4 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	resource := &externalDeletionResourceFake{count: 3}
	journal := &externalDeletionJournalFake{}
	adapter, err := NewExternalDeletionAdapter(descriptor, scope, resource, journal)
	if err != nil {
		t.Fatal(err)
	}
	adapter.now = func() time.Time { return now }
	return adapter, request, resource, journal, now
}

func TestExternalDeletionAdapterPersistsReceiptAndReplaysWithoutMutation(t *testing.T) {
	adapter, request, resource, journal, now := newExternalAdapterFixture(t)
	result, err := adapter.Delete(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if resource.deleteCalls != 1 || journal.completeCalls != 1 || result.Receipt.BeforeCount != 3 || result.Receipt.AfterCount != 0 {
		t.Fatalf("unexpected first deletion: result=%+v deletes=%d completes=%d", result, resource.deleteCalls, journal.completeCalls)
	}
	postState, err := adapter.Observe(t.Context(), request.ObservationRequest())
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyAdapterResult(adapter, request, result, postState, now); err != nil {
		t.Fatal(err)
	}

	refreshed, err := NewGovernanceDecision(42, GovernanceClear, 4, sha256Hex("governance-4"), now, now.Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	request.Governance = refreshed
	replayed, err := adapter.Delete(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.Replayed || replayed.Receipt.ReceiptDigest != result.Receipt.ReceiptDigest ||
		replayed.AuthorizationDigest != refreshed.Digest || resource.deleteCalls != 1 || journal.completeCalls != 1 {
		t.Fatalf("unexpected replay: result=%+v deletes=%d completes=%d", replayed, resource.deleteCalls, journal.completeCalls)
	}
}

func TestExternalDeletionAdapterRejectsResidualAndJournalFailure(t *testing.T) {
	adapter, request, resource, journal, _ := newExternalAdapterFixture(t)
	resource.residual = 1
	if _, err := adapter.Delete(t.Context(), request); !errors.Is(err, ErrDeletionResidualsRemain) {
		t.Fatalf("Delete() error = %v, want residuals", err)
	}
	if journal.completeCalls != 0 {
		t.Fatal("residual deletion persisted a completion receipt")
	}

	adapter, request, resource, journal, _ = newExternalAdapterFixture(t)
	journal.claimErr = ErrExternalDeletionOperationBusy
	if _, err := adapter.Delete(t.Context(), request); !errors.Is(err, ErrExternalDeletionOperationBusy) {
		t.Fatalf("Delete() error = %v, want busy", err)
	}
	if resource.deleteCalls != 0 {
		t.Fatal("busy operation mutated the external resource")
	}
}

func TestExternalDeletionAdapterReconcilesCrashAfterExternalMutation(t *testing.T) {
	adapter, request, resource, journal, now := newExternalAdapterFixture(t)
	persistedBefore, err := NewPostState(
		request.Capability, request.SubjectDigest, request.ScopeDigest, 3, now.Add(-time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	journal.before = persistedBefore
	resource.count = 0

	result, err := adapter.Delete(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Receipt.BeforeCount != 3 || result.Receipt.AffectedCount != 3 || result.Receipt.AfterCount != 0 {
		t.Fatalf("crash reconciliation lost persisted before-state: %+v", result.Receipt)
	}
	if resource.deleteCalls != 1 || journal.completeCalls != 1 {
		t.Fatalf("crash reconciliation did not converge once: deletes=%d completes=%d", resource.deleteCalls, journal.completeCalls)
	}
}

func TestExternalDeletionAdapterRejectsScopeDriftBeforeMutation(t *testing.T) {
	adapter, request, resource, _, _ := newExternalAdapterFixture(t)
	request.ScopeDigest = DeletionScopeDigest("different")
	request.OperationKey = AdapterOperationKey(request)
	if _, err := adapter.Delete(t.Context(), request); err == nil {
		t.Fatal("scope drift was accepted")
	}
	if resource.deleteCalls != 0 {
		t.Fatal("scope drift mutated the external resource")
	}
}
