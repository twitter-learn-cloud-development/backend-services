package deletion

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

const FinalizationReceiptSchemaV1 = "agent-data-deletion-finalization-receipt/v1"

// FinalizationReceipt binds the terminal control-plane transition to the
// immutable plan, capability snapshot and every preceding stage receipt.
// It deliberately carries only the subject digest, never the raw user ID.
type FinalizationReceipt struct {
	SchemaVersion       string    `json:"schema_version"`
	ReceiptDigest       string    `json:"receipt_digest"`
	RequestID           string    `json:"request_id"`
	SubjectDigest       string    `json:"subject_digest"`
	PlanDigest          string    `json:"plan_digest"`
	CapabilityDigest    string    `json:"capability_digest"`
	CheckpointRevision  int64     `json:"checkpoint_revision"`
	FinalizeAttempt     int64     `json:"finalize_attempt"`
	PriorReceiptsDigest string    `json:"prior_receipts_digest"`
	CompletedAt         time.Time `json:"completed_at"`
}

func BuildFinalizationReceipt(
	requestID string,
	userID uint64,
	protocol Protocol,
	capabilities CapabilitySnapshot,
	completedAt time.Time,
) (FinalizationReceipt, error) {
	if err := ValidateProtocol(protocol); err != nil {
		return FinalizationReceipt{}, err
	}
	if err := ValidatePlanCapabilities(protocol.Plan, capabilities); err != nil {
		return FinalizationReceipt{}, err
	}
	requestID = strings.TrimSpace(requestID)
	if !isBoundedIdentity(requestID, 64) || userID == 0 || completedAt.IsZero() {
		return FinalizationReceipt{}, errors.New("data deletion finalization identity is invalid")
	}
	if protocol.Checkpoint.CurrentStageID != StageFinalize || len(protocol.Checkpoint.Stages) != len(protocol.Plan.Stages) {
		return FinalizationReceipt{}, errors.New("data deletion finalization stage is not running")
	}
	parts := []string{FinalizationReceiptSchemaV1}
	var finalize StageCheckpoint
	for index, checkpoint := range protocol.Checkpoint.Stages {
		if index == len(protocol.Checkpoint.Stages)-1 {
			finalize = checkpoint
			if checkpoint.StageID != StageFinalize || checkpoint.Status != StageRunning || checkpoint.Attempts < 1 {
				return FinalizationReceipt{}, errors.New("data deletion finalization checkpoint is invalid")
			}
			continue
		}
		if checkpoint.Status != StageSucceeded || !isSHA256(checkpoint.ReceiptDigest) {
			return FinalizationReceipt{}, errors.New("data deletion finalization evidence is incomplete")
		}
		parts = append(parts, checkpoint.StageID+"|"+checkpoint.ReceiptDigest)
	}
	receipt := FinalizationReceipt{
		SchemaVersion: FinalizationReceiptSchemaV1,
		RequestID:     requestID, SubjectDigest: DeletionSubjectDigest(userID),
		PlanDigest: protocol.Plan.Digest, CapabilityDigest: capabilities.Digest,
		CheckpointRevision: protocol.Checkpoint.Revision, FinalizeAttempt: finalize.Attempts,
		PriorReceiptsDigest: sha256Hex(strings.Join(parts, "\x00")), CompletedAt: completedAt.UTC(),
	}
	receipt.ReceiptDigest = finalizationReceiptDigest(receipt)
	if err := ValidateFinalizationReceipt(receipt); err != nil {
		return FinalizationReceipt{}, err
	}
	return receipt, nil
}

func ValidateFinalizationReceipt(receipt FinalizationReceipt) error {
	if receipt.SchemaVersion != FinalizationReceiptSchemaV1 || !isSHA256(receipt.ReceiptDigest) ||
		!isBoundedIdentity(receipt.RequestID, 64) || !isSHA256(receipt.SubjectDigest) ||
		!isSHA256(receipt.PlanDigest) || !isSHA256(receipt.CapabilityDigest) ||
		receipt.CheckpointRevision < 1 || receipt.FinalizeAttempt < 1 ||
		!isSHA256(receipt.PriorReceiptsDigest) || receipt.CompletedAt.IsZero() ||
		receipt.ReceiptDigest != finalizationReceiptDigest(receipt) {
		return errors.New("data deletion finalization receipt is invalid")
	}
	return nil
}

func finalizationReceiptDigest(receipt FinalizationReceipt) string {
	return sha256Hex(strings.Join([]string{
		receipt.SchemaVersion, receipt.RequestID, receipt.SubjectDigest, receipt.PlanDigest,
		receipt.CapabilityDigest, strconv.FormatInt(receipt.CheckpointRevision, 10),
		strconv.FormatInt(receipt.FinalizeAttempt, 10), receipt.PriorReceiptsDigest,
		receipt.CompletedAt.UTC().Format(time.RFC3339Nano),
	}, "\x00"))
}
