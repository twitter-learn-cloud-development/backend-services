package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"twitter-clone/internal/module/agent/repository"
	"twitter-clone/internal/module/agent/workflow/engine"
)

func (s *AgentService) resolveCurrentWorkflowRevision(ctx context.Context, workflow *repository.WorkflowDefinition) (*repository.WorkflowRevision, error) {
	if workflow == nil {
		return nil, errors.New("workflow is required")
	}
	if revisionRepo, ok := s.repo.(repository.WorkflowRevisionRepository); ok {
		revision, err := revisionRepo.ResolveCurrentWorkflowRevision(ctx, workflow.ID, workflow.UserID)
		if err != nil {
			return nil, err
		}
		return validateWorkflowRevisionIntegrity(revision)
	}

	// Compatibility path for test doubles and deployments being rolled forward.
	// MongoAgentRepository always implements the immutable revision capability.
	return validateWorkflowRevisionIntegrity(&repository.WorkflowRevision{
		WorkflowID:     workflow.ID,
		UserID:         workflow.UserID,
		RevisionNumber: 1,
		DSLJSON:        workflow.DSLJSON,
		DSLHash:        workflow.CurrentDSLHash,
		CreatedAt:      workflow.UpdatedAt,
	})
}

func (s *AgentService) resolveWorkflowRevisionForRun(ctx context.Context, workflow *repository.WorkflowDefinition, run *repository.WorkflowRunRecord) (*repository.WorkflowRevision, error) {
	if run == nil {
		return nil, errors.New("workflow run is required")
	}
	if !run.WorkflowRevisionID.IsZero() {
		revisionRepo, ok := s.repo.(repository.WorkflowRevisionRepository)
		if !ok {
			return nil, errors.New("workflow revision repository is not available")
		}
		revision, err := revisionRepo.GetWorkflowRevision(ctx, run.WorkflowID, run.WorkflowRevisionID, run.UserID)
		if err != nil {
			return nil, err
		}
		return validateWorkflowRevisionIntegrity(revision)
	}
	return s.resolveCurrentWorkflowRevision(ctx, workflow)
}

func (s *AgentService) resolveRequestedWorkflowRevision(ctx context.Context, workflow *repository.WorkflowDefinition, revisionID string) (*repository.WorkflowRevision, error) {
	if revisionID == "" {
		return s.resolveCurrentWorkflowRevision(ctx, workflow)
	}
	revisionOID, err := primitive.ObjectIDFromHex(revisionID)
	if err != nil {
		return nil, fmt.Errorf("invalid workflow_revision_id: %w", err)
	}
	revisionRepo, err := s.workflowRevisionRepository()
	if err != nil {
		return nil, err
	}
	revision, err := revisionRepo.GetWorkflowRevision(ctx, workflow.ID, revisionOID, workflow.UserID)
	if err != nil {
		return nil, err
	}
	return validateWorkflowRevisionIntegrity(revision)
}

func (s *AgentService) workflowRevisionRepository() (repository.WorkflowRevisionRepository, error) {
	revisionRepo, ok := s.repo.(repository.WorkflowRevisionRepository)
	if !ok {
		return nil, errors.New("workflow revision repository is not available")
	}
	return revisionRepo, nil
}

func validateWorkflowRevisionIntegrity(revision *repository.WorkflowRevision) (*repository.WorkflowRevision, error) {
	if revision == nil {
		return nil, errors.New("workflow revision is required")
	}
	if revision.DSLHash == "" {
		return revision, nil
	}
	hash := sha256.Sum256([]byte(revision.DSLJSON))
	if hex.EncodeToString(hash[:]) != revision.DSLHash {
		return nil, fmt.Errorf("workflow revision %s failed DSL integrity validation", revision.ID.Hex())
	}
	return revision, nil
}

func (s *AgentService) workflowStateCommitOption(run *repository.WorkflowRunRecord) engine.SchedulerOption {
	if run == nil || s.workflowSnapshotInterval == 0 {
		return nil
	}
	if _, ok := s.repo.(repository.WorkflowStateEventRepository); !ok {
		return nil
	}
	if _, ok := s.repo.(repository.WorkflowStateSnapshotRepository); !ok {
		return nil
	}

	return engine.WithStateCommitHook(func(ctx context.Context, commit engine.StateCommit) error {
		if run.StateVersion < 0 || commit.StateVersion <= uint64(run.StateVersion) {
			return nil
		}
		if commit.StateVersion-uint64(run.StateVersion) < s.workflowSnapshotInterval {
			return nil
		}
		previousStateVersion := run.StateVersion
		if err := s.persistWorkflowStateCommit(ctx, run, commit, true); err != nil {
			return err
		}
		if err := s.advanceWorkflowRunStateVersion(ctx, run); err != nil {
			run.StateVersion = previousStateVersion
			return fmt.Errorf("advance workflow run snapshot cursor: %w", err)
		}
		return nil
	})
}

