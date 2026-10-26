package deletion

import (
	"testing"
	"time"
)

func TestAuditTrailBindsClaimAndCompletionWithoutLeaseSecret(t *testing.T) {
	now := time.Date(2026, 9, 25, 16, 0, 0, 0, time.UTC)
	protocol, err := BuildProtocol(testInventory(), allCapabilitySnapshot(t), now)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := ClaimNextStage(&protocol.Checkpoint, protocol.Plan, 1, "secret-lease", now, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	claimEvent, err := NewAuditEvent(AuditEventInput{
		RequestID: "507f1f77bcf86cd799439011", Sequence: 1, Type: AuditStageClaimed,
		Stage: *claimed, RequestRevision: 8, CheckpointRevision: 2,
		PlanDigest: protocol.Plan.Digest, CapabilityDigest: protocol.Plan.CapabilityDigest,
		LeaseToken: "secret-lease", OccurredAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if claimEvent.LeaseDigest == "secret-lease" || claimEvent.LeaseDigest == "" {
		t.Fatalf("lease token was not safely digested: %+v", claimEvent)
	}
	receipt := sha256Hex("receipt")
	if err := CompleteStage(&protocol.Checkpoint, protocol.Plan, 2, "secret-lease", StageSucceeded, receipt, "", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	completed := protocol.Checkpoint.Stages[0]
	completionEvent, err := NewAuditEvent(AuditEventInput{
		RequestID: "507f1f77bcf86cd799439011", Sequence: 2, Type: AuditStageSucceeded,
		Stage: completed, RequestRevision: 9, CheckpointRevision: 3,
		PlanDigest: protocol.Plan.Digest, CapabilityDigest: protocol.Plan.CapabilityDigest,
		LeaseToken: "secret-lease", PreviousDigest: claimEvent.EventDigest, OccurredAt: now.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	events := []AuditEvent{claimEvent, completionEvent}
	if err := ValidateAuditTrail(events, 2, completionEvent.EventDigest, protocol.Plan.Digest, protocol.Plan.CapabilityDigest); err != nil {
		t.Fatal(err)
	}
}

func TestAuditTrailRejectsTamperingAndUnpairedCompletion(t *testing.T) {
	now := time.Date(2026, 9, 25, 16, 0, 0, 0, time.UTC)
	protocol, err := BuildProtocol(testInventory(), allCapabilitySnapshot(t), now)
	if err != nil {
		t.Fatal(err)
	}
	stage := protocol.Checkpoint.Stages[0]
	stage.Status = StageSucceeded
	stage.Attempts = 1
	stage.ReceiptDigest = sha256Hex("receipt")
	event, err := NewAuditEvent(AuditEventInput{
		RequestID: "507f1f77bcf86cd799439011", Sequence: 1, Type: AuditStageSucceeded,
		Stage: stage, RequestRevision: 8, CheckpointRevision: 2,
		PlanDigest: protocol.Plan.Digest, CapabilityDigest: protocol.Plan.CapabilityDigest,
		LeaseToken: "lease", OccurredAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateAuditTrail([]AuditEvent{event}, 1, event.EventDigest, protocol.Plan.Digest, protocol.Plan.CapabilityDigest); err == nil {
		t.Fatal("unpaired completion was accepted")
	}
	event.StageID = StageDeleteMongo
	if err := ValidateAuditEvent(event); err == nil {
		t.Fatal("tampered audit event was accepted")
	}
}
