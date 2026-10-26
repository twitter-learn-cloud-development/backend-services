package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	agentdeletion "twitter-clone/internal/module/agent/deletion"
)

const CollectionAgentDataDeletionRequests = "agent_data_deletion_requests"

var ErrActiveDataDeletionRequest = errors.New("an active agent data deletion request already exists")

type DataDeletionRequestStatus string

const (
	DataDeletionRequested          DataDeletionRequestStatus = "requested"
	DataDeletionPreflightRunning   DataDeletionRequestStatus = "preflight_running"
	DataDeletionBlockedLegalHold   DataDeletionRequestStatus = "blocked_legal_hold"
	DataDeletionBlockedSubjectBusy DataDeletionRequestStatus = "blocked_subject_busy"
	DataDeletionPreviewReady       DataDeletionRequestStatus = "preview_ready"
	DataDeletionPreflightFailed    DataDeletionRequestStatus = "preflight_failed"
	DataDeletionCancelled          DataDeletionRequestStatus = "cancelled"
	DataDeletionCompleted          DataDeletionRequestStatus = "completed"
)

type DataDeletionRequest struct {
	ID                  primitive.ObjectID         `bson:"_id" json:"id"`
	UserID              uint64                     `bson:"user_id" json:"user_id"`
	Status              DataDeletionRequestStatus  `bson:"status" json:"status"`
	ReasonCode          string                     `bson:"reason_code" json:"reason_code"`
	ActorDigest         string                     `bson:"actor_digest" json:"actor_digest"`
	ScopeVersion        string                     `bson:"scope_version,omitempty" json:"scope_version,omitempty"`
	Inventory           map[string]int64           `bson:"inventory,omitempty" json:"inventory,omitempty"`
	RelatedInventory    map[string]int64           `bson:"related_inventory,omitempty" json:"related_inventory,omitempty"`
	RetainedInventory   map[string]int64           `bson:"retained_inventory,omitempty" json:"retained_inventory,omitempty"`
	Unsupported         []string                   `bson:"unsupported_resources,omitempty" json:"unsupported_resources,omitempty"`
	ExecutionProtocol   *agentdeletion.Protocol    `bson:"execution_protocol,omitempty" json:"execution_protocol,omitempty"`
	ExecutionSupported  bool                       `bson:"execution_supported" json:"execution_supported"`
	ExecutionAudit      []agentdeletion.AuditEvent `bson:"execution_audit,omitempty" json:"execution_audit,omitempty"`
	ExecutionAuditCount int64                      `bson:"execution_audit_count" json:"execution_audit_count"`
	ExecutionAuditTail  string                     `bson:"execution_audit_tail_digest,omitempty" json:"execution_audit_tail_digest,omitempty"`
	FailureCode         string                     `bson:"failure_code,omitempty" json:"failure_code,omitempty"`
	RequestedAt         time.Time                  `bson:"requested_at" json:"requested_at"`
	PreflightedAt       time.Time                  `bson:"preflighted_at,omitempty" json:"preflighted_at,omitempty"`
	CancelledAt         time.Time                  `bson:"cancelled_at,omitempty" json:"cancelled_at,omitempty"`
	CompletedAt         time.Time                  `bson:"completed_at,omitempty" json:"completed_at,omitempty"`
	UpdatedAt           time.Time                  `bson:"updated_at" json:"updated_at"`
	LeaseToken          string                     `bson:"lease_token,omitempty" json:"-"`
	LeaseUntil          time.Time                  `bson:"lease_until,omitempty" json:"-"`
	Revision            int64                      `bson:"revision" json:"revision"`
	Active              bool                       `bson:"active" json:"active"`
}

// DataDeletionInventory separates user-owned deletion candidates from shared
// relationships and governance evidence. Only Owned is eligible for a future
// deletion executor; Related and Retained require independent policy decisions.
type DataDeletionInventory struct {
	Owned    map[string]int64 `json:"owned"`
	Related  map[string]int64 `json:"related"`
	Retained map[string]int64 `json:"retained"`
}