func (s *AgentService) advanceWorkflowRunStateVersion(ctx context.Context, run *repository.WorkflowRunRecord) error {
	if cursorRepo, ok := s.repo.(repository.WorkflowRunStateCursorRepository); ok {
		return cursorRepo.AdvanceWorkflowRunStateVersion(ctx, run.ID, run.UserID, run.StateVersion)
	}
	// Compatibility path for legacy test doubles. MongoAgentRepository always
	// implements the isolated cursor update capability.
	return s.repo.UpdateWorkflowRun(ctx, run)
}

func (s *AgentService) persistWorkflowState(ctx context.Context, run *repository.WorkflowRunRecord, blackboard *engine.Blackboard, forceSnapshot bool) error {
	if run == nil || blackboard == nil {
		return nil
	}
	return s.persistWorkflowStateCommit(ctx, run, blackboard.Commit(), forceSnapshot)
}

func (s *AgentService) persistWorkflowStateCommit(ctx context.Context, run *repository.WorkflowRunRecord, commit engine.StateCommit, saveSnapshot bool) error {
	if run == nil {
		return nil
	}
	if commit.StateVersion > math.MaxInt64 {
		return errors.New("workflow state version exceeds persistence range")
	}
	if run.StateVersion < 0 || uint64(run.StateVersion) > commit.StateVersion {
		return fmt.Errorf("workflow state version regressed: persisted=%d current=%d", run.StateVersion, commit.StateVersion)
	}

	events := make([]engine.StateEvent, 0, len(commit.Events))
	for _, event := range commit.Events {
		if event.Sequence > uint64(run.StateVersion) {
			events = append(events, event)
		}
	}
	if eventRepo, ok := s.repo.(repository.WorkflowStateEventRepository); ok && len(events) > 0 {
		records := make([]*repository.WorkflowStateEvent, 0, len(events))
		for _, event := range events {
			record, err := workflowStateEventRecord(run, event)
			if err != nil {
				return err
			}
			records = append(records, record)
		}
		if err := eventRepo.AppendWorkflowStateEvents(ctx, records); err != nil {
			return err
		}
	}
	if saveSnapshot && commit.StateVersion > 0 {
		if snapshotRepo, ok := s.repo.(repository.WorkflowStateSnapshotRepository); ok {
			record, err := workflowStateSnapshotRecord(run, commit)
			if err != nil {
				return err
			}
			if err := snapshotRepo.SaveWorkflowStateSnapshot(ctx, record); err != nil {
				return err
			}
		}
	}

	run.StateVersion = int64(commit.StateVersion)
	return nil
}

func workflowStateEventRecord(run *repository.WorkflowRunRecord, event engine.StateEvent) (*repository.WorkflowStateEvent, error) {
	if event.Sequence > math.MaxInt64 {
		return nil, errors.New("workflow state event sequence exceeds persistence range")
	}
	deltaJSON, err := json.Marshal(event.Delta)
	if err != nil {
		return nil, fmt.Errorf("marshal workflow state event delta: %w", err)
	}
	eventHash, err := workflowStateEventHash(event.Sequence, event.NodeID, deltaJSON, event.AppliedAt)
	if err != nil {
		return nil, err
	}
	return &repository.WorkflowStateEvent{
		ID:        primitive.NewObjectID(),
		RunID:     run.ID,
		UserID:    run.UserID,
		Sequence:  int64(event.Sequence),
		NodeID:    event.NodeID,
		DeltaJSON: string(deltaJSON),
		EventHash: eventHash,
		AppliedAt: time.UnixMilli(event.AppliedAt),
	}, nil
}

func workflowStateEventHash(sequence uint64, nodeID string, deltaJSON []byte, appliedAt int64) (string, error) {
	hashPayload, err := json.Marshal(struct {
		Sequence  uint64          `json:"sequence"`
		NodeID    string          `json:"node_id"`
		Delta     json.RawMessage `json:"delta"`
		AppliedAt int64           `json:"applied_at"`
	}{
		Sequence: sequence, NodeID: nodeID,
		Delta: deltaJSON, AppliedAt: appliedAt,
	})
	if err != nil {
		return "", fmt.Errorf("marshal workflow state event hash payload: %w", err)
	}
	hash := sha256.Sum256(hashPayload)
	return hex.EncodeToString(hash[:]), nil
}

func workflowStateSnapshotRecord(run *repository.WorkflowRunRecord, commit engine.StateCommit) (*repository.WorkflowStateSnapshot, error) {
	if commit.StateVersion > math.MaxInt64 {
		return nil, errors.New("workflow state snapshot version exceeds persistence range")
	}
	snapshotJSON, snapshotHash, err := workflowStateDigest(commit.Snapshot)
	if err != nil {
		return nil, err
	}
	return &repository.WorkflowStateSnapshot{
		ID: primitive.NewObjectID(), RunID: run.ID, UserID: run.UserID,
		StateVersion: int64(commit.StateVersion), SnapshotJSON: snapshotJSON,
		SnapshotHash: snapshotHash, CreatedAt: time.Now(),
	}, nil
}

