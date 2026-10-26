package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readconcern"
	"go.mongodb.org/mongo-driver/mongo/writeconcern"

	agentdeletion "twitter-clone/internal/module/agent/deletion"
)

const (
	CollectionAgentDataDeletionOperations = "agent_data_deletion_operations"
	mongoOwnedDeletionAdapterName         = "repository.mongo-owned"
	mongoOwnedDeletionAdapterVersion      = "v1"
	mongoOwnedDeletionOperationSchema     = "agent-mongo-owned-deletion-operation/v1"
	mongoOwnedDeletionGovernanceSchema    = "agent-mongo-owned-deletion-governance/v1"
)

type mongoOwnedDeletionResource struct {
	Name       string
	Collection string
	Filter     bson.M
}

// mongoOwnedDeletionResources is the single source of truth for documents
// that are exclusively owned by one user. Shared project/MCP and governance
// records are deliberately absent.
func mongoOwnedDeletionResources(userID uint64) []mongoOwnedDeletionResource {
	byUser := bson.M{"user_id": userID}
	return []mongoOwnedDeletionResource{
		{Name: "dialogues", Collection: CollectionDialogues, Filter: byUser},
		{Name: "dialogue_messages", Collection: CollectionMessages, Filter: byUser},
		{Name: "workflow_definitions", Collection: CollectionWorkflows, Filter: byUser},
		{Name: "workflow_revisions", Collection: CollectionWorkflowRevisions, Filter: byUser},
		{Name: "workflow_runs", Collection: CollectionRuns, Filter: byUser},
		{Name: "workflow_state_events", Collection: CollectionWorkflowStateEvents, Filter: byUser},
		{Name: "workflow_state_snapshots", Collection: CollectionWorkflowSnapshots, Filter: byUser},
		{Name: "workflow_compensations", Collection: CollectionWorkflowCompensations, Filter: byUser},
		{Name: "provider_configs", Collection: CollectionProviderConfigs, Filter: byUser},
		{Name: "agent_task_templates", Collection: CollectionAgentTaskTemplates, Filter: byUser},
		{Name: "workflow_tool_publications", Collection: CollectionWorkflowToolPublications, Filter: byUser},
		{Name: "agent_execution_runs", Collection: CollectionAgentExecutionRuns, Filter: byUser},
		{Name: "run_traces", Collection: CollectionAgentRunTraces, Filter: byUser},
		{Name: "step_traces", Collection: CollectionAgentStepTraces, Filter: byUser},
		{Name: "llm_call_traces", Collection: CollectionAgentLLMCallTraces, Filter: byUser},
		{Name: "tool_call_traces", Collection: CollectionAgentToolCallTraces, Filter: byUser},
		{Name: "tool_approvals", Collection: CollectionToolApprovals, Filter: byUser},
		{Name: "tool_executions", Collection: CollectionToolExecutions, Filter: byUser},
	}
}

func MongoOwnedDeletionScopeDigest() string {
	parts := []string{"mongo-owned-manifest/v1"}
	for _, resource := range mongoOwnedDeletionResources(1) {
		parts = append(parts, resource.Name, resource.Collection, "user_id=subject")
	}
	return agentdeletion.DeletionScopeDigest(parts...)
}

type mongoOwnedDeletionOperation struct {
	ID               string                         `bson:"_id"`
	SchemaVersion    string                         `bson:"schema_version"`
	OperationDigest  string                         `bson:"operation_digest"`
	RequestID        string                         `bson:"request_id"`
	StageID          string                         `bson:"stage_id"`
	SubjectDigest    string                         `bson:"subject_digest"`
	PlanDigest       string                         `bson:"plan_digest"`
	CapabilityDigest string                         `bson:"capability_digest"`
	ScopeDigest      string                         `bson:"scope_digest"`
	ManifestDigest   string                         `bson:"manifest_digest"`
	BeforeCounts     map[string]int64               `bson:"before_counts"`
	AfterCounts      map[string]int64               `bson:"after_counts"`
	CountsDigest     string                         `bson:"counts_digest"`
	Receipt          agentdeletion.ExecutionReceipt `bson:"receipt"`
	CompletedAt      time.Time                      `bson:"completed_at"`
}

