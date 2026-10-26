package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	CollectionAgentRetentionLeases   = "agent_retention_leases"
	CollectionAgentRetentionSubjects = "agent_retention_subjects"
)

type DataRetentionResource string

const (
	DataRetentionDialogue           DataRetentionResource = "dialogue"
	DataRetentionOrphanMessage      DataRetentionResource = "orphan_message"
	DataRetentionRunTrace           DataRetentionResource = "run_trace"
	DataRetentionStepTrace          DataRetentionResource = "step_trace"
	DataRetentionLLMTrace           DataRetentionResource = "llm_trace"
	DataRetentionToolTrace          DataRetentionResource = "tool_trace"
	DataRetentionWorkflowCheckpoint DataRetentionResource = "workflow_checkpoint"
	DataRetentionAgentCheckpoint    DataRetentionResource = "agent_checkpoint"
	DataRetentionToolResult         DataRetentionResource = "tool_result"
)

type DataRetentionCutoffs struct {
	Now                 time.Time
	Dialogue            time.Time
	OrphanMessage       time.Time
	Trace               time.Time
	TerminalCheckpoint  time.Time
	SuspendedCheckpoint time.Time
	ToolResult          time.Time
}

type DataRetentionCandidate struct {
	UserID          uint64
	Resource        DataRetentionResource
	ObjectID        primitive.ObjectID
	StringID        string
	VersionField    string
	ObservedAt      time.Time
	Status          string
	OutputReference *ToolResultReference
}

type DataRetentionPurgeResult struct {
	PrimaryDocuments int64
	RelatedDocuments int64
}

type DataRetentionRepository interface {
	AcquireDataRetentionLease(context.Context, string, string, time.Time, time.Time) (bool, error)
	AcquireDataRetentionSubjectLease(context.Context, uint64, string, time.Time, time.Time) (DataRetentionSubjectLeaseState, error)
	ReleaseDataRetentionSubjectLease(context.Context, uint64, string) error
	ListDataRetentionCandidates(context.Context, DataRetentionCutoffs, int) ([]DataRetentionCandidate, error)
	ClaimDataRetentionCandidate(context.Context, DataRetentionCandidate, string, time.Time, time.Time) (bool, error)
	CompleteDataRetentionCandidate(context.Context, DataRetentionCandidate, string, time.Time) (DataRetentionPurgeResult, error)
	ReleaseDataRetentionCandidate(context.Context, DataRetentionCandidate, string) error
}

type DataRetentionSubjectLeaseState string

const (
	DataRetentionSubjectLeaseAcquired DataRetentionSubjectLeaseState = "acquired"
	DataRetentionSubjectHeld          DataRetentionSubjectLeaseState = "held"
	DataRetentionSubjectBusy          DataRetentionSubjectLeaseState = "busy"
)

type retentionDocument struct {
	ID              primitive.ObjectID   `bson:"_id"`
	UserID          uint64               `bson:"user_id"`
	CreatedAt       time.Time            `bson:"created_at"`
	UpdatedAt       time.Time            `bson:"updated_at"`
	FinishedAt      time.Time            `bson:"finished_at"`
	SuspendedAt     time.Time            `bson:"suspended_at"`
	Status          string               `bson:"status"`
	OutputReference *ToolResultReference `bson:"output_reference"`
}

type retentionAgentRunDocument struct {
	ID        string    `bson:"_id"`
	UserID    uint64    `bson:"user_id"`
	UpdatedAt time.Time `bson:"updated_at"`
	Status    string    `bson:"status"`
}

func (r *MongoAgentRepository) AcquireDataRetentionLease(
	ctx context.Context,
	leaseName string,
	owner string,
	now time.Time,
	leaseUntil time.Time,
) (bool, error) {
	if r == nil || r.retentionLeaseColl == nil {
		return false, errors.New("data retention lease repository is unavailable")
	}
	if leaseName == "" || owner == "" || now.IsZero() || !leaseUntil.After(now) {
		return false, errors.New("data retention lease identity or interval is invalid")
	}
	filter := bson.M{
		"_id": leaseName,
		"$or": bson.A{
			bson.M{"owner": owner},
			bson.M{"lease_until": bson.M{"$lte": now}},
			bson.M{"lease_until": bson.M{"$exists": false}},
		},
	}
	update := bson.M{"$set": bson.M{
		"owner": owner, "lease_until": leaseUntil, "updated_at": now,
	}}
	result := r.retentionLeaseColl.FindOneAndUpdate(
		ctx,
		filter,
		update,
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	)
	if err := result.Err(); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) || mongo.IsDuplicateKeyError(err) {
			return false, nil
		}
		return false, fmt.Errorf("acquire data retention lease: %w", err)
	}
	return true, nil
}