func ValidateDataDeletionRequestInput(userID uint64, reasonCode, actor string, now time.Time) error {
	reasonCode = strings.TrimSpace(reasonCode)
	actor = strings.TrimSpace(actor)
	if userID == 0 || now.IsZero() {
		return errors.New("data deletion request subject or time is invalid")
	}
	if reasonCode == "" || len(reasonCode) > 64 {
		return errors.New("data deletion request reason code must contain 1 to 64 characters")
	}
	for _, value := range reasonCode {
		if !unicode.IsLower(value) && !unicode.IsDigit(value) && value != '_' && value != '-' && value != '.' {
			return errors.New("data deletion request reason code contains unsupported characters")
		}
	}
	if actor == "" || len(actor) > 128 {
		return errors.New("data deletion request actor must contain 1 to 128 characters")
	}
	return nil
}

func DataDeletionActorDigest(actor string) string {
	digest := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(actor))))
	return hex.EncodeToString(digest[:])
}

func (r *MongoAgentRepository) CreateDataDeletionRequest(ctx context.Context, userID uint64, reasonCode, actor string, now time.Time) (*DataDeletionRequest, error) {
	if r == nil || r.dataDeletionRequestColl == nil {
		return nil, errors.New("data deletion request repository is unavailable")
	}
	reasonCode = strings.TrimSpace(reasonCode)
	actor = strings.TrimSpace(actor)
	if err := ValidateDataDeletionRequestInput(userID, reasonCode, actor, now); err != nil {
		return nil, err
	}
	request := &DataDeletionRequest{
		ID: primitive.NewObjectID(), UserID: userID, Status: DataDeletionRequested,
		ReasonCode: reasonCode, ActorDigest: DataDeletionActorDigest(actor),
		ExecutionSupported: false, RequestedAt: now, UpdatedAt: now, Revision: 1, Active: true,
	}
	if _, err := r.dataDeletionRequestColl.InsertOne(ctx, request); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil, ErrActiveDataDeletionRequest
		}
		return nil, fmt.Errorf("create data deletion request: %w", err)
	}
	return request, nil
}

func (r *MongoAgentRepository) GetDataDeletionRequest(ctx context.Context, requestID primitive.ObjectID) (*DataDeletionRequest, error) {
	if r == nil || r.dataDeletionRequestColl == nil || requestID.IsZero() {
		return nil, errors.New("data deletion request identity is invalid")
	}
	var request DataDeletionRequest
	if err := r.dataDeletionRequestColl.FindOne(ctx, bson.M{"_id": requestID}).Decode(&request); err != nil {
		return nil, fmt.Errorf("get data deletion request: %w", err)
	}
	return &request, nil
}

func (r *MongoAgentRepository) ClaimDataDeletionPreflight(ctx context.Context, requestID primitive.ObjectID, token string, now, leaseUntil time.Time) (*DataDeletionRequest, bool, error) {
	if r == nil || r.dataDeletionRequestColl == nil || requestID.IsZero() || token == "" || now.IsZero() || !leaseUntil.After(now) {
		return nil, false, errors.New("data deletion preflight claim is invalid")
	}
	filter := bson.M{
		"_id": requestID, "active": true,
		"status": bson.M{"$in": bson.A{DataDeletionRequested, DataDeletionBlockedLegalHold, DataDeletionBlockedSubjectBusy, DataDeletionPreviewReady, DataDeletionPreflightFailed}},
		"$and": bson.A{
			bson.M{"$or": bson.A{bson.M{"lease_until": bson.M{"$lte": now}}, bson.M{"lease_until": bson.M{"$exists": false}}}},
			dataDeletionNoAuditEventsFilter(),
		},
	}
	for key, value := range dataDeletionNoActiveStageFilter(now) {
		filter[key] = value
	}
	update := bson.M{
		"$set": bson.M{"status": DataDeletionPreflightRunning, "lease_token": token, "lease_until": leaseUntil, "updated_at": now, "failure_code": ""},
		"$inc": bson.M{"revision": 1},
	}
	var request DataDeletionRequest
	err := r.dataDeletionRequestColl.FindOneAndUpdate(ctx, filter, update, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&request)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("claim data deletion preflight: %w", err)
	}
	return &request, true, nil
}