type mongoOwnedDeletionPersistence interface {
	Observe(context.Context, uint64, time.Time) (map[string]int64, error)
	IssueGovernanceDecision(context.Context, uint64, time.Time, time.Time) (agentdeletion.GovernanceDecision, error)
	Delete(context.Context, agentdeletion.AdapterRequest, time.Time) (mongoOwnedDeletionOperation, bool, error)
}

type MongoOwnedDeletionAdapter struct {
	store mongoOwnedDeletionPersistence
	now   func() time.Time
}

func NewMongoOwnedDeletionAdapter(db *mongo.Database) *MongoOwnedDeletionAdapter {
	return &MongoOwnedDeletionAdapter{store: &mongoOwnedDeletionMongoStore{db: db}, now: time.Now}
}

func (a *MongoOwnedDeletionAdapter) DeletionCapability() agentdeletion.CapabilityDescriptor {
	return agentdeletion.CapabilityDescriptor{
		ID: agentdeletion.CapabilityMongoOwnedDelete, Adapter: mongoOwnedDeletionAdapterName,
		Version: mongoOwnedDeletionAdapterVersion,
	}
}

func (a *MongoOwnedDeletionAdapter) EnsureIndexes(ctx context.Context) error {
	store, ok := a.store.(*mongoOwnedDeletionMongoStore)
	if !ok || store.db == nil {
		return errors.New("mongo owned deletion operation store is unavailable")
	}
	_, err := store.db.Collection(CollectionAgentDataDeletionOperations).Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "subject_digest", Value: 1}, {Key: "completed_at", Value: -1}}, Options: options.Index().SetName("idx_subject_completed")},
		{Keys: bson.D{{Key: "request_id", Value: 1}, {Key: "stage_id", Value: 1}}, Options: options.Index().SetName("idx_request_stage")},
	})
	if err != nil {
		return fmt.Errorf("create mongo owned deletion operation indexes: %w", err)
	}
	return nil
}

// IssueGovernanceDecision creates a short-lived decision from the authoritative
// retention subject. Delete rechecks the same revision and evidence in its
// transaction before any owned document is changed.
func (a *MongoOwnedDeletionAdapter) IssueGovernanceDecision(
	ctx context.Context,
	userID uint64,
	now time.Time,
	validUntil time.Time,
) (agentdeletion.GovernanceDecision, error) {
	if a == nil || a.store == nil {
		return agentdeletion.GovernanceDecision{}, errors.New("mongo owned deletion adapter is unavailable")
	}
	return a.store.IssueGovernanceDecision(ctx, userID, now, validUntil)
}

func (a *MongoOwnedDeletionAdapter) Observe(ctx context.Context, request agentdeletion.ObservationRequest) (agentdeletion.PostState, error) {
	if a == nil || a.store == nil || a.now == nil {
		return agentdeletion.PostState{}, errors.New("mongo owned deletion adapter is unavailable")
	}
	if err := agentdeletion.ValidateObservationRequest(request); err != nil {
		return agentdeletion.PostState{}, err
	}
	if request.Capability != a.DeletionCapability() || request.ScopeDigest != MongoOwnedDeletionScopeDigest() {
		return agentdeletion.PostState{}, errors.New("mongo owned deletion observation binding is invalid")
	}
	now := a.now().UTC()
	counts, err := a.store.Observe(ctx, request.UserID, now)
	if err != nil {
		return agentdeletion.PostState{}, err
	}
	return agentdeletion.NewPostState(request.Capability, request.SubjectDigest, request.ScopeDigest, sumDeletionCounts(counts), now)
}

