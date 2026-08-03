package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	ErrWorkflowRevisionConflict   = errors.New("workflow revision conflict")
	ErrWorkflowStateEventConflict = errors.New("workflow state event conflict")
	ErrWorkflowSnapshotConflict   = errors.New("workflow state snapshot conflict")
)

// WorkflowRevision is an immutable workflow DSL snapshot.
type WorkflowRevision struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	WorkflowID     primitive.ObjectID `bson:"workflow_id" json:"workflow_id"`
	UserID         uint64             `bson:"user_id" json:"user_id"`
	RevisionNumber int64              `bson:"revision_number" json:"revision_number"`
	DSLJSON        string             `bson:"dsl_json" json:"dsl_json"`
	DSLHash        string             `bson:"dsl_hash" json:"dsl_hash"`
	CreatedAt      time.Time          `bson:"created_at" json:"created_at"`
}

// WorkflowStateEvent is one append-only blackboard transition.
type WorkflowStateEvent struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	RunID     primitive.ObjectID `bson:"run_id" json:"run_id"`
	UserID    uint64             `bson:"user_id" json:"user_id"`
	Sequence  int64              `bson:"sequence" json:"sequence"`
	NodeID    string             `bson:"node_id" json:"node_id"`
	DeltaJSON string             `bson:"delta_json" json:"delta_json"`
	EventHash string             `bson:"event_hash" json:"event_hash"`
	AppliedAt time.Time          `bson:"applied_at" json:"applied_at"`
}

// WorkflowStateSnapshot stores one immutable materialized Blackboard version.
type WorkflowStateSnapshot struct {
	ID           primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	RunID        primitive.ObjectID `bson:"run_id" json:"run_id"`
	UserID       uint64             `bson:"user_id" json:"user_id"`
	StateVersion int64              `bson:"state_version" json:"state_version"`
	SnapshotJSON string             `bson:"snapshot_json" json:"snapshot_json"`
	SnapshotHash string             `bson:"snapshot_hash" json:"snapshot_hash"`
	CreatedAt    time.Time          `bson:"created_at" json:"created_at"`
}

// WorkflowRevisionRepository is an additive capability so legacy test and
// migration repositories can continue implementing AgentRepository.
type WorkflowRevisionRepository interface {
	ResolveCurrentWorkflowRevision(ctx context.Context, workflowID primitive.ObjectID, userID uint64) (*WorkflowRevision, error)
	GetWorkflowRevision(ctx context.Context, workflowID, revisionID primitive.ObjectID, userID uint64) (*WorkflowRevision, error)
	ListWorkflowRevisions(ctx context.Context, workflowID primitive.ObjectID, userID uint64, page, pageSize int) ([]*WorkflowRevision, int64, error)
}

// WorkflowStateEventRepository persists and reads the append-only state log.
type WorkflowStateEventRepository interface {
	AppendWorkflowStateEvents(ctx context.Context, events []*WorkflowStateEvent) error
	ListWorkflowStateEvents(ctx context.Context, runID primitive.ObjectID, userID uint64, afterSequence int64) ([]*WorkflowStateEvent, error)
}

// WorkflowStateReplayRepository provides a bounded read path for public replay
// without changing the persistence/recovery contract above.
type WorkflowStateReplayRepository interface {
	ListWorkflowStateEventsForReplay(ctx context.Context, runID primitive.ObjectID, userID uint64, atOrBeforeSequence int64, limit int64) ([]*WorkflowStateEvent, error)
}

// WorkflowStateRangeRepository provides a bounded event slice for materializing
// a historical Blackboard version without loading the remainder of the run.
type WorkflowStateRangeRepository interface {
	ListWorkflowStateEventsRange(ctx context.Context, runID primitive.ObjectID, userID uint64, afterSequence, atOrBeforeSequence, limit int64) ([]*WorkflowStateEvent, error)
}

// WorkflowStateSnapshotRepository materializes the append-only event stream
// without changing event ownership or replay semantics.
type WorkflowStateSnapshotRepository interface {
	SaveWorkflowStateSnapshot(ctx context.Context, snapshot *WorkflowStateSnapshot) error
	GetLatestWorkflowStateSnapshot(ctx context.Context, runID primitive.ObjectID, userID uint64, atOrBeforeVersion int64) (*WorkflowStateSnapshot, error)
}

