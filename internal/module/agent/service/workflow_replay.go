package service

import (
	"context"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"twitter-clone/internal/module/agent/repository"
	"twitter-clone/internal/module/agent/workflow/engine"
)

const maxWorkflowReplayEvents = 10_000

type WorkflowReplay struct {
	Run           *repository.WorkflowRunRecord
	Revision      *repository.WorkflowRevision
	Events        []WorkflowReplayStateEvent
	Snapshot      *WorkflowReplaySnapshot
	Compensations []WorkflowReplayCompensation
	Integrity     WorkflowReplayIntegrity
}

type WorkflowReplayStateEvent struct {
	Sequence  int64
	NodeID    string
	DeltaJSON string
	EventHash string
	AppliedAt int64
}

type WorkflowReplaySnapshot struct {
	StateVersion int64
	SnapshotHash string
	CreatedAt    int64
}

type WorkflowReplayCompensation struct {
	Sequence          int
	SourceNodeID      string
	StepID            string
	ToolName          string
	InputHash         string
	PlanHash          string
	Status            string
	Attempt           int
	ErrorMessage      string
	ApprovalRequestID string
	LeaseUntil        int64
	CreatedAt         int64
	UpdatedAt         int64
	FinishedAt        int64
}

type WorkflowReplayIntegrity struct {
	Verified        bool
	StateVersion    int64
	EventCount      int64
	LastSequence    int64
	SnapshotVersion int64
}