func (a *MongoOwnedDeletionAdapter) Delete(ctx context.Context, request agentdeletion.AdapterRequest) (agentdeletion.AdapterResult, error) {
	if a == nil || a.store == nil || a.now == nil {
		return agentdeletion.AdapterResult{}, errors.New("mongo owned deletion adapter is unavailable")
	}
	now := a.now().UTC()
	if err := agentdeletion.ValidateAdapterRequest(request, now); err != nil {
		return agentdeletion.AdapterResult{}, err
	}
	if err := agentdeletion.ValidateAdapterBinding(a, request); err != nil {
		return agentdeletion.AdapterResult{}, err
	}
	if request.ScopeDigest != MongoOwnedDeletionScopeDigest() {
		return agentdeletion.AdapterResult{}, errors.New("mongo owned deletion scope does not match the fixed manifest")
	}
	operation, replayed, err := a.store.Delete(ctx, request, now)
	if err != nil {
		return agentdeletion.AdapterResult{}, err
	}
	if err := validateMongoOwnedDeletionOperation(operation, request); err != nil {
		return agentdeletion.AdapterResult{}, err
	}
	return agentdeletion.AdapterResult{
		Receipt: operation.Receipt, Replayed: replayed,
		AuthorizationDigest: request.Governance.Digest,
	}, nil
}

type mongoOwnedDeletionMongoStore struct {
	db *mongo.Database
}

func (s *mongoOwnedDeletionMongoStore) Observe(ctx context.Context, userID uint64, _ time.Time) (map[string]int64, error) {
	if s == nil || s.db == nil || userID == 0 {
		return nil, errors.New("mongo owned deletion observation is invalid")
	}
	return countMongoOwnedDeletionResources(ctx, s.db, userID)
}

func (s *mongoOwnedDeletionMongoStore) IssueGovernanceDecision(
	ctx context.Context,
	userID uint64,
	now time.Time,
	validUntil time.Time,
) (agentdeletion.GovernanceDecision, error) {
	if s == nil || s.db == nil || userID == 0 || now.IsZero() || !validUntil.After(now) {
		return agentdeletion.GovernanceDecision{}, errors.New("mongo owned deletion governance request is invalid")
	}
	collection := s.db.Collection(CollectionAgentRetentionSubjects)
	_, err := collection.UpdateOne(ctx, bson.M{"_id": dataRetentionSubjectID(userID)}, bson.M{
		"$setOnInsert": bson.M{"user_id": userID, "held": false, "created_at": now.UTC()},
		"$max":         bson.M{"revision": int64(1)},
	}, options.Update().SetUpsert(true))
	if err != nil {
		return agentdeletion.GovernanceDecision{}, fmt.Errorf("initialize mongo deletion governance subject: %w", err)
	}
	var subject dataRetentionSubjectDocument
	if err := collection.FindOne(ctx, bson.M{"_id": dataRetentionSubjectID(userID)}).Decode(&subject); err != nil {
		return agentdeletion.GovernanceDecision{}, fmt.Errorf("read mongo deletion governance subject: %w", err)
	}
	return mongoOwnedGovernanceDecision(subject, userID, now, validUntil)
}

