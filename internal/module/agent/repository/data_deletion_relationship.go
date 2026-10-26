package repository

import (
	"context"
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
	relationshipDeletionAdapterName     = "repository.mongo-relationships"
	relationshipDeletionAdapterVersion  = "v1"
	relationshipDeletionOperationSchema = "agent-relationship-deletion-operation/v1"
	relationshipDeletionManifestSchema  = "agent-relationship-deletion-manifest/v1"
)

var ErrRelationshipOwnershipRequiresResolution = errors.New("shared relationship ownership requires transfer before deletion")

type relationshipDeletionResource struct {
	Name       string
	Collection string
	Operation  string
}

type relationshipDeletionBlocker struct {
	Name       string
	Collection string
	Filter     bson.M
}

func relationshipDeletionResources() []relationshipDeletionResource {
	return []relationshipDeletionResource{
		{Name: "project_memberships", Collection: CollectionAgentProjects, Operation: "pull_non_owner_member"},
		{Name: "mcp_connections_personal", Collection: CollectionMCPConnections, Operation: "delete_personal_connections"},
		{Name: "mcp_tool_snapshots", Collection: CollectionMCPToolSnapshots, Operation: "delete_subject_snapshots_after_project_owner_guard"},
		{Name: "profile_role_bindings", Collection: CollectionProfileRoleBindings, Operation: "delete_subject_binding"},
	}
}

func relationshipDeletionBlockers(userID uint64) []relationshipDeletionBlocker {
	return []relationshipDeletionBlocker{
		{Name: "project_ownerships", Collection: CollectionAgentProjects, Filter: bson.M{"owner_id": userID}},
		{Name: "project_scoped_mcp_connections_created", Collection: CollectionMCPConnections, Filter: bson.M{"user_id": userID, "scope": "project"}},
		{Name: "marketplace_publisher_ownerships", Collection: CollectionExtensionMarketplacePublishers, Filter: bson.M{"owner_user_ids": userID}},
	}
}

func RelationshipDeletionScopeDigest() string {
	parts := []string{relationshipDeletionManifestSchema}
	for _, resource := range relationshipDeletionResources() {
		parts = append(parts, resource.Name, resource.Collection, resource.Operation)
	}
	for _, blocker := range relationshipDeletionBlockers(1) {
		parts = append(parts, "blocker", blocker.Name, blocker.Collection)
	}
	return agentdeletion.DeletionScopeDigest(parts...)
}

type relationshipDeletionOperation struct {
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

type relationshipDeletionPersistence interface {
	Observe(context.Context, uint64, time.Time) (map[string]int64, error)
	IssueGovernanceDecision(context.Context, uint64, time.Time, time.Time) (agentdeletion.GovernanceDecision, error)
	Delete(context.Context, agentdeletion.AdapterRequest, time.Time) (relationshipDeletionOperation, bool, error)
}

type RelationshipDeletionAdapter struct {
	store relationshipDeletionPersistence
	now   func() time.Time
}

func NewRelationshipDeletionAdapter(db *mongo.Database) *RelationshipDeletionAdapter {
	return &RelationshipDeletionAdapter{store: &relationshipDeletionMongoStore{db: db}, now: time.Now}
}

func (a *RelationshipDeletionAdapter) DeletionCapability() agentdeletion.CapabilityDescriptor {
	return agentdeletion.CapabilityDescriptor{
		ID:      agentdeletion.CapabilityRelationshipMutation,
		Adapter: relationshipDeletionAdapterName,
		Version: relationshipDeletionAdapterVersion,
	}
}

func (a *RelationshipDeletionAdapter) EnsureIndexes(ctx context.Context) error {
	store, ok := a.store.(*relationshipDeletionMongoStore)
	if !ok || store.db == nil {
		return errors.New("relationship deletion operation store is unavailable")
	}
	_, err := store.db.Collection(CollectionAgentDataDeletionOperations).Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "subject_digest", Value: 1}, {Key: "completed_at", Value: -1}}, Options: options.Index().SetName("idx_subject_completed")},
		{Keys: bson.D{{Key: "request_id", Value: 1}, {Key: "stage_id", Value: 1}}, Options: options.Index().SetName("idx_request_stage")},
	})
	if err != nil {
		return fmt.Errorf("create relationship deletion operation indexes: %w", err)
	}
	return nil
}

