package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	agentdeletion "twitter-clone/internal/module/agent/deletion"
)

type subjectWriteFenceStoreFake struct {
	receipt        agentdeletion.WriteFenceReceipt
	enforceErr     error
	authorizeErr   error
	enforceCalls   int
	authorizeCalls int
}

func (f *subjectWriteFenceStoreFake) Enforce(
	_ context.Context,
	_ agentdeletion.WriteFenceRequest,
	_ time.Time,
) (agentdeletion.WriteFenceReceipt, error) {
	f.enforceCalls++
	return f.receipt, f.enforceErr
}

func (f *subjectWriteFenceStoreFake) AuthorizeWrite(_ context.Context, _ uint64) error {
	f.authorizeCalls++
	return f.authorizeErr
}

func TestMongoSubjectWriteFenceCapabilityAndDelegation(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	request, subject := subjectWriteFenceFixture(t, now)
	planned, _, err := planSubjectWriteFence(subject, request, now)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := agentdeletion.NewWriteFenceReceipt(request, planned.FenceRevision, now)
	if err != nil {
		t.Fatal(err)
	}
	store := &subjectWriteFenceStoreFake{receipt: receipt, authorizeErr: ErrDataDeletionSubjectWriteFenced}
	fence := &MongoSubjectWriteFence{store: store, now: func() time.Time { return now }}

	descriptor := fence.DeletionCapability()
	if descriptor.ID != agentdeletion.CapabilitySubjectWriteFence ||
		descriptor.Adapter != mongoSubjectWriteFenceAdapterName || descriptor.Version != "v1" {
		t.Fatalf("unexpected capability: %+v", descriptor)
	}
	got, err := fence.EnforceDataDeletionFence(context.Background(), request)
	if err != nil || got.ReceiptDigest != receipt.ReceiptDigest || store.enforceCalls != 1 {
		t.Fatalf("EnforceDataDeletionFence() = (%+v, %v), calls=%d", got, err, store.enforceCalls)
	}
	if err := fence.AuthorizeSubjectWrite(context.Background(), request.UserID); !errors.Is(err, ErrDataDeletionSubjectWriteFenced) ||
		store.authorizeCalls != 1 {
		t.Fatalf("AuthorizeSubjectWrite() = %v, calls=%d", err, store.authorizeCalls)
	}
}

func TestPlanSubjectWriteFenceSupportsFreshGovernanceReplay(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	request, subject := subjectWriteFenceFixture(t, now)
	planned, existing, err := planSubjectWriteFence(subject, request, now)
	if err != nil || existing {
		t.Fatalf("initial plan = (%+v, %t, %v)", planned, existing, err)
	}
	if err := validateSubjectWriteFenceDocument(planned, request.UserID); err != nil {
		t.Fatal(err)
	}

	subject.WriteFence = &planned
	replayAt := now.Add(time.Second)
	replayDecision, err := mongoOwnedGovernanceDecision(subject, request.UserID, replayAt, replayAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	replayRequest := request
	replayRequest.Governance = replayDecision
	replayRequest.Deadline = replayAt.Add(time.Minute)
	replayed, existing, err := planSubjectWriteFence(subject, replayRequest, replayAt)
	if err != nil || !existing || replayed != planned {
		t.Fatalf("replay plan = (%+v, %t, %v), want persisted fence", replayed, existing, err)
	}
}

func TestPlanSubjectWriteFenceRejectsConflictGovernanceDriftAndCorruption(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	request, subject := subjectWriteFenceFixture(t, now)
	planned, _, err := planSubjectWriteFence(subject, request, now)
	if err != nil {
		t.Fatal(err)
	}
	subject.WriteFence = &planned

	conflict := request
	conflict.PlanDigest = subjectWriteFenceTestDigest("different-plan")
	if _, _, err := planSubjectWriteFence(subject, conflict, now); !errors.Is(err, ErrDataDeletionWriteFenceConflict) {
		t.Fatalf("conflicting plan error = %v", err)
	}

	drifted := subject
	drifted.Revision++
	if _, _, err := planSubjectWriteFence(drifted, request, now); err == nil {
		t.Fatal("governance revision drift was accepted")
	}

	corrupt := subject
	corruptFence := planned
	corruptFence.CapabilityDigest = "not-a-digest"
	corrupt.WriteFence = &corruptFence
	if _, _, err := planSubjectWriteFence(corrupt, request, now); err == nil {
		t.Fatal("corrupt persisted fence was accepted")
	}
}

func TestPlanSubjectWriteFenceRejectsLegalHold(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	request, subject := subjectWriteFenceFixture(t, now)
	subject.Held = true
	subject.HoldUntil = now.Add(time.Hour)
	subject.Revision++
	heldDecision, err := mongoOwnedGovernanceDecision(subject, request.UserID, now, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	request.Governance = heldDecision
	if _, _, err := planSubjectWriteFence(subject, request, now); !errors.Is(err, agentdeletion.ErrDeletionLegalHold) {
		t.Fatalf("held subject error = %v", err)
	}
}

func TestPlanSubjectWriteFenceWaitsForInFlightWrites(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	request, subject := subjectWriteFenceFixture(t, now)
	subject.ActiveWrites = 1
	if _, _, err := planSubjectWriteFence(subject, request, now); err == nil {
		t.Fatal("installed a fence while a user write was active")
	}
	subject.ActiveWrites = -1
	if _, _, err := planSubjectWriteFence(subject, request, now); err == nil {
		t.Fatal("accepted a corrupt negative write count")
	}
	subject.ActiveWrites = 0
	if _, _, err := planSubjectWriteFence(subject, request, now); err != nil {
		t.Fatalf("idle subject rejected: %v", err)
	}
}

func subjectWriteFenceFixture(
	t *testing.T,
	now time.Time,
) (agentdeletion.WriteFenceRequest, dataRetentionSubjectDocument) {
	t.Helper()
	const userID uint64 = 42
	subject := dataRetentionSubjectDocument{
		ID: dataRetentionSubjectID(userID), UserID: userID, Revision: 3,
	}
	decision, err := mongoOwnedGovernanceDecision(subject, userID, now, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return agentdeletion.WriteFenceRequest{
		RequestID: "deletion-request-42", UserID: userID,
		SubjectDigest:    agentdeletion.DeletionSubjectDigest(userID),
		PlanDigest:       subjectWriteFenceTestDigest("plan"),
		CapabilityDigest: subjectWriteFenceTestDigest("capabilities"),
		Governance:       decision, Deadline: now.Add(time.Minute),
	}, subject
}

func subjectWriteFenceTestDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