func (s *mongoOwnedDeletionMongoStore) Delete(
	ctx context.Context,
	request agentdeletion.AdapterRequest,
	now time.Time,
) (mongoOwnedDeletionOperation, bool, error) {
	if s == nil || s.db == nil {
		return mongoOwnedDeletionOperation{}, false, errors.New("mongo owned deletion store is unavailable")
	}
	session, err := s.db.Client().StartSession()
	if err != nil {
		return mongoOwnedDeletionOperation{}, false, fmt.Errorf("start mongo owned deletion transaction: %w", err)
	}
	defer session.EndSession(ctx)

	var operation mongoOwnedDeletionOperation
	replayed := false
	transactionOptions := options.Transaction().
		SetReadConcern(readconcern.Snapshot()).
		SetWriteConcern(writeconcern.Majority())
	_, err = session.WithTransaction(ctx, func(sessionContext mongo.SessionContext) (interface{}, error) {
		if err := s.recheckGovernance(sessionContext, request, now); err != nil {
			return nil, err
		}
		existing, exists, err := s.getOperation(sessionContext, request.OperationKey)
		if err != nil {
			return nil, err
		}
		if exists {
			if err := validateMongoOwnedDeletionOperation(existing, request); err != nil {
				return nil, err
			}
			operation = existing
			replayed = true
			return nil, nil
		}

		beforeCounts, err := countMongoOwnedDeletionResources(sessionContext, s.db, request.UserID)
		if err != nil {
			return nil, err
		}
		beforeState, err := agentdeletion.NewPostState(
			request.Capability, request.SubjectDigest, request.ScopeDigest, sumDeletionCounts(beforeCounts), now,
		)
		if err != nil {
			return nil, err
		}
		for _, resource := range mongoOwnedDeletionResources(request.UserID) {
			if _, err := s.db.Collection(resource.Collection).DeleteMany(sessionContext, resource.Filter); err != nil {
				return nil, fmt.Errorf("delete mongo owned resource %s: %w", resource.Name, err)
			}
		}
		afterCounts, err := countMongoOwnedDeletionResources(sessionContext, s.db, request.UserID)
		if err != nil {
			return nil, err
		}
		if sumDeletionCounts(afterCounts) != 0 {
			return nil, agentdeletion.ErrDeletionResidualsRemain
		}
		afterState, err := agentdeletion.NewPostState(
			request.Capability, request.SubjectDigest, request.ScopeDigest, 0, now,
		)
		if err != nil {
			return nil, err
		}
		receipt, err := agentdeletion.NewExecutionReceipt(request, beforeState, afterState, now)
		if err != nil {
			return nil, err
		}
		operation = newMongoOwnedDeletionOperation(request, beforeCounts, afterCounts, receipt, now)
		if _, err := s.db.Collection(CollectionAgentDataDeletionOperations).InsertOne(sessionContext, operation); err != nil {
			return nil, fmt.Errorf("persist mongo owned deletion operation: %w", err)
		}
		return nil, nil
	}, transactionOptions)
	if err != nil {
		return mongoOwnedDeletionOperation{}, false, fmt.Errorf("execute mongo owned deletion transaction: %w", err)
	}
	return operation, replayed, nil
}

func (s *mongoOwnedDeletionMongoStore) recheckGovernance(
	ctx context.Context,
	request agentdeletion.AdapterRequest,
	now time.Time,
) error {
	filter := bson.M{
		"_id": dataRetentionSubjectID(request.UserID), "user_id": request.UserID,
		"revision": request.Governance.Revision,
		"$or":      bson.A{bson.M{"held": bson.M{"$ne": true}}, bson.M{"hold_until": bson.M{"$lte": now}}},
	}
	var subject dataRetentionSubjectDocument
	err := s.db.Collection(CollectionAgentRetentionSubjects).FindOneAndUpdate(
		ctx, filter, bson.M{"$set": bson.M{"deletion_guard_observed_at": now.UTC()}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&subject)
	if errors.Is(err, mongo.ErrNoDocuments) {
		if inspectErr := s.db.Collection(CollectionAgentRetentionSubjects).FindOne(
			ctx, bson.M{"_id": dataRetentionSubjectID(request.UserID)},
		).Decode(&subject); inspectErr != nil {
			return fmt.Errorf("inspect changed mongo deletion governance: %w", inspectErr)
		}
		if subject.UserID == request.UserID && subject.holdActive(now) {
			return agentdeletion.ErrDeletionLegalHold
		}
		return errors.New("mongo owned deletion governance evidence changed")
	}
	if err != nil {
		return fmt.Errorf("recheck mongo owned deletion governance: %w", err)
	}
	if subject.UserID != request.UserID {
		return errors.New("mongo owned deletion governance subject is invalid")
	}
	decision, err := mongoOwnedGovernanceDecision(subject, request.UserID, request.Governance.ObservedAt, request.Governance.ValidUntil)
	if err != nil {
		return err
	}
	if decision.Status != agentdeletion.GovernanceClear || decision.Revision != request.Governance.Revision ||
		decision.EvidenceDigest != request.Governance.EvidenceDigest || decision.Digest != request.Governance.Digest {
		return errors.New("mongo owned deletion governance evidence changed")
	}
	return nil
}

func (s *mongoOwnedDeletionMongoStore) getOperation(ctx context.Context, operationKey string) (mongoOwnedDeletionOperation, bool, error) {
	var operation mongoOwnedDeletionOperation
	err := s.db.Collection(CollectionAgentDataDeletionOperations).FindOne(ctx, bson.M{"_id": operationKey}).Decode(&operation)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return mongoOwnedDeletionOperation{}, false, nil
	}
	if err != nil {
		return mongoOwnedDeletionOperation{}, false, fmt.Errorf("read mongo owned deletion operation: %w", err)
	}
	return operation, true, nil
}