func (a *RelationshipDeletionAdapter) IssueGovernanceDecision(
	ctx context.Context,
	userID uint64,
	now time.Time,
	validUntil time.Time,
) (agentdeletion.GovernanceDecision, error) {
	if a == nil || a.store == nil {
		return agentdeletion.GovernanceDecision{}, errors.New("relationship deletion adapter is unavailable")
	}
	return a.store.IssueGovernanceDecision(ctx, userID, now, validUntil)
}

func (a *RelationshipDeletionAdapter) Observe(
	ctx context.Context,
	request agentdeletion.ObservationRequest,
) (agentdeletion.PostState, error) {
	if a == nil || a.store == nil || a.now == nil {
		return agentdeletion.PostState{}, errors.New("relationship deletion adapter is unavailable")
	}
	if err := agentdeletion.ValidateObservationRequest(request); err != nil {
		return agentdeletion.PostState{}, err
	}
	if request.Capability != a.DeletionCapability() || request.ScopeDigest != RelationshipDeletionScopeDigest() {
		return agentdeletion.PostState{}, errors.New("relationship deletion observation binding is invalid")
	}
	now := a.now().UTC()
	counts, err := a.store.Observe(ctx, request.UserID, now)
	if err != nil {
		return agentdeletion.PostState{}, err
	}
	return agentdeletion.NewPostState(
		request.Capability,
		request.SubjectDigest,
		request.ScopeDigest,
		sumDeletionCounts(counts),
		now,
	)
}

func (a *RelationshipDeletionAdapter) Delete(
	ctx context.Context,
	request agentdeletion.AdapterRequest,
) (agentdeletion.AdapterResult, error) {
	if a == nil || a.store == nil || a.now == nil {
		return agentdeletion.AdapterResult{}, errors.New("relationship deletion adapter is unavailable")
	}
	now := a.now().UTC()
	if err := agentdeletion.ValidateAdapterRequest(request, now); err != nil {
		return agentdeletion.AdapterResult{}, err
	}
	if err := agentdeletion.ValidateAdapterBinding(a, request); err != nil {
		return agentdeletion.AdapterResult{}, err
	}
	if request.ScopeDigest != RelationshipDeletionScopeDigest() {
		return agentdeletion.AdapterResult{}, errors.New("relationship deletion scope does not match the fixed manifest")
	}
	operation, replayed, err := a.store.Delete(ctx, request, now)
	if err != nil {
		return agentdeletion.AdapterResult{}, err
	}
	if err := validateRelationshipDeletionOperation(operation, request); err != nil {
		return agentdeletion.AdapterResult{}, err
	}
	return agentdeletion.AdapterResult{
		Receipt:             operation.Receipt,
		Replayed:            replayed,
		AuthorizationDigest: request.Governance.Digest,
	}, nil
}

type relationshipDeletionMongoStore struct {
	db *mongo.Database
}

func (s *relationshipDeletionMongoStore) Observe(
	ctx context.Context,
	userID uint64,
	_ time.Time,
) (map[string]int64, error) {
	if s == nil || s.db == nil || userID == 0 {
		return nil, errors.New("relationship deletion observation is invalid")
	}
	return countRelationshipDeletionPostState(ctx, s.db, userID)
}

func (s *relationshipDeletionMongoStore) IssueGovernanceDecision(
	ctx context.Context,
	userID uint64,
	now time.Time,
	validUntil time.Time,
) (agentdeletion.GovernanceDecision, error) {
	if s == nil || s.db == nil || userID == 0 || now.IsZero() || !validUntil.After(now) {
		return agentdeletion.GovernanceDecision{}, errors.New("relationship deletion governance request is invalid")
	}
	collection := s.db.Collection(CollectionAgentRetentionSubjects)
	_, err := collection.UpdateOne(ctx, bson.M{"_id": dataRetentionSubjectID(userID)}, bson.M{
		"$setOnInsert": bson.M{"user_id": userID, "held": false, "created_at": now.UTC()},
		"$max":         bson.M{"revision": int64(1)},
	}, options.Update().SetUpsert(true))
	if err != nil {
		return agentdeletion.GovernanceDecision{}, fmt.Errorf("initialize relationship deletion governance subject: %w", err)
	}
	var subject dataRetentionSubjectDocument
	if err := collection.FindOne(ctx, bson.M{"_id": dataRetentionSubjectID(userID)}).Decode(&subject); err != nil {
		return agentdeletion.GovernanceDecision{}, fmt.Errorf("read relationship deletion governance subject: %w", err)
	}
	return mongoOwnedGovernanceDecision(subject, userID, now, validUntil)
}