// WorkflowRunStateCursorRepository advances only the persisted Blackboard
// cursor. It intentionally cannot overwrite execution-control fields such as
// status or cancellation metadata with a stale in-memory run record.
type WorkflowRunStateCursorRepository interface {
	AdvanceWorkflowRunStateVersion(ctx context.Context, runID primitive.ObjectID, userID uint64, stateVersion int64) error
}

func newWorkflowRevision(workflow *WorkflowDefinition, revisionNumber int64, now time.Time) *WorkflowRevision {
	hash := sha256.Sum256([]byte(workflow.DSLJSON))
	return &WorkflowRevision{
		ID:             primitive.NewObjectID(),
		WorkflowID:     workflow.ID,
		UserID:         workflow.UserID,
		RevisionNumber: revisionNumber,
		DSLJSON:        workflow.DSLJSON,
		DSLHash:        hex.EncodeToString(hash[:]),
		CreatedAt:      now,
	}
}

func (r *MongoAgentRepository) createWorkflowWithRevision(ctx context.Context, workflow *WorkflowDefinition) error {
	now := time.Now()
	if workflow.ID.IsZero() {
		workflow.ID = primitive.NewObjectID()
	}
	if workflow.CreatedAt.IsZero() {
		workflow.CreatedAt = now
	}
	workflow.UpdatedAt = now

	revision := newWorkflowRevision(workflow, 1, now)
	workflow.CurrentRevisionID = revision.ID
	workflow.CurrentRevisionNumber = revision.RevisionNumber
	workflow.CurrentDSLHash = revision.DSLHash

	if _, err := r.workflowRevisionColl.InsertOne(ctx, revision); err != nil {
		return fmt.Errorf("insert initial workflow revision failed: %w", err)
	}
	if _, err := r.workflowColl.InsertOne(ctx, workflow); err != nil {
		committed, verifyErr := r.workflowRevisionWasCommitted(ctx, workflow.ID, workflow.UserID, revision, false)
		if verifyErr == nil && committed {
			return nil
		}
		if verifyErr == nil {
			r.cleanupWorkflowRevision(ctx, revision.ID)
		}
		return fmt.Errorf("insert workflow failed: %w", err)
	}
	return nil
}

func (r *MongoAgentRepository) updateWorkflowWithRevision(ctx context.Context, workflow *WorkflowDefinition) error {
	current, err := r.ResolveCurrentWorkflowRevision(ctx, workflow.ID, workflow.UserID)
	if err != nil {
		return err
	}

	now := time.Now()
	revision := newWorkflowRevision(workflow, current.RevisionNumber+1, now)
	if _, err := r.workflowRevisionColl.InsertOne(ctx, revision); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrWorkflowRevisionConflict
		}
		return fmt.Errorf("insert workflow revision failed: %w", err)
	}

	res, err := r.workflowColl.UpdateOne(ctx,
		bson.M{
			"_id": workflow.ID, "user_id": workflow.UserID,
			"current_revision_id":     current.ID,
			"current_revision_number": current.RevisionNumber,
		},
		bson.M{"$set": bson.M{
			"name": workflow.Name, "dsl_json": workflow.DSLJSON,
			"current_revision_id": revision.ID, "current_revision_number": revision.RevisionNumber,
			"current_dsl_hash": revision.DSLHash, "updated_at": now,
		}},
	)
	if err != nil {
		committed, verifyErr := r.workflowRevisionWasCommitted(ctx, workflow.ID, workflow.UserID, revision, true)
		if verifyErr == nil && committed {
			workflow.CurrentRevisionID = revision.ID
			workflow.CurrentRevisionNumber = revision.RevisionNumber
			workflow.CurrentDSLHash = revision.DSLHash
			workflow.UpdatedAt = now
			return nil
		}
		if verifyErr == nil {
			r.cleanupWorkflowRevision(ctx, revision.ID)
		}
		return fmt.Errorf("update workflow failed: %w", err)
	}
	if res.MatchedCount == 0 {
		r.cleanupWorkflowRevision(ctx, revision.ID)
		return ErrWorkflowRevisionConflict
	}

	workflow.CurrentRevisionID = revision.ID
	workflow.CurrentRevisionNumber = revision.RevisionNumber
	workflow.CurrentDSLHash = revision.DSLHash
	workflow.UpdatedAt = now
	return nil
}