func (r *MongoAgentRepository) ListDataRetentionCandidates(
	ctx context.Context,
	cutoffs DataRetentionCutoffs,
	limit int,
) ([]DataRetentionCandidate, error) {
	if r == nil {
		return nil, errors.New("data retention repository is unavailable")
	}
	if cutoffs.Now.IsZero() || limit <= 0 {
		return nil, errors.New("data retention scan parameters are invalid")
	}
	candidates := make([]DataRetentionCandidate, 0, limit)
	appendObjectCandidates := func(
		collection *mongo.Collection,
		resource DataRetentionResource,
		filter bson.M,
		versionField string,
		observedAt func(retentionDocument) time.Time,
	) error {
		remaining := limit - len(candidates)
		if remaining <= 0 {
			return nil
		}
		filter = withAvailableRetentionClaim(filter, cutoffs.Now)
		cursor, err := collection.Find(ctx, filter, options.Find().
			SetSort(bson.D{{Key: versionField, Value: 1}, {Key: "_id", Value: 1}}).
			SetLimit(int64(remaining)))
		if err != nil {
			return err
		}
		defer cursor.Close(ctx)
		for cursor.Next(ctx) {
			var document retentionDocument
			if err := cursor.Decode(&document); err != nil {
				return err
			}
			candidates = append(candidates, DataRetentionCandidate{
				UserID: document.UserID, Resource: resource, ObjectID: document.ID, VersionField: versionField,
				ObservedAt: observedAt(document), Status: document.Status,
				OutputReference: document.OutputReference,
			})
		}
		return cursor.Err()
	}

	if err := appendObjectCandidates(
		r.dialogueColl,
		DataRetentionDialogue,
		bson.M{"updated_at": bson.M{"$lte": cutoffs.Dialogue}},
		"updated_at",
		func(document retentionDocument) time.Time { return document.UpdatedAt },
	); err != nil {
		return nil, fmt.Errorf("scan expired dialogues: %w", err)
	}

	remaining := limit - len(candidates)
	if remaining > 0 {
		pipeline := mongo.Pipeline{
			{{Key: "$match", Value: withAvailableRetentionClaim(bson.M{
				"created_at": bson.M{"$lte": cutoffs.OrphanMessage},
			}, cutoffs.Now)}},
			{{Key: "$sort", Value: bson.D{{Key: "created_at", Value: 1}, {Key: "_id", Value: 1}}}},
			{{Key: "$lookup", Value: bson.M{
				"from": CollectionDialogues, "localField": "dialogue_id", "foreignField": "_id", "as": "retention_parent",
			}}},
			{{Key: "$match", Value: bson.M{"retention_parent.0": bson.M{"$exists": false}}}},
			{{Key: "$limit", Value: int64(remaining)}},
			{{Key: "$project", Value: bson.M{"retention_parent": 0}}},
		}
		cursor, err := r.messageColl.Aggregate(ctx, pipeline)
		if err != nil {
			return nil, fmt.Errorf("scan orphan dialogue messages: %w", err)
		}
		defer cursor.Close(ctx)
		for cursor.Next(ctx) {
			var document retentionDocument
			if err := cursor.Decode(&document); err != nil {
				return nil, fmt.Errorf("decode orphan dialogue message: %w", err)
			}
			candidates = append(candidates, DataRetentionCandidate{
				UserID: document.UserID, Resource: DataRetentionOrphanMessage, ObjectID: document.ID,
				VersionField: "created_at", ObservedAt: document.CreatedAt,
			})
		}
		if err := cursor.Err(); err != nil {
			return nil, fmt.Errorf("scan orphan dialogue messages: %w", err)
		}
	}

	traceCollections := []struct {
		resource   DataRetentionResource
		collection *mongo.Collection
	}{
		{DataRetentionRunTrace, r.db.Collection(CollectionAgentRunTraces)},
		{DataRetentionStepTrace, r.db.Collection(CollectionAgentStepTraces)},
		{DataRetentionLLMTrace, r.db.Collection(CollectionAgentLLMCallTraces)},
		{DataRetentionToolTrace, r.db.Collection(CollectionAgentToolCallTraces)},
	}
	for _, trace := range traceCollections {
		if err := appendObjectCandidates(
			trace.collection,
			trace.resource,
			bson.M{"updated_at": bson.M{"$lte": cutoffs.Trace}},
			"updated_at",
			func(document retentionDocument) time.Time { return document.UpdatedAt },
		); err != nil {
			return nil, fmt.Errorf("scan expired %s: %w", trace.resource, err)
		}
	}

	workflowTerminalCheckpointFilter := bson.M{
		"checkpoint_json": bson.M{"$type": "string", "$ne": ""},
		"status":          bson.M{"$in": bson.A{"success", "failed", "rejected", "compensated", "compensation_failed", "canceled"}},
		"finished_at":     bson.M{"$lte": cutoffs.TerminalCheckpoint},
	}
	if err := appendObjectCandidates(
		r.runColl,
		DataRetentionWorkflowCheckpoint,
		workflowTerminalCheckpointFilter,
		"finished_at",
		func(document retentionDocument) time.Time { return document.FinishedAt },
	); err != nil {
		return nil, fmt.Errorf("scan expired terminal workflow checkpoints: %w", err)
	}
	if err := appendObjectCandidates(
		r.runColl,
		DataRetentionWorkflowCheckpoint,
		bson.M{
			"checkpoint_json": bson.M{"$type": "string", "$ne": ""},
			"status":          "suspended", "suspended_at": bson.M{"$lte": cutoffs.SuspendedCheckpoint},
		},
		"suspended_at",
		func(document retentionDocument) time.Time { return document.SuspendedAt },
	); err != nil {
		return nil, fmt.Errorf("scan expired suspended workflow checkpoints: %w", err)
	}

	remaining = limit - len(candidates)
	if remaining > 0 {
		filter := withAvailableRetentionClaim(bson.M{
			"checkpoint_ciphertext": bson.M{"$type": "string", "$ne": ""},
			"$or": bson.A{
				bson.M{"status": bson.M{"$in": bson.A{AgentExecutionRunCompleted, AgentExecutionRunFailed, AgentExecutionRunCanceled}}, "updated_at": bson.M{"$lte": cutoffs.TerminalCheckpoint}},
				bson.M{"status": bson.M{"$in": bson.A{AgentExecutionRunAwaitingHuman, AgentExecutionRunApprovalRequired}}, "updated_at": bson.M{"$lte": cutoffs.SuspendedCheckpoint}},
			},
		}, cutoffs.Now)
		cursor, err := r.agentExecutionRunColl.Find(ctx, filter, options.Find().
			SetSort(bson.D{{Key: "updated_at", Value: 1}, {Key: "_id", Value: 1}}).
			SetLimit(int64(remaining)))
		if err != nil {
			return nil, fmt.Errorf("scan expired agent checkpoints: %w", err)
		}
		defer cursor.Close(ctx)
		for cursor.Next(ctx) {
			var document retentionAgentRunDocument
			if err := cursor.Decode(&document); err != nil {
				return nil, fmt.Errorf("decode expired agent checkpoint: %w", err)
			}
			candidates = append(candidates, DataRetentionCandidate{
				UserID: document.UserID, Resource: DataRetentionAgentCheckpoint, StringID: document.ID,
				VersionField: "updated_at", ObservedAt: document.UpdatedAt, Status: document.Status,
			})
		}
		if err := cursor.Err(); err != nil {
			return nil, fmt.Errorf("scan expired agent checkpoints: %w", err)
		}
	}

	if err := appendObjectCandidates(
		r.executionColl,
		DataRetentionToolResult,
		bson.M{
			"status":     ToolExecutionStatusSucceeded,
			"updated_at": bson.M{"$lte": cutoffs.ToolResult},
			"$or": bson.A{
				bson.M{"output": bson.M{"$exists": true}},
				bson.M{"output_reference": bson.M{"$exists": true}},
			},
		},
		"updated_at",
		func(document retentionDocument) time.Time { return document.UpdatedAt },
	); err != nil {
		return nil, fmt.Errorf("scan expired tool results: %w", err)
	}
	return candidates, nil
}