func (s *relationshipDeletionMongoStore) Delete(
	ctx context.Context,
	request agentdeletion.AdapterRequest,
	now time.Time,
) (relationshipDeletionOperation, bool, error) {
	if s == nil || s.db == nil {
		return relationshipDeletionOperation{}, false, errors.New("relationship deletion store is unavailable")
	}
	session, err := s.db.Client().StartSession()
	if err != nil {
		return relationshipDeletionOperation{}, false, fmt.Errorf("start relationship deletion transaction: %w", err)
	}
	defer session.EndSession(ctx)

	var operation relationshipDeletionOperation
	replayed := false
	transactionOptions := options.Transaction().
		SetReadConcern(readconcern.Snapshot()).
		SetWriteConcern(writeconcern.Majority())
	_, err = session.WithTransaction(ctx, func(sessionContext mongo.SessionContext) (interface{}, error) {
		if err := recheckRelationshipDeletionGovernance(sessionContext, s.db, request, now); err != nil {
			return nil, err
		}
		existing, exists, err := getRelationshipDeletionOperation(sessionContext, s.db, request.OperationKey)
		if err != nil {
			return nil, err
		}
		if exists {
			if err := validateRelationshipDeletionOperation(existing, request); err != nil {
				return nil, err
			}
			operation = existing
			replayed = true
			return nil, nil
		}
		if err := ensureRelationshipDeletionUnblocked(sessionContext, s.db, request.UserID); err != nil {
			return nil, err
		}
		beforeCounts, err := countRelationshipDeletionResources(sessionContext, s.db, request.UserID)
		if err != nil {
			return nil, err
		}
		beforeState, err := agentdeletion.NewPostState(
			request.Capability,
			request.SubjectDigest,
			request.ScopeDigest,
			sumDeletionCounts(beforeCounts),
			now,
		)
		if err != nil {
			return nil, err
		}
		if _, err := s.db.Collection(CollectionAgentProjects).UpdateMany(
			sessionContext,
			bson.M{"owner_id": bson.M{"$ne": request.UserID}, "members.user_id": request.UserID},
			bson.M{
				"$pull": bson.M{"members": bson.M{"user_id": request.UserID}},
				"$inc":  bson.M{"revision": 1},
				"$set":  bson.M{"updated_at": now.UTC()},
			},
		); err != nil {
			return nil, fmt.Errorf("detach project memberships: %w", err)
		}
		// A project-scoped connection created by the subject is an ownership
		// blocker, so every remaining subject snapshot is personal or orphaned.
		if _, err := s.db.Collection(CollectionMCPToolSnapshots).DeleteMany(
			sessionContext,
			bson.M{"user_id": request.UserID},
		); err != nil {
			return nil, fmt.Errorf("delete personal MCP snapshots: %w", err)
		}
		if _, err := s.db.Collection(CollectionMCPConnections).DeleteMany(
			sessionContext,
			personalMCPConnectionFilter(request.UserID),
		); err != nil {
			return nil, fmt.Errorf("delete personal MCP connections: %w", err)
		}
		if _, err := s.db.Collection(CollectionProfileRoleBindings).DeleteMany(
			sessionContext,
			bson.M{"user_id": request.UserID},
		); err != nil {
			return nil, fmt.Errorf("delete profile role binding: %w", err)
		}
		afterCounts, err := countRelationshipDeletionResources(sessionContext, s.db, request.UserID)
		if err != nil {
			return nil, err
		}
		if sumDeletionCounts(afterCounts) != 0 {
			return nil, agentdeletion.ErrDeletionResidualsRemain
		}
		afterState, err := agentdeletion.NewPostState(
			request.Capability,
			request.SubjectDigest,
			request.ScopeDigest,
			0,
			now,
		)
		if err != nil {
			return nil, err
		}
		receipt, err := agentdeletion.NewExecutionReceipt(request, beforeState, afterState, now)
		if err != nil {
			return nil, err
		}
		operation = newRelationshipDeletionOperation(request, beforeCounts, afterCounts, receipt, now)
		if _, err := s.db.Collection(CollectionAgentDataDeletionOperations).InsertOne(
			sessionContext,
			operation,
		); err != nil {
			return nil, fmt.Errorf("persist relationship deletion operation: %w", err)
		}
		return nil, nil
	}, transactionOptions)
	if err != nil {
		return relationshipDeletionOperation{}, false, fmt.Errorf("execute relationship deletion transaction: %w", err)
	}
	return operation, replayed, nil
}