func (r *MongoAgentRepository) ResolveCurrentWorkflowRevision(ctx context.Context, workflowID primitive.ObjectID, userID uint64) (*WorkflowRevision, error) {
	workflow, err := r.GetWorkflow(ctx, workflowID, userID)
	if err != nil {
		return nil, err
	}
	if !workflow.CurrentRevisionID.IsZero() {
		return r.GetWorkflowRevision(ctx, workflowID, workflow.CurrentRevisionID, userID)
	}

	// Legacy definitions are lazily materialized as revision 1. The unique
	// workflow/revision index makes concurrent migration deterministic.
	revision := newWorkflowRevision(workflow, 1, workflow.UpdatedAt)
	if revision.CreatedAt.IsZero() {
		revision.CreatedAt = time.Now()
	}
	if _, insertErr := r.workflowRevisionColl.InsertOne(ctx, revision); insertErr != nil {
		if !mongo.IsDuplicateKeyError(insertErr) {
			return nil, fmt.Errorf("migrate legacy workflow revision failed: %w", insertErr)
		}
		if err := r.workflowRevisionColl.FindOne(ctx, bson.M{
			"workflow_id": workflowID, "user_id": userID, "revision_number": int64(1),
		}).Decode(revision); err != nil {
			return nil, fmt.Errorf("load migrated workflow revision failed: %w", err)
		}
	}

	res, updateErr := r.workflowColl.UpdateOne(ctx,
		bson.M{
			"_id": workflowID, "user_id": userID,
			"$or": bson.A{
				bson.M{"current_revision_id": bson.M{"$exists": false}},
				bson.M{"current_revision_id": primitive.NilObjectID},
			},
		},
		bson.M{"$set": bson.M{
			"current_revision_id":     revision.ID,
			"current_revision_number": revision.RevisionNumber,
			"current_dsl_hash":        revision.DSLHash,
		}},
	)
	if updateErr != nil {
		return nil, fmt.Errorf("attach migrated workflow revision failed: %w", updateErr)
	}
	if res.MatchedCount == 0 {
		latest, getErr := r.GetWorkflow(ctx, workflowID, userID)
		if getErr != nil {
			return nil, getErr
		}
		if latest.CurrentRevisionID.IsZero() {
			return nil, ErrWorkflowRevisionConflict
		}
		return r.GetWorkflowRevision(ctx, workflowID, latest.CurrentRevisionID, userID)
	}
	return revision, nil
}

func (r *MongoAgentRepository) GetWorkflowRevision(ctx context.Context, workflowID, revisionID primitive.ObjectID, userID uint64) (*WorkflowRevision, error) {
	var revision WorkflowRevision
	err := r.workflowRevisionColl.FindOne(ctx, bson.M{
		"_id": revisionID, "workflow_id": workflowID, "user_id": userID,
	}).Decode(&revision)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("workflow revision not found: %s", revisionID.Hex())
		}
		return nil, fmt.Errorf("find workflow revision failed: %w", err)
	}
	return &revision, nil
}

func (r *MongoAgentRepository) ListWorkflowRevisions(ctx context.Context, workflowID primitive.ObjectID, userID uint64, page, pageSize int) ([]*WorkflowRevision, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	filter := bson.M{"workflow_id": workflowID, "user_id": userID}
	total, err := r.workflowRevisionColl.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("count workflow revisions failed: %w", err)
	}
	cursor, err := r.workflowRevisionColl.Find(ctx, filter, options.Find().
		SetSort(bson.D{{Key: "revision_number", Value: -1}}).
		SetSkip(int64((page-1)*pageSize)).SetLimit(int64(pageSize)))
	if err != nil {
		return nil, 0, fmt.Errorf("find workflow revisions failed: %w", err)
	}
	defer cursor.Close(ctx)
	var revisions []*WorkflowRevision
	if err := cursor.All(ctx, &revisions); err != nil {
		return nil, 0, fmt.Errorf("decode workflow revisions failed: %w", err)
	}
	return revisions, total, nil
}