// GetWorkflowRunReplay returns verified, read-only persistence evidence. It
// never invokes the scheduler, an LLM, or a tool handler.
func (s *AgentService) GetWorkflowRunReplay(ctx context.Context, userID uint64, runID string) (*WorkflowReplay, error) {
	if s.repo == nil {
		return nil, errors.New("agent repository is not initialized")
	}
	runOID, err := primitive.ObjectIDFromHex(runID)
	if err != nil {
		return nil, fmt.Errorf("invalid run_id: %w", err)
	}
	run, err := s.repo.GetWorkflowRun(ctx, runOID, userID)
	if err != nil {
		return nil, err
	}
	if run.StateVersion < 0 {
		return nil, errors.New("workflow run has negative state version")
	}

	eventRepo, ok := s.repo.(repository.WorkflowStateEventRepository)
	if !ok {
		return nil, errors.New("workflow state event repository is not available")
	}
	var records []*repository.WorkflowStateEvent
	if replayRepo, supported := s.repo.(repository.WorkflowStateReplayRepository); supported {
		records, err = replayRepo.ListWorkflowStateEventsForReplay(
			ctx, run.ID, run.UserID, run.StateVersion, maxWorkflowReplayEvents+1,
		)
	} else {
		records, err = eventRepo.ListWorkflowStateEvents(ctx, run.ID, run.UserID, 0)
	}
	if err != nil {
		return nil, err
	}
	if len(records) > maxWorkflowReplayEvents {
		return nil, fmt.Errorf("workflow replay exceeds maximum event count %d", maxWorkflowReplayEvents)
	}

	replay := &WorkflowReplay{Run: run}
	decodedEvents := make([]engine.StateEvent, 0, len(records))
	expectedSequence := int64(1)
	for _, record := range records {
		if record == nil {
			return nil, errors.New("workflow state event stream contains a nil record")
		}
		// The run cursor is the public consistency boundary. A concurrently
		// persisted future event is not part of this replay view yet.
		if record.Sequence > run.StateVersion {
			break
		}
		if record.Sequence != expectedSequence {
			return nil, fmt.Errorf("workflow state event stream is not contiguous: expected=%d actual=%d", expectedSequence, record.Sequence)
		}
		decoded, err := decodeWorkflowStateEvent(run, record)
		if err != nil {
			return nil, err
		}
		decodedEvents = append(decodedEvents, decoded)
		replay.Events = append(replay.Events, WorkflowReplayStateEvent{
			Sequence: record.Sequence, NodeID: record.NodeID, DeltaJSON: record.DeltaJSON,
			EventHash: record.EventHash, AppliedAt: record.AppliedAt.UnixMilli(),
		})
		expectedSequence++
	}
	lastSequence := expectedSequence - 1
	if lastSequence != run.StateVersion {
		return nil, fmt.Errorf("workflow state event stream incomplete: expected=%d actual=%d", run.StateVersion, lastSequence)
	}

	blackboard := engine.NewBlackboard()
	snapshotVersion := int64(0)
	if snapshotRepo, supported := s.repo.(repository.WorkflowStateSnapshotRepository); supported {
		snapshot, err := snapshotRepo.GetLatestWorkflowStateSnapshot(ctx, run.ID, run.UserID, run.StateVersion)
		if err != nil {
			return nil, err
		}
		if snapshot != nil {
			state, err := decodeWorkflowStateSnapshot(snapshot)
			if err != nil {
				return nil, err
			}
			blackboard.LoadSnapshotAtVersion(state, uint64(snapshot.StateVersion))
			snapshotVersion = snapshot.StateVersion
			replay.Snapshot = &WorkflowReplaySnapshot{
				StateVersion: snapshot.StateVersion, SnapshotHash: snapshot.SnapshotHash,
				CreatedAt: snapshot.CreatedAt.Unix(),
			}
		}
	}
	remaining := make([]engine.StateEvent, 0, len(decodedEvents))
	for _, event := range decodedEvents {
		if event.Sequence > uint64(snapshotVersion) {
			remaining = append(remaining, event)
		}
	}
	if err := blackboard.Replay(remaining); err != nil {
		return nil, err
	}
	if blackboard.Version() != uint64(run.StateVersion) {
		return nil, fmt.Errorf("workflow replay state version mismatch: expected=%d actual=%d", run.StateVersion, blackboard.Version())
	}

	if revisionRepo, supported := s.repo.(repository.WorkflowRevisionRepository); supported && !run.WorkflowRevisionID.IsZero() {
		replay.Revision, err = revisionRepo.GetWorkflowRevision(ctx, run.WorkflowID, run.WorkflowRevisionID, run.UserID)
		if err != nil {
			return nil, err
		}
	}
	if compensationRepo, supported := s.repo.(repository.WorkflowCompensationRepository); supported {
		records, err := compensationRepo.ListWorkflowCompensations(ctx, run.ID, run.UserID)
		if err != nil {
			return nil, err
		}
		for _, record := range records {
			if record == nil {
				continue
			}
			replay.Compensations = append(replay.Compensations, workflowReplayCompensation(record))
		}
	}
	replay.Integrity = WorkflowReplayIntegrity{
		Verified: true, StateVersion: run.StateVersion, EventCount: int64(len(replay.Events)),
		LastSequence: lastSequence, SnapshotVersion: snapshotVersion,
	}
	return replay, nil
}

func workflowReplayCompensation(record *repository.WorkflowCompensationRecord) WorkflowReplayCompensation {
	approvalID := ""
	if !record.ApprovalRequestID.IsZero() {
		approvalID = record.ApprovalRequestID.Hex()
	}
	return WorkflowReplayCompensation{
		Sequence: record.Sequence, SourceNodeID: record.SourceNodeID, StepID: record.StepID,
		ToolName: record.ToolName, InputHash: record.InputHash, PlanHash: record.PlanHash,
		Status: record.Status, Attempt: record.Attempt, ErrorMessage: record.ErrorMessage,
		ApprovalRequestID: approvalID, LeaseUntil: unixTime(record.LeaseUntil),
		CreatedAt: unixTime(record.CreatedAt), UpdatedAt: unixTime(record.UpdatedAt),
		FinishedAt: unixTime(record.FinishedAt),
	}
}

func unixTime(value interface {
	Unix() int64
	IsZero() bool
}) int64 {
	if value.IsZero() {
		return 0
	}
	return value.Unix()
}
