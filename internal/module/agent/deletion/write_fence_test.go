package deletion

import (
	"testing"
	"time"
)

func TestWriteFenceReceiptBindsGovernanceAndPlan(t *testing.T) {
	now := time.Date(2026, 9, 26, 9, 0, 0, 0, time.UTC)
	governance, err := NewGovernanceDecision(
		42, GovernanceClear, 3, sha256Hex("clear"), now, now.Add(time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	request := WriteFenceRequest{
		RequestID: "request-1", UserID: 42, SubjectDigest: DeletionSubjectDigest(42),
		PlanDigest: sha256Hex("plan"), CapabilityDigest: sha256Hex("capabilities"),
		Governance: governance, Deadline: now.Add(time.Minute),
	}
	receipt, err := NewWriteFenceReceipt(request, 7, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyWriteFenceReceipt(request, receipt, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	tampered := request
	tampered.PlanDigest = sha256Hex("other-plan")
	if err := VerifyWriteFenceReceipt(tampered, receipt, now.Add(2*time.Second)); err == nil {
		t.Fatal("write fence receipt accepted a different plan")
	}
}

func TestCompositeWriteFenceReceiptBindsEveryAuthority(t *testing.T) {
	now := time.Date(2026, 9, 26, 9, 0, 0, 0, time.UTC)
	governance, err := NewGovernanceDecision(
		42, GovernanceClear, 3, sha256Hex("clear"), now, now.Add(time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	request := WriteFenceRequest{
		RequestID: "request-1", UserID: 42, SubjectDigest: DeletionSubjectDigest(42),
		PlanDigest: sha256Hex("plan"), CapabilityDigest: sha256Hex("capabilities"),
		Governance: governance, Deadline: now.Add(time.Minute),
	}
	authorities := []WriteFenceAuthorityEvidence{
		{Authority: "tweet-service.mysql", ReceiptDigest: sha256Hex("tweet"), FenceRevision: 2, CompletedAt: now.Add(time.Second)},
		{Authority: "repository.mongo-subject-write-fence", ReceiptDigest: sha256Hex("agent"), FenceRevision: 7, CompletedAt: now},
	}
	receipt, err := NewCompositeWriteFenceReceipt(request, authorities, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if receipt.SchemaVersion != WriteFenceReceiptSchemaV2 || receipt.AuthorityCount != 2 ||
		receipt.FenceRevision != 7 || receipt.AuthorityDigest == "" {
		t.Fatalf("unexpected composite receipt: %+v", receipt)
	}
	if err := VerifyWriteFenceReceipt(request, receipt, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	tampered := receipt
	tampered.AuthorityDigest = sha256Hex("tampered")
	if err := ValidateWriteFenceReceipt(tampered); err == nil {
		t.Fatal("composite receipt accepted tampered authority evidence")
	}
	reordered := []WriteFenceAuthorityEvidence{authorities[1], authorities[0]}
	other, err := NewCompositeWriteFenceReceipt(request, reordered, receipt.CompletedAt)
	if err != nil || other.ReceiptDigest != receipt.ReceiptDigest {
		t.Fatalf("authority order changed receipt: %v", err)
	}
	for _, scenario := range []string{"duplicate", "whitespace", "future", "missing"} {
		t.Run(scenario, func(t *testing.T) {
			invalid := append([]WriteFenceAuthorityEvidence(nil), authorities...)
			switch scenario {
			case "duplicate":
				invalid[1].Authority = invalid[0].Authority
			case "whitespace":
				invalid[1].Authority = " " + invalid[1].Authority
			case "future":
				invalid[1].CompletedAt = receipt.CompletedAt.Add(time.Second)
			case "missing":
				invalid = invalid[:1]
			}
			if _, err := NewCompositeWriteFenceReceipt(request, invalid, receipt.CompletedAt); err == nil {
				t.Fatal("invalid authority evidence accepted")
			}
		})
	}
}