func (r *MongoAgentRepository) AppendWorkflowStateEvents(ctx context.Context, events []*WorkflowStateEvent) error {
	if len(events) == 0 {
		return nil
	}
	models := make([]mongo.WriteModel, 0, len(events))
	for _, event := range events {
		if event == nil {
			continue
		}
		if event.ID.IsZero() {
			event.ID = primitive.NewObjectID()
		}
		models = append(models, mongo.NewUpdateOneModel().
			SetFilter(bson.M{
				"run_id": event.RunID, "user_id": event.UserID,
				"sequence": event.Sequence, "event_hash": event.EventHash,
			}).
			SetUpdate(bson.M{"$setOnInsert": event}).SetUpsert(true))
	}
	if len(models) == 0 {
		return nil
	}
	if _, err := r.workflowStateEventColl.BulkWrite(ctx, models, options.BulkWrite().SetOrdered(true)); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrWorkflowStateEventConflict
		}
		return fmt.Errorf("append workflow state events failed: %w", err)
	}
	return nil
}

func (r *MongoAgentRepository) ListWorkflowStateEvents(ctx context.Context, runID primitive.ObjectID, userID uint64, afterSequence int64) ([]*WorkflowStateEvent, error) {
	cursor, err := r.workflowStateEventColl.Find(ctx, bson.M{
		"run_id": runID, "user_id": userID, "sequence": bson.M{"$gt": afterSequence},
	}, options.Find().SetSort(bson.D{{Key: "sequence", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("find workflow state events failed: %w", err)
	}
	defer cursor.Close(ctx)
	var events []*WorkflowStateEvent
	if err := cursor.All(ctx, &events); err != nil {
		return nil, fmt.Errorf("decode workflow state events failed: %w", err)
	}
	return events, nil
}

func (r *MongoAgentRepository) ListWorkflowStateEventsForReplay(
	ctx context.Context,
	runID primitive.ObjectID,
	userID uint64,
	atOrBeforeSequence int64,
	limit int64,
) ([]*WorkflowStateEvent, error) {
	if limit < 1 {
		return []*WorkflowStateEvent{}, nil
	}
	cursor, err := r.workflowStateEventColl.Find(ctx, bson.M{
		"run_id": runID, "user_id": userID,
		"sequence": bson.M{"$gt": int64(0), "$lte": atOrBeforeSequence},
	}, options.Find().SetSort(bson.D{{Key: "sequence", Value: 1}}).SetLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("find workflow replay state events failed: %w", err)
	}
	defer cursor.Close(ctx)
	var events []*WorkflowStateEvent
	if err := cursor.All(ctx, &events); err != nil {
		return nil, fmt.Errorf("decode workflow replay state events failed: %w", err)
	}
	return events, nil
}

func (r *MongoAgentRepository) ListWorkflowStateEventsRange(
	ctx context.Context,
	runID primitive.ObjectID,
	userID uint64,
	afterSequence int64,
	atOrBeforeSequence int64,
	limit int64,
) ([]*WorkflowStateEvent, error) {
	if limit < 1 || atOrBeforeSequence <= afterSequence {
		return []*WorkflowStateEvent{}, nil
	}
	cursor, err := r.workflowStateEventColl.Find(ctx, bson.M{
		"run_id": runID, "user_id": userID,
		"sequence": bson.M{"$gt": afterSequence, "$lte": atOrBeforeSequence},
	}, options.Find().SetSort(bson.D{{Key: "sequence", Value: 1}}).SetLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("find workflow state event range failed: %w", err)
	}
	defer cursor.Close(ctx)
	var events []*WorkflowStateEvent
	if err := cursor.All(ctx, &events); err != nil {
		return nil, fmt.Errorf("decode workflow state event range failed: %w", err)
	}
	return events, nil
}

func (r *MongoAgentRepository) SaveWorkflowStateSnapshot(ctx context.Context, snapshot *WorkflowStateSnapshot) error {
	if snapshot == nil {
		return errors.New("workflow state snapshot is required")
	}
	if snapshot.ID.IsZero() {
		snapshot.ID = primitive.NewObjectID()
	}
	if snapshot.CreatedAt.IsZero() {
		snapshot.CreatedAt = time.Now()
	}
	_, err := r.workflowSnapshotColl.UpdateOne(ctx, bson.M{
		"run_id": snapshot.RunID, "user_id": snapshot.UserID,
		"state_version": snapshot.StateVersion, "snapshot_hash": snapshot.SnapshotHash,
	}, bson.M{"$setOnInsert": snapshot}, options.Update().SetUpsert(true))
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrWorkflowSnapshotConflict
		}
		return fmt.Errorf("save workflow state snapshot failed: %w", err)
	}
	return nil
}

func (r *MongoAgentRepository) GetLatestWorkflowStateSnapshot(ctx context.Context, runID primitive.ObjectID, userID uint64, atOrBeforeVersion int64) (*WorkflowStateSnapshot, error) {
	filter := bson.M{
		"run_id": runID, "user_id": userID,
		"state_version": bson.M{"$lte": atOrBeforeVersion},
	}
	var snapshot WorkflowStateSnapshot
	err := r.workflowSnapshotColl.FindOne(ctx, filter,
		options.FindOne().SetSort(bson.D{{Key: "state_version", Value: -1}}),
	).Decode(&snapshot)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, fmt.Errorf("find workflow state snapshot failed: %w", err)
	}
	return &snapshot, nil
}