func countRelationshipDeletionResources(
	ctx context.Context,
	db *mongo.Database,
	userID uint64,
) (map[string]int64, error) {
	filters := map[string]struct {
		collection string
		filter     bson.M
	}{
		"project_memberships": {
			CollectionAgentProjects,
			bson.M{"owner_id": bson.M{"$ne": userID}, "members.user_id": userID},
		},
		"mcp_connections_personal": {
			CollectionMCPConnections,
			personalMCPConnectionFilter(userID),
		},
		"mcp_tool_snapshots": {
			CollectionMCPToolSnapshots,
			bson.M{"user_id": userID},
		},
		"profile_role_bindings": {
			CollectionProfileRoleBindings,
			bson.M{"user_id": userID},
		},
	}
	counts := make(map[string]int64, len(filters))
	for _, resource := range relationshipDeletionResources() {
		query := filters[resource.Name]
		count, err := db.Collection(query.collection).CountDocuments(ctx, query.filter)
		if err != nil {
			return nil, fmt.Errorf("count relationship resource %s: %w", resource.Name, err)
		}
		counts[resource.Name] = count
	}
	return counts, nil
}

func countRelationshipDeletionPostState(
	ctx context.Context,
	db *mongo.Database,
	userID uint64,
) (map[string]int64, error) {
	counts, err := countRelationshipDeletionResources(ctx, db, userID)
	if err != nil {
		return nil, err
	}
	for _, blocker := range relationshipDeletionBlockers(userID) {
		count, err := db.Collection(blocker.Collection).CountDocuments(
			ctx,
			blocker.Filter,
			options.Count().SetLimit(1),
		)
		if err != nil {
			return nil, fmt.Errorf("count relationship post-state blocker %s: %w", blocker.Name, err)
		}
		counts[blocker.Name] = count
	}
	return counts, nil
}

func ensureRelationshipDeletionUnblocked(
	ctx context.Context,
	db *mongo.Database,
	userID uint64,
) error {
	for _, blocker := range relationshipDeletionBlockers(userID) {
		count, err := db.Collection(blocker.Collection).CountDocuments(
			ctx,
			blocker.Filter,
			options.Count().SetLimit(1),
		)
		if err != nil {
			return fmt.Errorf("count relationship blocker %s: %w", blocker.Name, err)
		}
		if count > 0 {
			return fmt.Errorf("%w: %s", ErrRelationshipOwnershipRequiresResolution, blocker.Name)
		}
	}
	return nil
}

func recheckRelationshipDeletionGovernance(
	ctx context.Context,
	db *mongo.Database,
	request agentdeletion.AdapterRequest,
	now time.Time,
) error {
	store := mongoOwnedDeletionMongoStore{db: db}
	return store.recheckGovernance(ctx, request, now)
}

func getRelationshipDeletionOperation(
	ctx context.Context,
	db *mongo.Database,
	operationKey string,
) (relationshipDeletionOperation, bool, error) {
	var operation relationshipDeletionOperation
	err := db.Collection(CollectionAgentDataDeletionOperations).
		FindOne(ctx, bson.M{"_id": operationKey}).
		Decode(&operation)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return relationshipDeletionOperation{}, false, nil
	}
	if err != nil {
		return relationshipDeletionOperation{}, false, fmt.Errorf("read relationship deletion operation: %w", err)
	}
	return operation, true, nil
}