func countMongoOwnedDeletionResources(ctx context.Context, db *mongo.Database, userID uint64) (map[string]int64, error) {
	counts := make(map[string]int64, len(mongoOwnedDeletionResources(userID)))
	for _, resource := range mongoOwnedDeletionResources(userID) {
		count, err := db.Collection(resource.Collection).CountDocuments(ctx, resource.Filter)
		if err != nil {
			return nil, fmt.Errorf("count mongo owned resource %s: %w", resource.Name, err)
		}
		counts[resource.Name] = count
	}
	return counts, nil
}

func mongoOwnedGovernanceDecision(
	subject dataRetentionSubjectDocument,
	userID uint64,
	observedAt time.Time,
	validUntil time.Time,
) (agentdeletion.GovernanceDecision, error) {
	status := agentdeletion.GovernanceClear
	if subject.holdActive(observedAt) {
		status = agentdeletion.GovernanceHeld
	}
	evidence := mongoOwnedGovernanceEvidenceDigest(subject, userID, status)
	return agentdeletion.NewGovernanceDecision(userID, status, subject.Revision, evidence, observedAt, validUntil)
}

func mongoOwnedGovernanceEvidenceDigest(subject dataRetentionSubjectDocument, userID uint64, status agentdeletion.GovernanceStatus) string {
	return deletionSHA256(strings.Join([]string{
		mongoOwnedDeletionGovernanceSchema, agentdeletion.DeletionSubjectDigest(userID), string(status),
		strconv.FormatInt(subject.Revision, 10), subject.HoldUntil.UTC().Format(time.RFC3339Nano),
	}, "\x00"))
}

func newMongoOwnedDeletionOperation(
	request agentdeletion.AdapterRequest,
	beforeCounts map[string]int64,
	afterCounts map[string]int64,
	receipt agentdeletion.ExecutionReceipt,
	completedAt time.Time,
) mongoOwnedDeletionOperation {
	operation := mongoOwnedDeletionOperation{
		ID: request.OperationKey, SchemaVersion: mongoOwnedDeletionOperationSchema,
		RequestID: request.RequestID, StageID: request.StageID, SubjectDigest: request.SubjectDigest,
		PlanDigest: request.PlanDigest, CapabilityDigest: request.CapabilityDigest,
		ScopeDigest: request.ScopeDigest, ManifestDigest: MongoOwnedDeletionScopeDigest(),
		BeforeCounts: cloneDeletionCounts(beforeCounts), AfterCounts: cloneDeletionCounts(afterCounts),
		Receipt: receipt, CompletedAt: completedAt.UTC(),
	}
	operation.CountsDigest = mongoOwnedCountsDigest(operation.BeforeCounts, operation.AfterCounts)
	operation.OperationDigest = mongoOwnedOperationDigest(operation)
	return operation
}

