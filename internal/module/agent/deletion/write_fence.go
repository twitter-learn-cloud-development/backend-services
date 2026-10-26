package deletion

import (
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	WriteFenceReceiptSchemaV1 = "agent-data-deletion-write-fence-receipt/v1"
	WriteFenceReceiptSchemaV2 = "agent-data-deletion-write-fence-receipt/v2"
)

type WriteFenceAuthorityEvidence struct {
	Authority     string
	ReceiptDigest string
	FenceRevision int64
	CompletedAt   time.Time
}

type WriteFenceRequest struct {
	RequestID        string
	UserID           uint64
	SubjectDigest    string
	PlanDigest       string
	CapabilityDigest string
	Governance       GovernanceDecision
	Deadline         time.Time
}

type WriteFenceReceipt struct {
	SchemaVersion    string    `json:"schema_version"`
	ReceiptDigest    string    `json:"receipt_digest"`
	RequestID        string    `json:"request_id"`
	SubjectDigest    string    `json:"subject_digest"`
	PlanDigest       string    `json:"plan_digest"`
	CapabilityDigest string    `json:"capability_digest"`
	GovernanceDigest string    `json:"governance_digest"`
	FenceRevision    int64     `json:"fence_revision"`
	AuthorityDigest  string    `json:"authority_digest,omitempty"`
	AuthorityCount   int       `json:"authority_count,omitempty"`
	CompletedAt      time.Time `json:"completed_at"`
}

func ValidateWriteFenceRequest(request WriteFenceRequest, now time.Time) error {
	if !isBoundedIdentity(strings.TrimSpace(request.RequestID), 64) || request.UserID == 0 ||
		request.SubjectDigest != DeletionSubjectDigest(request.UserID) || !isSHA256(request.PlanDigest) ||
		!isSHA256(request.CapabilityDigest) || now.IsZero() || !request.Deadline.After(now) ||
		request.Deadline.Sub(now) > maxStageLease {
		return errors.New("data deletion write fence request is invalid")
	}
	return AuthorizeGovernanceDecision(request.Governance, request.UserID, now)
}

func NewWriteFenceReceipt(
	request WriteFenceRequest,
	fenceRevision int64,
	completedAt time.Time,
) (WriteFenceReceipt, error) {
	if err := ValidateWriteFenceRequest(request, completedAt); err != nil {
		return WriteFenceReceipt{}, err
	}
	receipt := WriteFenceReceipt{
		SchemaVersion: WriteFenceReceiptSchemaV1,
		RequestID:     strings.TrimSpace(request.RequestID), SubjectDigest: request.SubjectDigest,
		PlanDigest: request.PlanDigest, CapabilityDigest: request.CapabilityDigest,
		GovernanceDigest: request.Governance.Digest, FenceRevision: fenceRevision,
		CompletedAt: completedAt.UTC(),
	}
	receipt.ReceiptDigest = writeFenceReceiptDigest(receipt)
	if err := ValidateWriteFenceReceipt(receipt); err != nil {
		return WriteFenceReceipt{}, err
	}
	return receipt, nil
}

func NewCompositeWriteFenceReceipt(
	request WriteFenceRequest,
	authorities []WriteFenceAuthorityEvidence,
	completedAt time.Time,
) (WriteFenceReceipt, error) {
	if err := ValidateWriteFenceRequest(request, completedAt); err != nil {
		return WriteFenceReceipt{}, err
	}
	authorityDigest, maxRevision, err := writeFenceAuthorityDigest(authorities)
	if err != nil {
		return WriteFenceReceipt{}, err
	}
	for _, authority := range authorities {
		if authority.CompletedAt.After(completedAt) {
			return WriteFenceReceipt{}, errors.New("write fence authority completed after composite receipt")
		}
	}
	receipt := WriteFenceReceipt{
		SchemaVersion: WriteFenceReceiptSchemaV2,
		RequestID:     strings.TrimSpace(request.RequestID), SubjectDigest: request.SubjectDigest,
		PlanDigest: request.PlanDigest, CapabilityDigest: request.CapabilityDigest,
		GovernanceDigest: request.Governance.Digest, FenceRevision: maxRevision,
		AuthorityDigest: authorityDigest, AuthorityCount: len(authorities),
		CompletedAt: completedAt.UTC(),
	}
	receipt.ReceiptDigest = writeFenceReceiptDigest(receipt)
	if err := ValidateWriteFenceReceipt(receipt); err != nil {
		return WriteFenceReceipt{}, err
	}
	return receipt, nil
}