func newRelationshipDeletionOperation(
	request agentdeletion.AdapterRequest,
	beforeCounts map[string]int64,
	afterCounts map[string]int64,
	receipt agentdeletion.ExecutionReceipt,
	completedAt time.Time,
) relationshipDeletionOperation {
	operation := relationshipDeletionOperation{
		ID: request.OperationKey, SchemaVersion: relationshipDeletionOperationSchema,
		RequestID: request.RequestID, StageID: request.StageID, SubjectDigest: request.SubjectDigest,
		PlanDigest: request.PlanDigest, CapabilityDigest: request.CapabilityDigest,
		ScopeDigest: request.ScopeDigest, ManifestDigest: RelationshipDeletionScopeDigest(),
		BeforeCounts: cloneDeletionCounts(beforeCounts), AfterCounts: cloneDeletionCounts(afterCounts),
		Receipt: receipt, CompletedAt: completedAt.UTC(),
	}
	operation.CountsDigest = relationshipDeletionCountsDigest(operation.BeforeCounts, operation.AfterCounts)
	operation.OperationDigest = relationshipDeletionOperationDigest(operation)
	return operation
}

func validateRelationshipDeletionOperation(
	operation relationshipDeletionOperation,
	request agentdeletion.AdapterRequest,
) error {
	if err := validateRelationshipDeletionCounts(operation.BeforeCounts); err != nil {
		return err
	}
	if err := validateRelationshipDeletionCounts(operation.AfterCounts); err != nil {
		return err
	}
	if operation.SchemaVersion != relationshipDeletionOperationSchema ||
		operation.ID != request.OperationKey ||
		operation.RequestID != request.RequestID ||
		operation.StageID != request.StageID ||
		operation.SubjectDigest != request.SubjectDigest ||
		operation.PlanDigest != request.PlanDigest ||
		operation.CapabilityDigest != request.CapabilityDigest ||
		operation.ScopeDigest != request.ScopeDigest ||
		operation.ManifestDigest != RelationshipDeletionScopeDigest() ||
		operation.CompletedAt.IsZero() ||
		operation.CountsDigest != relationshipDeletionCountsDigest(operation.BeforeCounts, operation.AfterCounts) ||
		operation.OperationDigest != relationshipDeletionOperationDigest(operation) ||
		sumDeletionCounts(operation.AfterCounts) != 0 {
		return errors.New("persisted relationship deletion operation does not match the request")
	}
	if err := agentdeletion.ValidateExecutionReceipt(operation.Receipt); err != nil {
		return err
	}
	if operation.Receipt.RequestID != request.RequestID ||
		operation.Receipt.StageID != request.StageID ||
		operation.Receipt.SubjectDigest != request.SubjectDigest ||
		operation.Receipt.PlanDigest != request.PlanDigest ||
		operation.Receipt.CapabilityDigest != request.CapabilityDigest ||
		operation.Receipt.Capability != request.Capability ||
		operation.Receipt.ScopeDigest != request.ScopeDigest ||
		operation.Receipt.OperationKey != request.OperationKey ||
		operation.Receipt.CompletedAt != operation.CompletedAt ||
		operation.Receipt.BeforeCount != sumDeletionCounts(operation.BeforeCounts) ||
		operation.Receipt.AfterCount != 0 {
		return errors.New("persisted relationship deletion receipt does not match collection counts")
	}
	return nil
}

func validateRelationshipDeletionCounts(counts map[string]int64) error {
	if len(counts) != len(relationshipDeletionResources()) {
		return errors.New("relationship deletion counts do not cover the fixed manifest")
	}
	for _, resource := range relationshipDeletionResources() {
		if count, exists := counts[resource.Name]; !exists || count < 0 {
			return errors.New("relationship deletion counts are invalid")
		}
	}
	return nil
}

func relationshipDeletionCountsDigest(beforeCounts, afterCounts map[string]int64) string {
	parts := []string{"relationship-deletion-counts/v1"}
	for _, category := range []struct {
		name   string
		counts map[string]int64
	}{
		{name: "before", counts: beforeCounts},
		{name: "after", counts: afterCounts},
	} {
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

func relationshipDeletionOperationDigest(operation relationshipDeletionOperation) string {
	return deletionSHA256(strings.Join([]string{
		operation.SchemaVersion,
		operation.ID,
		operation.RequestID,
		operation.StageID,
		operation.SubjectDigest,
		operation.PlanDigest,
		operation.CapabilityDigest,
		operation.ScopeDigest,
		operation.ManifestDigest,
		operation.CountsDigest,
		operation.Receipt.ReceiptDigest,
		operation.CompletedAt.UTC().Format(time.RFC3339Nano),
	}, "\x00"))
}