func (r *MongoAgentRepository) ClaimDataRetentionCandidate(
	ctx context.Context,
	candidate DataRetentionCandidate,
	token string,
	now time.Time,
	claimUntil time.Time,
) (bool, error) {
	collection, identity, err := r.retentionCandidateCollection(candidate)
	if err != nil {
		return false, err
	}
	if token == "" || now.IsZero() || !claimUntil.After(now) {
		return false, errors.New("data retention claim is invalid")
	}
	filter := bson.M{"_id": identity, candidate.VersionField: candidate.ObservedAt}
	filter = withAvailableRetentionClaim(filter, now)
	result, err := collection.UpdateOne(ctx, filter, bson.M{"$set": bson.M{
		"retention_claim_token": token, "retention_claim_until": claimUntil,
	}})
	if err != nil {
		return false, fmt.Errorf("claim %s retention candidate: %w", candidate.Resource, err)
	}
	return result.ModifiedCount == 1, nil
}

func (r *MongoAgentRepository) CompleteDataRetentionCandidate(
	ctx context.Context,
	candidate DataRetentionCandidate,
	token string,
	now time.Time,
) (DataRetentionPurgeResult, error) {
	collection, identity, err := r.retentionCandidateCollection(candidate)
	if err != nil {
		return DataRetentionPurgeResult{}, err
	}
	filter := bson.M{"_id": identity, "retention_claim_token": token}
	result := DataRetentionPurgeResult{}
	switch candidate.Resource {
	case DataRetentionDialogue:
		deleted, deleteErr := collection.DeleteOne(ctx, filter)
		if deleteErr != nil {
			return result, fmt.Errorf("delete expired dialogue: %w", deleteErr)
		}
		result.PrimaryDocuments = deleted.DeletedCount
		if deleted.DeletedCount == 0 {
			return result, nil
		}
		related, deleteErr := r.messageColl.DeleteMany(ctx, bson.M{"dialogue_id": candidate.ObjectID})
		if deleteErr != nil {
			return result, fmt.Errorf("delete messages for expired dialogue: %w", deleteErr)
		}
		result.RelatedDocuments = related.DeletedCount
	case DataRetentionOrphanMessage:
		deleted, deleteErr := collection.DeleteOne(ctx, filter)
		if deleteErr != nil {
			return result, fmt.Errorf("delete orphan dialogue message: %w", deleteErr)
		}
		result.PrimaryDocuments = deleted.DeletedCount
	case DataRetentionRunTrace, DataRetentionStepTrace, DataRetentionLLMTrace, DataRetentionToolTrace:
		deleted, deleteErr := collection.DeleteOne(ctx, filter)
		if deleteErr != nil {
			return result, fmt.Errorf("delete expired %s: %w", candidate.Resource, deleteErr)
		}
		result.PrimaryDocuments = deleted.DeletedCount
	case DataRetentionWorkflowCheckpoint:
		set := bson.M{}
		if candidate.Status == "suspended" {
			set["status"] = "failed"
			set["error_message"] = "workflow resume window expired by data retention policy"
			set["finished_at"] = now
		}
		update := bson.M{"$unset": checkpointUnsetFields(false)}
		if len(set) > 0 {
			update["$set"] = set
		}
		updated, updateErr := collection.UpdateOne(ctx, filter, update)
		if updateErr != nil {
			return result, fmt.Errorf("purge expired workflow checkpoint: %w", updateErr)
		}
		result.PrimaryDocuments = updated.ModifiedCount
	case DataRetentionAgentCheckpoint:
		set := bson.M{"resume_supported": false}
		if candidate.Status == string(AgentExecutionRunAwaitingHuman) || candidate.Status == string(AgentExecutionRunApprovalRequired) {
			set["status"] = AgentExecutionRunFailed
			set["failure_code"] = "retention_expired"
			set["failure_digest"] = "agent resume window expired by data retention policy"
			set["finished_at"] = now
			set["updated_at"] = now
		}
		updated, updateErr := collection.UpdateOne(ctx, filter, bson.M{
			"$set": set, "$unset": checkpointUnsetFields(true),
		})
		if updateErr != nil {
			return result, fmt.Errorf("purge expired agent checkpoint: %w", updateErr)
		}
		result.PrimaryDocuments = updated.ModifiedCount
	case DataRetentionToolResult:
		updated, updateErr := collection.UpdateOne(ctx, filter, bson.M{
			"$set": bson.M{"result_purged_at": now},
			"$unset": bson.M{
				"output": "", "output_reference": "", "retention_claim_token": "", "retention_claim_until": "",
			},
		})
		if updateErr != nil {
			return result, fmt.Errorf("purge expired tool result: %w", updateErr)
		}
		result.PrimaryDocuments = updated.ModifiedCount
	default:
		return result, fmt.Errorf("unsupported data retention resource %q", candidate.Resource)
	}
	return result, nil
}