func workflowStateDigest(snapshot map[string]map[string]interface{}) (string, string, error) {
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return "", "", fmt.Errorf("marshal workflow state snapshot: %w", err)
	}
	hash := sha256.Sum256(payload)
	return string(payload), hex.EncodeToString(hash[:]), nil
}

func (s *AgentService) rehydrateWorkflowCheckpoint(ctx context.Context, run *repository.WorkflowRunRecord, checkpoint engine.WorkflowCheckpoint) (engine.WorkflowCheckpoint, error) {
	if run == nil || run.StateVersion <= 0 {
		return checkpoint, nil
	}
	eventRepo, ok := s.repo.(repository.WorkflowStateEventRepository)
	if !ok {
		return checkpoint, nil
	}

	blackboard := engine.NewBlackboard()
	afterSequence := int64(0)
	if snapshotRepo, supported := s.repo.(repository.WorkflowStateSnapshotRepository); supported {
		snapshot, err := snapshotRepo.GetLatestWorkflowStateSnapshot(ctx, run.ID, run.UserID, run.StateVersion)
		if err != nil {
			return checkpoint, err
		}
		if snapshot != nil {
			state, err := decodeWorkflowStateSnapshot(snapshot)
			if err != nil {
				return checkpoint, err
			}
			blackboard.LoadSnapshotAtVersion(state, uint64(snapshot.StateVersion))
			afterSequence = snapshot.StateVersion
		}
	}

	records, err := eventRepo.ListWorkflowStateEvents(ctx, run.ID, run.UserID, afterSequence)
	if err != nil {
		return checkpoint, err
	}
	events := make([]engine.StateEvent, 0, len(records))
	for _, record := range records {
		if record == nil || record.Sequence > run.StateVersion {
			continue
		}
		event, err := decodeWorkflowStateEvent(run, record)
		if err != nil {
			return checkpoint, err
		}
		events = append(events, event)
	}
	if err := blackboard.Replay(events); err != nil {
		return checkpoint, err
	}
	if blackboard.Version() != uint64(run.StateVersion) {
		return checkpoint, fmt.Errorf("workflow state event stream incomplete: expected=%d actual=%d", run.StateVersion, blackboard.Version())
	}
	if checkpoint.StateVersion != uint64(run.StateVersion) {
		return checkpoint, fmt.Errorf("workflow checkpoint version mismatch: checkpoint=%d persisted=%d", checkpoint.StateVersion, run.StateVersion)
	}

	rehydrated := blackboard.GetSnapshot()
	_, rehydratedHash, err := workflowStateDigest(rehydrated)
	if err != nil {
		return checkpoint, err
	}
	_, checkpointHash, err := workflowStateDigest(checkpoint.Blackboard)
	if err != nil {
		return checkpoint, err
	}
	if checkpointHash != rehydratedHash {
		return checkpoint, errors.New("workflow checkpoint state failed persisted event replay validation")
	}
	checkpoint.Blackboard = rehydrated
	return checkpoint, nil
}

func decodeWorkflowStateSnapshot(snapshot *repository.WorkflowStateSnapshot) (map[string]map[string]interface{}, error) {
	if snapshot.StateVersion < 0 {
		return nil, errors.New("workflow state snapshot has negative version")
	}
	hash := sha256.Sum256([]byte(snapshot.SnapshotJSON))
	if hex.EncodeToString(hash[:]) != snapshot.SnapshotHash {
		return nil, fmt.Errorf("workflow state snapshot %d failed integrity validation", snapshot.StateVersion)
	}
	state := make(map[string]map[string]interface{})
	if err := json.Unmarshal([]byte(snapshot.SnapshotJSON), &state); err != nil {
		return nil, fmt.Errorf("decode workflow state snapshot: %w", err)
	}
	return state, nil
}

func decodeWorkflowStateEvent(run *repository.WorkflowRunRecord, record *repository.WorkflowStateEvent) (engine.StateEvent, error) {
	if record.RunID != run.ID || record.UserID != run.UserID || record.Sequence <= 0 {
		return engine.StateEvent{}, errors.New("workflow state event ownership or sequence is invalid")
	}
	if record.Sequence > math.MaxInt64 {
		return engine.StateEvent{}, errors.New("workflow state event sequence exceeds replay range")
	}
	delta := make(map[string]interface{})
	if err := json.Unmarshal([]byte(record.DeltaJSON), &delta); err != nil {
		return engine.StateEvent{}, fmt.Errorf("decode workflow state event %d: %w", record.Sequence, err)
	}
	hash, err := workflowStateEventHash(uint64(record.Sequence), record.NodeID, []byte(record.DeltaJSON), record.AppliedAt.UnixMilli())
	if err != nil {
		return engine.StateEvent{}, err
	}
	if hash != record.EventHash {
		return engine.StateEvent{}, fmt.Errorf("workflow state event %d failed integrity validation", record.Sequence)
	}
	return engine.StateEvent{
		Sequence: uint64(record.Sequence), NodeID: record.NodeID,
		Delta: delta, AppliedAt: record.AppliedAt.UnixMilli(),
	}, nil
}
