package deletion

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	AuditEventSchemaV1 = "agent-data-deletion-audit/v1"
	MaxAuditEvents     = 128
)

type AuditEventType string

const (
	AuditStageClaimed   AuditEventType = "stage_claimed"
	AuditStageSucceeded AuditEventType = "stage_succeeded"
	AuditStageFailed    AuditEventType = "stage_failed"
)

type AuditEvent struct {
	SchemaVersion      string         `bson:"schema_version" json:"schema_version"`
	EventID            string         `bson:"event_id" json:"event_id"`
	RequestID          string         `bson:"request_id" json:"request_id"`
	Sequence           int64          `bson:"sequence" json:"sequence"`
	Type               AuditEventType `bson:"type" json:"type"`
	StageID            string         `bson:"stage_id" json:"stage_id"`
	Attempt            int64          `bson:"attempt" json:"attempt"`
	RequestRevision    int64          `bson:"request_revision" json:"request_revision"`
	CheckpointRevision int64          `bson:"checkpoint_revision" json:"checkpoint_revision"`
	PlanDigest         string         `bson:"plan_digest" json:"plan_digest"`
	CapabilityDigest   string         `bson:"capability_digest" json:"capability_digest"`
	LeaseDigest        string         `bson:"lease_digest" json:"lease_digest"`
	ReceiptDigest      string         `bson:"receipt_digest,omitempty" json:"receipt_digest,omitempty"`
	FailureCode        string         `bson:"failure_code,omitempty" json:"failure_code,omitempty"`
	PreviousDigest     string         `bson:"previous_digest,omitempty" json:"previous_digest,omitempty"`
	EventDigest        string         `bson:"event_digest" json:"event_digest"`
	OccurredAt         time.Time      `bson:"occurred_at" json:"occurred_at"`
}

type AuditEventInput struct {
	RequestID          string
	Sequence           int64
	Type               AuditEventType
	Stage              StageCheckpoint
	RequestRevision    int64
	CheckpointRevision int64
	PlanDigest         string
	CapabilityDigest   string
	LeaseToken         string
	PreviousDigest     string
	OccurredAt         time.Time
}

func NewAuditEvent(input AuditEventInput) (AuditEvent, error) {
	if strings.TrimSpace(input.LeaseToken) == "" {
		return AuditEvent{}, errors.New("data deletion audit lease token is required")
	}
	event := AuditEvent{
		SchemaVersion: AuditEventSchemaV1, RequestID: strings.TrimSpace(input.RequestID),
		Sequence: input.Sequence, Type: input.Type, StageID: input.Stage.StageID,
		Attempt: input.Stage.Attempts, RequestRevision: input.RequestRevision,
		CheckpointRevision: input.CheckpointRevision, PlanDigest: input.PlanDigest,
		CapabilityDigest: input.CapabilityDigest, LeaseDigest: LeaseTokenDigest(input.LeaseToken),
		ReceiptDigest: input.Stage.ReceiptDigest, FailureCode: input.Stage.FailureCode,
		PreviousDigest: input.PreviousDigest, OccurredAt: input.OccurredAt.UTC(),
	}
	event.EventDigest = auditEventDigest(event)
	event.EventID = event.EventDigest
	if err := ValidateAuditEvent(event); err != nil {
		return AuditEvent{}, err
	}
	return event, nil
}

func LeaseTokenDigest(token string) string {
	return sha256Hex(strings.TrimSpace(token))
}

func ValidateAuditEvent(event AuditEvent) error {
	if event.SchemaVersion != AuditEventSchemaV1 || event.EventID == "" || event.EventID != event.EventDigest ||
		!isSHA256(event.EventDigest) || !isBoundedIdentity(event.RequestID, 64) || event.Sequence < 1 ||
		event.Sequence > MaxAuditEvents || event.Attempt < 1 || event.RequestRevision < 1 ||
		event.CheckpointRevision < 2 || !isSHA256(event.PlanDigest) || !isSHA256(event.CapabilityDigest) ||
		!isSHA256(event.LeaseDigest) || event.OccurredAt.IsZero() {
		return errors.New("data deletion audit event identity is invalid")
	}
	if _, ok := knownStageIDs()[event.StageID]; !ok {
		return fmt.Errorf("unknown data deletion audit stage %q", event.StageID)
	}
	if event.Sequence == 1 {
		if event.PreviousDigest != "" {
			return errors.New("first data deletion audit event cannot have a previous digest")
		}
	} else if !isSHA256(event.PreviousDigest) {
		return errors.New("data deletion audit event previous digest is invalid")
	}
	switch event.Type {
	case AuditStageClaimed:
		if event.ReceiptDigest != "" || event.FailureCode != "" {
			return errors.New("claimed data deletion audit event cannot contain completion evidence")
		}
	case AuditStageSucceeded:
		if !isSHA256(event.ReceiptDigest) || event.FailureCode != "" {
			return errors.New("succeeded data deletion audit event requires a receipt digest")
		}
	case AuditStageFailed:
		if event.ReceiptDigest != "" || !isFailureCode(event.FailureCode) {
			return errors.New("failed data deletion audit event requires a failure code")
		}
	default:
		return errors.New("data deletion audit event type is invalid")
	}
	if event.EventDigest != auditEventDigest(event) {
		return errors.New("data deletion audit event digest mismatch")
	}
	return nil
}

