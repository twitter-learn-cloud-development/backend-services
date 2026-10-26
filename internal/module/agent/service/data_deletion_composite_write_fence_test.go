package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	agentdeletion "twitter-clone/internal/module/agent/deletion"
)

type compositeAgentFenceFake struct {
	receipt agentdeletion.WriteFenceReceipt
	calls   int
}

func (f *compositeAgentFenceFake) DeletionCapability() agentdeletion.CapabilityDescriptor {
	return agentdeletion.CapabilityDescriptor{
		ID:      agentdeletion.CapabilitySubjectWriteFence,
		Adapter: "repository.mongo-subject-write-fence",
		Version: "v1",
	}
}

func (f *compositeAgentFenceFake) EnforceDataDeletionFence(
	_ context.Context,
	_ agentdeletion.WriteFenceRequest,
) (agentdeletion.WriteFenceReceipt, error) {
	f.calls++
	return f.receipt, nil
}

type compositeTweetFenceFake struct {
	evidence agentdeletion.WriteFenceAuthorityEvidence
	err      error
	calls    int
}

func (f *compositeTweetFenceFake) EnforceSubjectWriteFence(
	_ context.Context,
	_ agentdeletion.WriteFenceRequest,
) (agentdeletion.WriteFenceAuthorityEvidence, error) {
	f.calls++
	return f.evidence, f.err
}

func TestCompositeDataDeletionSubjectWriteFenceBuildsBoundReceipt(t *testing.T) {
	now := time.Now().UTC()
	request := compositeWriteFenceRequest(t, now)
	agentReceipt, err := agentdeletion.NewWriteFenceReceipt(request, 2, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	agentFence := &compositeAgentFenceFake{receipt: agentReceipt}
	tweetFence := &compositeTweetFenceFake{evidence: agentdeletion.WriteFenceAuthorityEvidence{
		Authority: "tweet-service.mysql", ReceiptDigest: compositeDigest("tweet"),
		FenceRevision: 5, CompletedAt: now.Add(2 * time.Second),
	}}
	composite, err := NewCompositeDataDeletionSubjectWriteFence(agentFence, tweetFence)
	if err != nil {
		t.Fatal(err)
	}
	composite.now = func() time.Time { return now.Add(3 * time.Second) }
	receipt, err := composite.EnforceDataDeletionFence(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if agentFence.calls != 1 || tweetFence.calls != 1 ||
		receipt.SchemaVersion != agentdeletion.WriteFenceReceiptSchemaV2 ||
		receipt.AuthorityCount != 2 || receipt.FenceRevision != 5 {
		t.Fatalf("unexpected composite receipt=%+v calls=(%d,%d)", receipt, agentFence.calls, tweetFence.calls)
	}
	if err := agentdeletion.VerifyWriteFenceReceipt(request, receipt, now.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	tweetFence.evidence.Authority = "unexpected"
	if _, err := composite.EnforceDataDeletionFence(context.Background(), request); err == nil {
		t.Fatal("accepted another authority in place of Tweet")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := agentFence.calls
	if _, err := composite.EnforceDataDeletionFence(ctx, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request error=%v", err)
	}
	if agentFence.calls != calls {
		t.Fatal("canceled request installed a fence")
	}
}

func TestCompositeDataDeletionSubjectWriteFenceKeepsPartialFenceFailClosed(t *testing.T) {
	now := time.Now().UTC()
	request := compositeWriteFenceRequest(t, now)
	agentReceipt, err := agentdeletion.NewWriteFenceReceipt(request, 1, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	agentFence := &compositeAgentFenceFake{receipt: agentReceipt}
	tweetFence := &compositeTweetFenceFake{err: errors.New("tweet unavailable")}
	composite, err := NewCompositeDataDeletionSubjectWriteFence(agentFence, tweetFence)
	if err != nil {
		t.Fatal(err)
	}
	composite.now = func() time.Time { return now.Add(2 * time.Second) }
	if _, err := composite.EnforceDataDeletionFence(context.Background(), request); err == nil {
		t.Fatal("partial fence failure was accepted")
	}
	if agentFence.calls != 1 || tweetFence.calls != 1 {
		t.Fatalf("unexpected partial fence calls=(%d,%d)", agentFence.calls, tweetFence.calls)
	}
}

func compositeWriteFenceRequest(t *testing.T, now time.Time) agentdeletion.WriteFenceRequest {
	t.Helper()
	governance, err := agentdeletion.NewGovernanceDecision(
		42, agentdeletion.GovernanceClear, 3, compositeDigest("clear"), now, now.Add(time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	return agentdeletion.WriteFenceRequest{
		RequestID: "request-1", UserID: 42, SubjectDigest: agentdeletion.DeletionSubjectDigest(42),
		PlanDigest: compositeDigest("plan"), CapabilityDigest: compositeDigest("capabilities"),
		Governance: governance, Deadline: now.Add(time.Minute),
	}
}

func compositeDigest(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