func (r *MongoAgentRepository) ReleaseDataRetentionCandidate(
	ctx context.Context,
	candidate DataRetentionCandidate,
	token string,
) error {
	collection, identity, err := r.retentionCandidateCollection(candidate)
	if err != nil {
		return err
	}
	_, err = collection.UpdateOne(ctx, bson.M{
		"_id": identity, "retention_claim_token": token,
	}, bson.M{"$unset": bson.M{"retention_claim_token": "", "retention_claim_until": ""}})
	if err != nil {
		return fmt.Errorf("release %s retention candidate: %w", candidate.Resource, err)
	}
	return nil
}

func (r *MongoAgentRepository) retentionCandidateCollection(candidate DataRetentionCandidate) (*mongo.Collection, interface{}, error) {
	if r == nil || r.db == nil {
		return nil, nil, errors.New("data retention repository is unavailable")
	}
	if candidate.VersionField != "updated_at" && candidate.VersionField != "finished_at" && candidate.VersionField != "suspended_at" && candidate.VersionField != "created_at" {
		return nil, nil, errors.New("data retention candidate version field is invalid")
	}
	if candidate.Resource == DataRetentionAgentCheckpoint {
		if candidate.StringID == "" {
			return nil, nil, errors.New("agent checkpoint retention candidate has no identity")
		}
		return r.agentExecutionRunColl, candidate.StringID, nil
	}
	if candidate.ObjectID.IsZero() {
		return nil, nil, errors.New("data retention candidate has no identity")
	}
	switch candidate.Resource {
	case DataRetentionDialogue:
		return r.dialogueColl, candidate.ObjectID, nil
	case DataRetentionOrphanMessage:
		return r.messageColl, candidate.ObjectID, nil
	case DataRetentionRunTrace:
		return r.db.Collection(CollectionAgentRunTraces), candidate.ObjectID, nil
	case DataRetentionStepTrace:
		return r.db.Collection(CollectionAgentStepTraces), candidate.ObjectID, nil
	case DataRetentionLLMTrace:
		return r.db.Collection(CollectionAgentLLMCallTraces), candidate.ObjectID, nil
	case DataRetentionToolTrace:
		return r.db.Collection(CollectionAgentToolCallTraces), candidate.ObjectID, nil
	case DataRetentionWorkflowCheckpoint:
		return r.runColl, candidate.ObjectID, nil
	case DataRetentionToolResult:
		return r.executionColl, candidate.ObjectID, nil
	default:
		return nil, nil, fmt.Errorf("unsupported data retention resource %q", candidate.Resource)
	}
}