func ValidateAuditTrail(events []AuditEvent, count int64, tailDigest, planDigest, capabilityDigest string) error {
	if count < 0 || count > MaxAuditEvents || int64(len(events)) != count {
		return errors.New("data deletion audit event count is invalid")
	}
	if count == 0 {
		if tailDigest != "" {
			return errors.New("empty data deletion audit trail has a tail digest")
		}
		return nil
	}
	if !isSHA256(tailDigest) {
		return errors.New("data deletion audit tail digest is invalid")
	}
	var active *AuditEvent
	for index := range events {
		event := events[index]
		if err := ValidateAuditEvent(event); err != nil {
			return fmt.Errorf("validate data deletion audit event %d: %w", index+1, err)
		}
		if event.Sequence != int64(index+1) || event.PlanDigest != planDigest || event.CapabilityDigest != capabilityDigest {
			return errors.New("data deletion audit event is not bound to its protocol")
		}
		if index == 0 {
			if event.PreviousDigest != "" {
				return errors.New("data deletion audit chain does not start at its root")
			}
		} else {
			previous := events[index-1]
			if event.PreviousDigest != previous.EventDigest || event.RequestID != previous.RequestID ||
				event.RequestRevision != previous.RequestRevision+1 ||
				event.CheckpointRevision != previous.CheckpointRevision+1 || event.OccurredAt.Before(previous.OccurredAt) {
				return errors.New("data deletion audit chain continuity is invalid")
			}
		}
		switch event.Type {
		case AuditStageClaimed:
			if active != nil {
				return errors.New("data deletion audit trail contains concurrent stage claims")
			}
			copy := event
			active = &copy
		case AuditStageSucceeded, AuditStageFailed:
			if active == nil || active.StageID != event.StageID || active.Attempt != event.Attempt || active.LeaseDigest != event.LeaseDigest {
				return errors.New("data deletion audit completion does not match its claim")
			}
			active = nil
		}
	}
	if events[len(events)-1].EventDigest != tailDigest {
		return errors.New("data deletion audit tail does not match the event chain")
	}
	return nil
}

func ValidateAuditProtocolBinding(events []AuditEvent, requestRevision int64, protocol Protocol) error {
	if requestRevision < 1 {
		return errors.New("data deletion audit request revision is invalid")
	}
	if err := ValidateProtocol(protocol); err != nil {
		return err
	}
	if len(events) == 0 {
		if protocol.Checkpoint.Revision != 1 {
			return errors.New("empty data deletion audit trail cannot describe an advanced checkpoint")
		}
		return nil
	}
	tail := events[len(events)-1]
	if tail.RequestRevision != requestRevision || tail.CheckpointRevision != protocol.Checkpoint.Revision {
		return errors.New("data deletion audit tail revision does not match its checkpoint")
	}
	stage, ok := auditCheckpointStage(protocol.Checkpoint, tail.StageID)
	if !ok || stage.Attempts != tail.Attempt {
		return errors.New("data deletion audit tail stage does not match its checkpoint")
	}
	switch tail.Type {
	case AuditStageClaimed:
		if protocol.Checkpoint.CurrentStageID != stage.StageID || stage.Status != StageRunning ||
			stage.ReceiptDigest != "" || stage.FailureCode != "" || LeaseTokenDigest(stage.LeaseToken) != tail.LeaseDigest {
			return errors.New("data deletion audit claim does not match running checkpoint state")
		}
	case AuditStageSucceeded:
		if protocol.Checkpoint.CurrentStageID != "" || stage.Status != StageSucceeded || stage.ReceiptDigest != tail.ReceiptDigest {
			return errors.New("data deletion audit success does not match checkpoint state")
		}
	case AuditStageFailed:
		if protocol.Checkpoint.CurrentStageID != "" || stage.Status != StageFailed || stage.FailureCode != tail.FailureCode {
			return errors.New("data deletion audit failure does not match checkpoint state")
		}
	}
	return nil
}

func auditEventDigest(event AuditEvent) string {
	return sha256Hex(strings.Join([]string{
		event.SchemaVersion, event.RequestID, strconv.FormatInt(event.Sequence, 10), string(event.Type),
		event.StageID, strconv.FormatInt(event.Attempt, 10), strconv.FormatInt(event.RequestRevision, 10),
		strconv.FormatInt(event.CheckpointRevision, 10), event.PlanDigest, event.CapabilityDigest,
		event.LeaseDigest, event.ReceiptDigest, event.FailureCode, event.PreviousDigest,
		event.OccurredAt.UTC().Format(time.RFC3339Nano),
	}, "\x00"))
}

func knownStageIDs() map[string]struct{} {
	result := make(map[string]struct{}, len(defaultStages()))
	for _, stage := range defaultStages() {
		result[stage.ID] = struct{}{}
	}
	return result
}

func auditCheckpointStage(checkpoint Checkpoint, stageID string) (StageCheckpoint, bool) {
	for _, stage := range checkpoint.Stages {
		if stage.StageID == stageID {
			return stage, true
		}
	}
	return StageCheckpoint{}, false
}
