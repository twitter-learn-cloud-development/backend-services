package deletion

import (
	"context"
	"errors"
	"testing"
	"time"
)

type conformanceDeletionAdapter struct {
	descriptor CapabilityDescriptor
	now        time.Time
	remaining  int64
	receipts   map[string]ExecutionReceipt
	mutations  int
}

func (adapter *conformanceDeletionAdapter) DeletionCapability() CapabilityDescriptor {
	return adapter.descriptor
}

func (adapter *conformanceDeletionAdapter) Observe(_ context.Context, request ObservationRequest) (PostState, error) {
	if err := ValidateObservationRequest(request); err != nil {
		return PostState{}, err
	}
	return NewPostState(adapter.descriptor, request.SubjectDigest, request.ScopeDigest, adapter.remaining, adapter.now)
}

func (adapter *conformanceDeletionAdapter) Delete(ctx context.Context, request AdapterRequest) (AdapterResult, error) {
	if err := ValidateAdapterRequest(request, adapter.now); err != nil {
		return AdapterResult{}, err
	}
	if err := ValidateAdapterBinding(adapter, request); err != nil {
		return AdapterResult{}, err
	}
	if receipt, ok := adapter.receipts[request.OperationKey]; ok {
		return AdapterResult{
			Receipt: receipt, Replayed: true, AuthorizationDigest: request.Governance.Digest,
		}, nil
	}
	before, err := adapter.Observe(ctx, request.ObservationRequest())
	if err != nil {
		return AdapterResult{}, err
	}
	adapter.remaining = 0
	adapter.mutations++
	adapter.now = adapter.now.Add(time.Second)
	after, err := adapter.Observe(ctx, request.ObservationRequest())
	if err != nil {
		return AdapterResult{}, err
	}
	receipt, err := NewExecutionReceipt(request, before, after, adapter.now)
	if err != nil {
		return AdapterResult{}, err
	}
	adapter.receipts[request.OperationKey] = receipt
	return AdapterResult{Receipt: receipt, AuthorizationDigest: request.Governance.Digest}, nil
}