func (r *MongoAgentRepository) CompleteDataDeletionPreflight(ctx context.Context, requestID primitive.ObjectID, token string, status DataDeletionRequestStatus, inventory DataDeletionInventory, protocol *agentdeletion.Protocol, unsupported []string, failureCode string, now time.Time) (*DataDeletionRequest, error) {
	if r == nil || r.dataDeletionRequestColl == nil || requestID.IsZero() || token == "" || now.IsZero() {
		return nil, errors.New("data deletion preflight completion is invalid")
	}
	if status != DataDeletionBlockedLegalHold && status != DataDeletionBlockedSubjectBusy && status != DataDeletionPreviewReady && status != DataDeletionPreflightFailed {
		return nil, errors.New("data deletion preflight completion status is invalid")
	}
	update := bson.M{
		"$set": bson.M{
			"status": status, "scope_version": "agent-data-deletion-preview/v2", "inventory": inventory.Owned,
			"related_inventory": inventory.Related, "retained_inventory": inventory.Retained,
			"execution_protocol":    protocol,
			"unsupported_resources": unsupported, "execution_supported": false, "failure_code": failureCode,
			"execution_audit": []agentdeletion.AuditEvent{}, "execution_audit_count": int64(0),
			"execution_audit_tail_digest": "",
			"preflighted_at":              now, "updated_at": now,
		},
		"$unset": bson.M{"lease_token": "", "lease_until": ""}, "$inc": bson.M{"revision": 1},
	}
	var request DataDeletionRequest
	err := r.dataDeletionRequestColl.FindOneAndUpdate(ctx, bson.M{
		"_id": requestID, "active": true, "status": DataDeletionPreflightRunning, "lease_token": token,
	}, update, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&request)
	if err != nil {
		return nil, fmt.Errorf("complete data deletion preflight: %w", err)
	}
	return &request, nil
}

func (r *MongoAgentRepository) CancelDataDeletionRequest(ctx context.Context, requestID primitive.ObjectID, now time.Time) (*DataDeletionRequest, error) {
	if r == nil || r.dataDeletionRequestColl == nil || requestID.IsZero() || now.IsZero() {
		return nil, errors.New("data deletion cancellation is invalid")
	}
	filter := bson.M{
		"_id": requestID, "active": true,
		"$or": bson.A{bson.M{"status": bson.M{"$ne": DataDeletionPreflightRunning}}, bson.M{"lease_until": bson.M{"$lte": now}}},
	}
	for key, value := range dataDeletionNoActiveStageFilter(now) {
		filter[key] = value
	}
	var request DataDeletionRequest
	err := r.dataDeletionRequestColl.FindOneAndUpdate(ctx, filter, bson.M{
		"$set":   bson.M{"status": DataDeletionCancelled, "active": false, "cancelled_at": now, "updated_at": now, "execution_supported": false},
		"$unset": bson.M{"lease_token": "", "lease_until": ""}, "$inc": bson.M{"revision": 1},
	}, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&request)
	if err != nil {
		return nil, fmt.Errorf("cancel data deletion request: %w", err)
	}
	return &request, nil
}