func ValidateWriteFenceReceipt(receipt WriteFenceReceipt) error {
	if receipt.SchemaVersion != WriteFenceReceiptSchemaV1 && receipt.SchemaVersion != WriteFenceReceiptSchemaV2 {
		return errors.New("data deletion write fence receipt is invalid")
	}
	if !isSHA256(receipt.ReceiptDigest) ||
		!isBoundedIdentity(receipt.RequestID, 64) || !isSHA256(receipt.SubjectDigest) ||
		!isSHA256(receipt.PlanDigest) || !isSHA256(receipt.CapabilityDigest) ||
		!isSHA256(receipt.GovernanceDigest) || receipt.FenceRevision < 1 || receipt.CompletedAt.IsZero() ||
		receipt.ReceiptDigest != writeFenceReceiptDigest(receipt) {
		return errors.New("data deletion write fence receipt is invalid")
	}
	if receipt.SchemaVersion == WriteFenceReceiptSchemaV1 {
		if receipt.AuthorityDigest != "" || receipt.AuthorityCount != 0 {
			return errors.New("data deletion write fence receipt is invalid")
		}
		return nil
	}
	if !isSHA256(receipt.AuthorityDigest) || receipt.AuthorityCount < 2 || receipt.AuthorityCount > 16 {
		return errors.New("data deletion write fence receipt is invalid")
	}
	return nil
}

func VerifyWriteFenceReceipt(request WriteFenceRequest, receipt WriteFenceReceipt, now time.Time) error {
	if err := ValidateWriteFenceReceipt(receipt); err != nil {
		return err
	}
	if now.IsZero() || now.Before(receipt.CompletedAt) ||
		receipt.RequestID != strings.TrimSpace(request.RequestID) ||
		receipt.SubjectDigest != request.SubjectDigest || receipt.PlanDigest != request.PlanDigest ||
		receipt.CapabilityDigest != request.CapabilityDigest ||
		receipt.GovernanceDigest != request.Governance.Digest {
		return ErrDeletionReceiptMismatch
	}
	return nil
}

func writeFenceReceiptDigest(receipt WriteFenceReceipt) string {
	parts := []string{
		receipt.SchemaVersion, receipt.RequestID, receipt.SubjectDigest, receipt.PlanDigest,
		receipt.CapabilityDigest, receipt.GovernanceDigest,
		strconv.FormatInt(receipt.FenceRevision, 10),
	}
	if receipt.SchemaVersion == WriteFenceReceiptSchemaV2 {
		parts = append(parts, receipt.AuthorityDigest, strconv.Itoa(receipt.AuthorityCount))
	}
	parts = append(parts, receipt.CompletedAt.UTC().Format(time.RFC3339Nano))
	return sha256Hex(strings.Join(parts, "\x00"))
}

func writeFenceAuthorityDigest(authorities []WriteFenceAuthorityEvidence) (string, int64, error) {
	if len(authorities) < 2 || len(authorities) > 16 {
		return "", 0, errors.New("data deletion write fence authority evidence is invalid")
	}
	ordered := append([]WriteFenceAuthorityEvidence(nil), authorities...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Authority < ordered[j].Authority })
	parts := []string{"agent-data-deletion-write-fence-authorities/v1"}
	var maxRevision int64
	for index, authority := range ordered {
		if authority.Authority != strings.TrimSpace(authority.Authority) ||
			!isBoundedIdentity(authority.Authority, 80) || !isSHA256(authority.ReceiptDigest) ||
			authority.FenceRevision < 1 || authority.CompletedAt.IsZero() ||
			(index > 0 && authority.Authority == strings.TrimSpace(ordered[index-1].Authority)) {
			return "", 0, errors.New("data deletion write fence authority evidence is invalid")
		}
		if authority.FenceRevision > maxRevision {
			maxRevision = authority.FenceRevision
		}
		parts = append(parts, authority.Authority, authority.ReceiptDigest,
			strconv.FormatInt(authority.FenceRevision, 10), authority.CompletedAt.UTC().Format(time.RFC3339Nano))
	}
	return sha256Hex(strings.Join(parts, "\x00")), maxRevision, nil
}