func (r *MongoAgentRepository) AdvanceWorkflowRunStateVersion(
	ctx context.Context,
	runID primitive.ObjectID,
	userID uint64,
	stateVersion int64,
) error {
	if runID.IsZero() || userID == 0 {
		return errors.New("workflow run identity is incomplete")
	}
	if stateVersion < 0 {
		return errors.New("workflow state version cannot be negative")
	}
	result, err := r.runColl.UpdateOne(ctx, bson.M{
		"_id": runID, "user_id": userID,
		"$or": bson.A{
			bson.M{"state_version": bson.M{"$lt": stateVersion}},
			bson.M{"state_version": bson.M{"$exists": false}},
		},
	}, bson.M{
		"$set": bson.M{"state_version": stateVersion},
		"$inc": bson.M{"revision": 1},
	})
	if err != nil {
		return fmt.Errorf("advance workflow run state version failed: %w", err)
	}
	if result.MatchedCount == 1 {
		return nil
	}

	var current struct {
		StateVersion int64 `bson:"state_version"`
	}
	err = r.runColl.FindOne(
		ctx,
		bson.M{"_id": runID, "user_id": userID},
		options.FindOne().SetProjection(bson.M{"state_version": 1}),
	).Decode(&current)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return errors.New("workflow run not found or not owned by user")
		}
		return fmt.Errorf("verify workflow run state version failed: %w", err)
	}
	if current.StateVersion >= stateVersion {
		return nil
	}
	return fmt.Errorf("workflow run state version did not advance: current=%d target=%d", current.StateVersion, stateVersion)
}

func (r *MongoAgentRepository) cleanupWorkflowRevision(ctx context.Context, revisionID primitive.ObjectID) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	_, _ = r.workflowRevisionColl.DeleteOne(cleanupCtx, bson.M{"_id": revisionID})
}

func (r *MongoAgentRepository) workflowRevisionWasCommitted(ctx context.Context, workflowID primitive.ObjectID, userID uint64, revision *WorkflowRevision, allowLaterRevision bool) (bool, error) {
	verifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	var current struct {
		RevisionID     primitive.ObjectID `bson:"current_revision_id"`
		RevisionNumber int64              `bson:"current_revision_number"`
	}
	err := r.workflowColl.FindOne(verifyCtx, bson.M{"_id": workflowID, "user_id": userID},
		options.FindOne().SetProjection(bson.M{"current_revision_id": 1, "current_revision_number": 1}),
	).Decode(&current)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return false, nil
		}
		return false, err
	}
	return current.RevisionID == revision.ID || (allowLaterRevision && current.RevisionNumber >= revision.RevisionNumber), nil
}