func validateMongoOwnedDeletionOperation(operation mongoOwnedDeletionOperation, request agentdeletion.AdapterRequest) error {
	if err := validateMongoOwnedCounts(operation.BeforeCounts); err != nil {
		return err
	}
	if err := validateMongoOwnedCounts(operation.AfterCounts); err != nil {
		return err
	}
	if operation.SchemaVersion != mongoOwnedDeletionOperationSchema || operation.ID != request.OperationKey ||
		operation.RequestID != request.RequestID || operation.StageID != request.StageID ||
		operation.SubjectDigest != request.SubjectDigest || operation.PlanDigest != request.PlanDigest ||
		operation.CapabilityDigest != request.CapabilityDigest || operation.ScopeDigest != request.ScopeDigest ||
		operation.ManifestDigest != MongoOwnedDeletionScopeDigest() || operation.CompletedAt.IsZero() ||
		operation.CountsDigest != mongoOwnedCountsDigest(operation.BeforeCounts, operation.AfterCounts) ||
		operation.OperationDigest != mongoOwnedOperationDigest(operation) || sumDeletionCounts(operation.AfterCounts) != 0 {
		return errors.New("persisted mongo owned deletion operation does not match the request")
	}
	if err := agentdeletion.ValidateExecutionReceipt(operation.Receipt); err != nil {
		return err
	}
	if operation.Receipt.RequestID != request.RequestID || operation.Receipt.StageID != request.StageID ||
		operation.Receipt.SubjectDigest != request.SubjectDigest || operation.Receipt.PlanDigest != request.PlanDigest ||
		operation.Receipt.CapabilityDigest != request.CapabilityDigest || operation.Receipt.Capability != request.Capability ||
		operation.Receipt.ScopeDigest != request.ScopeDigest || operation.Receipt.OperationKey != request.OperationKey ||
		operation.Receipt.CompletedAt != operation.CompletedAt || operation.Receipt.ReceiptDigest == "" ||
		operation.Receipt.BeforeCount != sumDeletionCounts(operation.BeforeCounts) || operation.Receipt.AfterCount != 0 {
		return errors.New("persisted mongo owned deletion receipt does not match collection counts")
	}
	return nil
}

func validateMongoOwnedCounts(counts map[string]int64) error {
	resources := mongoOwnedDeletionResources(1)
	if len(counts) != len(resources) {
		return errors.New("mongo owned deletion counts do not cover the fixed manifest")
	}
	for _, resource := range resources {
		count, exists := counts[resource.Name]
		if !exists || count < 0 {
			return errors.New("mongo owned deletion counts are invalid")
		}
	}
	return nil
}

func mongoOwnedCountsDigest(beforeCounts, afterCounts map[string]int64) string {
	parts := []string{"mongo-owned-counts/v1"}
	for _, category := range []struct {
		name   string
		counts map[string]int64
	}{{"before", beforeCounts}, {"after", afterCounts}} {
		keys := make([]string, 0, len(category.counts))
		for key := range category.counts {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			parts = append(parts, category.name+":"+key+"="+strconv.FormatInt(category.counts[key], 10))
		}
	}
	return deletionSHA256(strings.Join(parts, "\x00"))
}

func mongoOwnedOperationDigest(operation mongoOwnedDeletionOperation) string {
	return deletionSHA256(strings.Join([]string{
		operation.SchemaVersion, operation.ID, operation.RequestID, operation.StageID,
		operation.SubjectDigest, operation.PlanDigest, operation.CapabilityDigest,
		operation.ScopeDigest, operation.ManifestDigest, operation.CountsDigest,
		operation.Receipt.ReceiptDigest, operation.CompletedAt.UTC().Format(time.RFC3339Nano),
	}, "\x00"))
}

func sumDeletionCounts(counts map[string]int64) int64 {
	var total int64
	for _, count := range counts {
		if count > 0 {
			total += count
		}
	}
	return total
}

func cloneDeletionCounts(counts map[string]int64) map[string]int64 {
	clone := make(map[string]int64, len(counts))
	for key, count := range counts {
		clone[key] = count
	}
	return clone
}

func deletionSHA256(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