func newAdapterContractFixture(t *testing.T, status GovernanceStatus) (*conformanceDeletionAdapter, AdapterRequest, time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 25, 17, 0, 0, 0, time.UTC)
	descriptor := CapabilityDescriptor{ID: CapabilityMongoOwnedDelete, Adapter: "mongo/owned-delete", Version: "v1"}
	decision, err := NewGovernanceDecision(
		42, status, 7, sha256Hex("retention-subject-revision-7"), now, now.Add(5*time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewAdapterRequest(AdapterRequestInput{
		RequestID: "507f1f77bcf86cd799439011", StageID: StageDeleteMongo, UserID: 42,
		PlanDigest: sha256Hex("plan"), CapabilityDigest: sha256Hex("capability-snapshot"),
		Capability: descriptor, ScopeDigest: DeletionScopeDigest("dialogues", "workflow_runs"),
		Governance: decision, Now: now, Deadline: now.Add(2 * time.Minute),
	})
	if status == GovernanceHeld {
		if !errors.Is(err, ErrDeletionLegalHold) {
			t.Fatalf("NewAdapterRequest() error = %v, want legal hold", err)
		}
		request = AdapterRequest{
			SchemaVersion: AdapterContractSchemaV1, RequestID: "507f1f77bcf86cd799439011",
			StageID: StageDeleteMongo, UserID: 42, SubjectDigest: DeletionSubjectDigest(42),
			PlanDigest: sha256Hex("plan"), CapabilityDigest: sha256Hex("capability-snapshot"),
			Capability: descriptor, ScopeDigest: DeletionScopeDigest("dialogues", "workflow_runs"),
			Governance: decision, Deadline: now.Add(2 * time.Minute),
		}
		request.OperationKey = AdapterOperationKey(request)
	} else if err != nil {
		t.Fatal(err)
	}
	return &conformanceDeletionAdapter{
		descriptor: descriptor, now: now, remaining: 3, receipts: make(map[string]ExecutionReceipt),
	}, request, now
}

func TestDeletionAdapterContractIsIdempotentAndPostStateVerified(t *testing.T) {
	adapter, request, now := newAdapterContractFixture(t, GovernanceClear)
	result, err := adapter.Delete(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	postState, err := adapter.Observe(context.Background(), request.ObservationRequest())
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyAdapterResult(adapter, request, result, postState, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	replayed, err := adapter.Delete(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.Replayed || replayed.Receipt.ReceiptDigest != result.Receipt.ReceiptDigest || adapter.mutations != 1 {
		t.Fatalf("idempotent replay changed mutation or receipt: replay=%+v mutations=%d", replayed, adapter.mutations)
	}
	if err := VerifyAdapterResult(adapter, request, replayed, postState, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
}

func TestDeletionAdapterReplayAcceptsRefreshedClearDecisionWithoutRewritingReceipt(t *testing.T) {
	adapter, request, now := newAdapterContractFixture(t, GovernanceClear)
	first, err := adapter.Delete(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	refreshed, err := NewGovernanceDecision(
		42, GovernanceClear, 8, sha256Hex("retention-subject-revision-8"),
		now.Add(time.Second), now.Add(6*time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Governance = refreshed
	request.Deadline = now.Add(5 * time.Minute)
	adapter.now = now.Add(2 * time.Second)
	replayed, err := adapter.Delete(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	postState, err := adapter.Observe(context.Background(), request.ObservationRequest())
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.Replayed || replayed.Receipt.ReceiptDigest != first.Receipt.ReceiptDigest ||
		replayed.AuthorizationDigest != refreshed.Digest || adapter.mutations != 1 {
		t.Fatalf("refreshed replay is invalid: result=%+v mutations=%d", replayed, adapter.mutations)
	}
	if err := VerifyAdapterResult(adapter, request, replayed, postState, adapter.now); err != nil {
		t.Fatal(err)
	}
}

func TestDeletionAdapterContractRejectsHeldAndExpiredGovernance(t *testing.T) {
	adapter, heldRequest, _ := newAdapterContractFixture(t, GovernanceHeld)
	if _, err := adapter.Delete(context.Background(), heldRequest); !errors.Is(err, ErrDeletionLegalHold) {
		t.Fatalf("Delete() error = %v, want legal hold", err)
	}
	if adapter.mutations != 0 {
		t.Fatal("held request mutated adapter state")
	}

	adapter, request, now := newAdapterContractFixture(t, GovernanceClear)
	request.Deadline = now.Add(10 * time.Minute)
	adapter.now = now.Add(6 * time.Minute)
	if _, err := adapter.Delete(context.Background(), request); !errors.Is(err, ErrDeletionGovernanceExpired) {
		t.Fatalf("Delete() error = %v, want expired governance", err)
	}
	if adapter.mutations != 0 {
		t.Fatal("expired governance request mutated adapter state")
	}
}

func TestDeletionAdapterContractRejectsForgedReceiptAndResidualState(t *testing.T) {
	adapter, request, now := newAdapterContractFixture(t, GovernanceClear)
	result, err := adapter.Delete(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	postState, err := adapter.Observe(context.Background(), request.ObservationRequest())
	if err != nil {
		t.Fatal(err)
	}
	forged := result
	forged.Receipt.AffectedCount++
	if err := VerifyAdapterResult(adapter, request, forged, postState, now.Add(time.Second)); err == nil {
		t.Fatal("forged receipt was accepted")
	}
	residual, err := NewPostState(request.Capability, request.SubjectDigest, request.ScopeDigest, 1, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyAdapterResult(adapter, request, result, residual, now.Add(2*time.Second)); !errors.Is(err, ErrDeletionReceiptMismatch) {
		t.Fatalf("VerifyAdapterResult() error = %v, want receipt mismatch", err)
	}
}

func TestDeletionAdapterContractRejectsStageCapabilityAndOperationKeyDrift(t *testing.T) {
	_, request, now := newAdapterContractFixture(t, GovernanceClear)
	request.StageID = StageDeleteMinIO
	if err := ValidateAdapterRequest(request, now); err == nil {
		t.Fatal("stage capability drift was accepted")
	}
	_, request, now = newAdapterContractFixture(t, GovernanceClear)
	request.ScopeDigest = DeletionScopeDigest("different-scope")
	if err := ValidateAdapterRequest(request, now); err == nil {
		t.Fatal("operation-key drift was accepted")
	}
}

func TestDeletionAdapterContractRejectsGovernanceTampering(t *testing.T) {
	_, request, now := newAdapterContractFixture(t, GovernanceClear)
	request.Governance.Revision++
	if err := ValidateAdapterRequest(request, now); err == nil {
		t.Fatal("tampered governance decision was accepted")
	}
}

func TestDeletionAdapterContractRejectsImplementationVersionDriftBeforeMutation(t *testing.T) {
	adapter, request, _ := newAdapterContractFixture(t, GovernanceClear)
	request.Capability.Version = "v2"
	request.OperationKey = AdapterOperationKey(request)
	if _, err := adapter.Delete(context.Background(), request); err == nil {
		t.Fatal("adapter implementation drift was accepted")
	}
	if adapter.mutations != 0 {
		t.Fatal("adapter implementation drift mutated state")
	}
}