func (r *MongoAgentRepository) CompareAndSwapDataDeletionCheckpoint(
	ctx context.Context,
	requestID primitive.ObjectID,
	expectedRequestRevision int64,
	expectedCheckpointRevision int64,
	expectedAuditCount int64,
	expectedAuditTail string,
	protocol agentdeletion.Protocol,
	auditEvent agentdeletion.AuditEvent,
	now time.Time,
) (*DataDeletionRequest, bool, error) {
	if r == nil || r.dataDeletionRequestColl == nil || requestID.IsZero() || expectedRequestRevision < 1 ||
		expectedCheckpointRevision < 1 || expectedAuditCount < 0 || expectedAuditCount >= agentdeletion.MaxAuditEvents ||
		protocol.Checkpoint.Revision != expectedCheckpointRevision+1 || now.IsZero() {
		return nil, false, errors.New("data deletion checkpoint compare-and-swap is invalid")
	}
	if err := agentdeletion.ValidateProtocol(protocol); err != nil {
		return nil, false, fmt.Errorf("validate data deletion protocol: %w", err)
	}
	if err := agentdeletion.ValidateAuditEvent(auditEvent); err != nil {
		return nil, false, fmt.Errorf("validate data deletion audit event: %w", err)
	}
	if auditEvent.RequestID != requestID.Hex() || auditEvent.Sequence != expectedAuditCount+1 ||
		auditEvent.RequestRevision != expectedRequestRevision+1 ||
		auditEvent.CheckpointRevision != protocol.Checkpoint.Revision ||
		auditEvent.PlanDigest != protocol.Plan.Digest || auditEvent.CapabilityDigest != protocol.Plan.CapabilityDigest ||
		auditEvent.PreviousDigest != expectedAuditTail || !auditEvent.OccurredAt.Equal(now.UTC()) {
		return nil, false, errors.New("data deletion audit event does not match checkpoint transition")
	}
	filter := bson.M{
		"_id": requestID, "active": true, "status": DataDeletionPreviewReady,
		"execution_supported":                       true,
		"revision":                                  expectedRequestRevision,
		"execution_protocol.plan.ready":             true,
		"execution_protocol.plan.digest":            protocol.Plan.Digest,
		"execution_protocol.checkpoint.plan_digest": protocol.Plan.Digest,
		"execution_protocol.checkpoint.revision":    expectedCheckpointRevision,
		"execution_audit_count":                     expectedAuditCount,
		"execution_audit_tail_digest":               expectedAuditTail,
	}
	set := bson.M{
		"execution_protocol.checkpoint": protocol.Checkpoint, "updated_at": now.UTC(),
		"execution_audit_tail_digest": auditEvent.EventDigest,
	}
	update := bson.M{
		"$set":  set,
		"$inc":  bson.M{"revision": 1, "execution_audit_count": 1},
		"$push": bson.M{"execution_audit": auditEvent},
	}
	if dataDeletionProtocolCompleted(protocol) {
		if auditEvent.Type != agentdeletion.AuditStageSucceeded || auditEvent.StageID != agentdeletion.StageFinalize {
			return nil, false, errors.New("data deletion final checkpoint requires a matching success audit event")
		}
		set["status"] = DataDeletionCompleted
		set["active"] = false
		set["execution_supported"] = false
		set["completed_at"] = now.UTC()
		update["$unset"] = bson.M{"lease_token": "", "lease_until": ""}
	}
	var request DataDeletionRequest
	err := r.dataDeletionRequestColl.FindOneAndUpdate(
		ctx, filter, update, options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&request)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("compare and swap data deletion checkpoint: %w", err)
	}
	return &request, true, nil
}

func dataDeletionProtocolCompleted(protocol agentdeletion.Protocol) bool {
	if protocol.Checkpoint.CurrentStageID != "" || len(protocol.Plan.Stages) == 0 ||
		len(protocol.Checkpoint.Stages) != len(protocol.Plan.Stages) {
		return false
	}
	for index, checkpoint := range protocol.Checkpoint.Stages {
		if checkpoint.StageID != protocol.Plan.Stages[index].ID || checkpoint.Status != agentdeletion.StageSucceeded {
			return false
		}
	}
	return protocol.Checkpoint.Stages[len(protocol.Checkpoint.Stages)-1].StageID == agentdeletion.StageFinalize
}

func dataDeletionNoActiveStageFilter(now time.Time) bson.M {
	return bson.M{
		"$nor": bson.A{
			bson.M{
				"execution_protocol.checkpoint.stages": bson.M{
					"$elemMatch": bson.M{"status": agentdeletion.StageRunning, "lease_until": bson.M{"$gt": now}},
				},
			},
		},
	}
}

func dataDeletionNoAuditEventsFilter() bson.M {
	return bson.M{
		"$or": bson.A{
			bson.M{"execution_audit_count": int64(0)},
			bson.M{"execution_audit_count": bson.M{"$exists": false}},
		},
	}
}

type dataDeletionInventoryClass string

const (
	dataDeletionOwned    dataDeletionInventoryClass = "owned"
	dataDeletionRelated  dataDeletionInventoryClass = "related"
	dataDeletionRetained dataDeletionInventoryClass = "retained"
)

type dataDeletionInventoryQuery struct {
	class      dataDeletionInventoryClass
	name       string
	collection string
	filter     bson.M
}