func withAvailableRetentionClaim(filter bson.M, now time.Time) bson.M {
	filter["$and"] = bson.A{
		bson.M{"$or": bson.A{
			bson.M{"retention_claim_until": bson.M{"$exists": false}},
			bson.M{"retention_claim_until": bson.M{"$lte": now}},
		}},
	}
	return filter
}

func checkpointUnsetFields(agent bool) bson.M {
	fields := bson.M{
		"checkpoint_json": "", "waiting_node_id": "", "approval_request_id": "",
		"resume_token": "", "resume_token_hash": "", "resume_attempt_id": "",
		"resume_grant_issued_at": "", "resume_grant_expires_at": "",
		"retention_claim_token": "", "retention_claim_until": "",
	}
	if agent {
		fields = bson.M{
			"checkpoint_version": "", "checkpoint_key_id": "", "checkpoint_nonce": "",
			"checkpoint_ciphertext": "", "checkpoint_digest": "", "checkpoint_size_bytes": "",
			"pending_action_type": "", "pending_action_name": "", "pending_action_id": "",
			"pending_resume_kind": "", "approval_request_id": "", "approval_input_digest": "",
			"approval_idempotency_key": "", "approval_expires_at": "",
			"resume_attempt_id": "", "resume_lease_until": "", "resume_claimed_at": "",
			"resume_token_hash": "", "resume_grant_issued_at": "", "resume_grant_expires_at": "",
			"retention_claim_token": "", "retention_claim_until": "",
		}
	}
	return fields
}