func dataDeletionInventoryQueries(userID uint64) []dataDeletionInventoryQuery {
	byUser := bson.M{"user_id": userID}
	queries := make([]dataDeletionInventoryQuery, 0, len(mongoOwnedDeletionResources(userID))+20)
	for _, resource := range mongoOwnedDeletionResources(userID) {
		queries = append(queries, dataDeletionInventoryQuery{
			class: dataDeletionOwned, name: resource.Name,
			collection: resource.Collection, filter: resource.Filter,
		})
	}
	queries = append(queries, []dataDeletionInventoryQuery{
		// MCP connections and snapshots can be project-scoped. Relationship
		// execution rechecks project ownership before deleting subject records.
		dataDeletionInventoryQuery{dataDeletionRelated, "mcp_connections_personal", CollectionMCPConnections, personalMCPConnectionFilter(userID)},
		{dataDeletionRelated, "project_ownerships", CollectionAgentProjects, bson.M{"owner_id": userID}},
		{dataDeletionRelated, "project_memberships", CollectionAgentProjects, bson.M{"members.user_id": userID}},
		{dataDeletionRelated, "project_scoped_mcp_connections_created", CollectionMCPConnections, bson.M{"user_id": userID, "scope": "project"}},
		{dataDeletionRelated, "mcp_tool_snapshots", CollectionMCPToolSnapshots, byUser},
		{dataDeletionRelated, "profile_role_bindings", CollectionProfileRoleBindings, byUser},
		{dataDeletionRelated, "marketplace_publisher_ownerships", CollectionExtensionMarketplacePublishers, bson.M{"owner_user_ids": userID}},

		{dataDeletionRetained, "product_events", CollectionAgentProductEvents, byUser},
		{dataDeletionRetained, "published_content_attributions", CollectionAgentPublishedContentAttributions, bson.M{"author_user_id": userID}},
		{dataDeletionRetained, "data_deletion_requests", CollectionAgentDataDeletionRequests, byUser},
		{dataDeletionRetained, "retention_subject_state", CollectionAgentRetentionSubjects, bson.M{"_id": dataRetentionSubjectID(userID)}},
		{dataDeletionRetained, "profile_versions_authored", CollectionProfileVersions, bson.M{"$or": bson.A{bson.M{"created_by": userID}, bson.M{"published_by": userID}}}},
		{dataDeletionRetained, "profile_releases_authored", CollectionProfileReleases, bson.M{"$or": bson.A{bson.M{"created_by": userID}, bson.M{"updated_by": userID}}}},
		{dataDeletionRetained, "profile_audit_events", CollectionProfileAudits, bson.M{"actor_user_id": userID}},
		{dataDeletionRetained, "profile_publish_approvals", CollectionProfilePublishApprovals, bson.M{"$or": bson.A{bson.M{"requested_by": userID}, bson.M{"decided_by": userID}, bson.M{"applying_by": userID}}}},
		{dataDeletionRetained, "profile_experiments_authored", CollectionProfileExperiments, bson.M{"$or": bson.A{bson.M{"created_by": userID}, bson.M{"updated_by": userID}}}},
		{dataDeletionRetained, "profile_experiment_outcomes_recorded", CollectionProfileExperimentRuns, bson.M{"outcome_recorded_by": userID}},
		{dataDeletionRetained, "profile_role_audit_events", CollectionProfileRoleAudits, bson.M{"$or": bson.A{bson.M{"actor_user_id": userID}, bson.M{"subject_user_id": userID}}}},
		{dataDeletionRetained, "marketplace_releases_authored", CollectionExtensionMarketplaceReleases, bson.M{"$or": bson.A{bson.M{"published_by": userID}, bson.M{"withdrawn_by": userID}}}},
		{dataDeletionRetained, "marketplace_audit_events", CollectionExtensionMarketplaceAudit, bson.M{"actor_user_id": userID}},
	}...)
	return queries
}

func (r *MongoAgentRepository) CountAgentDataDeletionInventory(ctx context.Context, userID uint64) (DataDeletionInventory, error) {
	if r == nil || r.db == nil || userID == 0 {
		return DataDeletionInventory{}, errors.New("data deletion inventory subject is invalid")
	}
	inventory := DataDeletionInventory{
		Owned: make(map[string]int64), Related: make(map[string]int64), Retained: make(map[string]int64),
	}
	for _, resource := range dataDeletionInventoryQueries(userID) {
		count, err := r.db.Collection(resource.collection).CountDocuments(ctx, resource.filter)
		if err != nil {
			return DataDeletionInventory{}, fmt.Errorf("count %s for data deletion preview: %w", resource.name, err)
		}
		switch resource.class {
		case dataDeletionOwned:
			inventory.Owned[resource.name] = count
		case dataDeletionRelated:
			inventory.Related[resource.name] = count
		case dataDeletionRetained:
			inventory.Retained[resource.name] = count
		}
	}
	return inventory, nil
}
