package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
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
	externalDeletionOperationSchema = "agent-external-deletion-operation/v1"
	externalDeletionStatusRunning   = "running"
	externalDeletionStatusCompleted = "completed"
	externalDeletionLeaseDuration   = 2 * time.Minute
)

type externalDeletionOperation struct {
	ID               string                             `bson:"_id"`
	SchemaVersion    string                             `bson:"schema_version"`
	RecordDigest     string                             `bson:"record_digest"`
	RequestID        string                             `bson:"request_id"`
	StageID          string                             `bson:"stage_id"`
	SubjectDigest    string                             `bson:"subject_digest"`
	PlanDigest       string                             `bson:"plan_digest"`
	CapabilityDigest string                             `bson:"capability_digest"`
	Capability       agentdeletion.CapabilityDescriptor `bson:"capability"`
	ScopeDigest      string                             `bson:"scope_digest"`
	GovernanceDigest string                             `bson:"governance_digest"`
	BeforeState      agentdeletion.PostState            `bson:"before_state"`
	Status           string                             `bson:"status"`
	LeaseToken       string                             `bson:"lease_token,omitempty"`
	LeaseUntil       time.Time                          `bson:"lease_until,omitempty"`
	Revision         int64                              `bson:"revision"`
	Receipt          *agentdeletion.ExecutionReceipt    `bson:"receipt,omitempty"`
	CreatedAt        time.Time                          `bson:"created_at"`
	UpdatedAt        time.Time                          `bson:"updated_at"`
	CompletedAt      time.Time                          `bson:"completed_at,omitempty"`
}

// MongoExternalDeletionJournal coordinates non-transactional external stores
// without pretending Mongo and the target store share one transaction. The
// durable before-state lets an expired lease safely reconcile a deletion that
// completed externally before its receipt was persisted.
type MongoExternalDeletionJournal struct {
	db *mongo.Database
}

func NewMongoExternalDeletionJournal(db *mongo.Database) *MongoExternalDeletionJournal {
	return &MongoExternalDeletionJournal{db: db}
}

func (j *MongoExternalDeletionJournal) EnsureIndexes(ctx context.Context) error {
	if j == nil || j.db == nil {
		return errors.New("external deletion operation journal is unavailable")
	}
	_, err := j.db.Collection(CollectionAgentDataDeletionOperations).Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "subject_digest", Value: 1}, {Key: "completed_at", Value: -1}}, Options: options.Index().SetName("idx_subject_completed")},
		{Keys: bson.D{{Key: "request_id", Value: 1}, {Key: "stage_id", Value: 1}}, Options: options.Index().SetName("idx_request_stage")},
		{Keys: bson.D{{Key: "status", Value: 1}, {Key: "lease_until", Value: 1}}, Options: options.Index().SetName("idx_external_status_lease")},
	})
	if err != nil {
		return fmt.Errorf("create external deletion operation indexes: %w", err)
	}
	return nil
}

func (j *MongoExternalDeletionJournal) IssueGovernanceDecision(
	ctx context.Context,
	userID uint64,
	now time.Time,
	validUntil time.Time,
) (agentdeletion.GovernanceDecision, error) {
	if j == nil || j.db == nil {
		return agentdeletion.GovernanceDecision{}, errors.New("external deletion operation journal is unavailable")
	}
	store := mongoOwnedDeletionMongoStore{db: j.db}
	return store.IssueGovernanceDecision(ctx, userID, now, validUntil)
}

func (j *MongoExternalDeletionJournal) Claim(
	ctx context.Context,
	request agentdeletion.AdapterRequest,
	before agentdeletion.PostState,
	now time.Time,
) (agentdeletion.ExternalDeletionClaim, error) {
	if j == nil || j.db == nil || now.IsZero() {
		return agentdeletion.ExternalDeletionClaim{}, errors.New("external deletion operation claim is invalid")
	}
	if err := agentdeletion.ValidatePostState(before); err != nil {
		return agentdeletion.ExternalDeletionClaim{}, err
	}
	token, err := newExternalDeletionLeaseToken()
	if err != nil {
		return agentdeletion.ExternalDeletionClaim{}, err
	}
	leaseUntil := now.Add(externalDeletionLeaseDuration)
	if request.Deadline.Before(leaseUntil) {
		leaseUntil = request.Deadline
	}
	if !leaseUntil.After(now) {
		return agentdeletion.ExternalDeletionClaim{}, agentdeletion.ErrDeletionGovernanceExpired
	}

	session, err := j.db.Client().StartSession()
	if err != nil {
		return agentdeletion.ExternalDeletionClaim{}, fmt.Errorf("start external deletion claim transaction: %w", err)
	}
	defer session.EndSession(ctx)

	var claimed externalDeletionOperation
	replayed := false
	transactionOptions := options.Transaction().SetReadConcern(readconcern.Snapshot()).SetWriteConcern(writeconcern.Majority())
	_, err = session.WithTransaction(ctx, func(sessionContext mongo.SessionContext) (interface{}, error) {
		governanceStore := mongoOwnedDeletionMongoStore{db: j.db}
		if err := governanceStore.recheckGovernance(sessionContext, request, now); err != nil {
			return nil, err
		}
		operation, exists, err := j.get(sessionContext, request.OperationKey)
		if err != nil {
			return nil, err
		}
		if exists {
			if err := validateExternalDeletionOperation(operation, request); err != nil {
				return nil, err
			}
			if operation.Status == externalDeletionStatusCompleted {
				claimed, replayed = operation, true
				return nil, nil
			}
			if operation.LeaseUntil.After(now) {
				return nil, agentdeletion.ErrExternalDeletionOperationBusy
			}
			operation.LeaseToken = token
			operation.LeaseUntil = leaseUntil.UTC()
			operation.GovernanceDigest = request.Governance.Digest
			operation.Revision++
			operation.UpdatedAt = now.UTC()
			operation.RecordDigest = externalDeletionOperationDigest(operation)
			result, err := j.db.Collection(CollectionAgentDataDeletionOperations).ReplaceOne(
				sessionContext,
				bson.M{"_id": operation.ID, "revision": operation.Revision - 1, "status": externalDeletionStatusRunning},
				operation,
			)
			if err != nil {
				return nil, fmt.Errorf("reclaim external deletion operation: %w", err)
			}
			if result.MatchedCount != 1 {
				return nil, agentdeletion.ErrExternalDeletionOperationBusy
			}
			claimed = operation
			return nil, nil
		}

		operation = newExternalDeletionOperation(request, before, token, leaseUntil, now)
		if _, err := j.db.Collection(CollectionAgentDataDeletionOperations).InsertOne(sessionContext, operation); err != nil {
			if mongo.IsDuplicateKeyError(err) {
				return nil, agentdeletion.ErrExternalDeletionOperationBusy
			}
			return nil, fmt.Errorf("persist external deletion operation claim: %w", err)
		}
		claimed = operation
		return nil, nil
	}, transactionOptions)
	if err != nil {
		return agentdeletion.ExternalDeletionClaim{}, fmt.Errorf("claim external deletion operation: %w", err)
	}
	if replayed {
		return agentdeletion.ExternalDeletionClaim{Receipt: claimed.Receipt, Replayed: true}, nil
	}
	return agentdeletion.ExternalDeletionClaim{LeaseToken: claimed.LeaseToken, BeforeState: claimed.BeforeState}, nil
}

func (j *MongoExternalDeletionJournal) Complete(
	ctx context.Context,
	request agentdeletion.AdapterRequest,
	leaseToken string,
	receipt agentdeletion.ExecutionReceipt,
	now time.Time,
) error {
	if j == nil || j.db == nil || strings.TrimSpace(leaseToken) == "" || now.IsZero() {
		return errors.New("external deletion operation completion is invalid")
	}
	if err := validateExternalDeletionReceipt(request, receipt); err != nil {
		return err
	}
	operation, exists, err := j.get(ctx, request.OperationKey)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("external deletion operation claim is missing")
	}
	if err := validateExternalDeletionOperation(operation, request); err != nil {
		return err
	}
	if operation.Status == externalDeletionStatusCompleted {
		if operation.Receipt != nil && operation.Receipt.ReceiptDigest == receipt.ReceiptDigest {
			return nil
		}
		return errors.New("external deletion operation already has a different receipt")
	}
	if operation.LeaseToken != leaseToken || now.After(operation.LeaseUntil) {
		return agentdeletion.ErrExternalDeletionOperationBusy
	}

	previousRevision := operation.Revision
	operation.Status = externalDeletionStatusCompleted
	operation.LeaseToken = ""
	operation.LeaseUntil = time.Time{}
	operation.Revision++
	operation.Receipt = &receipt
	operation.CompletedAt = receipt.CompletedAt.UTC()
	operation.UpdatedAt = now.UTC()
	operation.RecordDigest = externalDeletionOperationDigest(operation)
	result, err := j.db.Collection(CollectionAgentDataDeletionOperations).ReplaceOne(ctx, bson.M{
		"_id": operation.ID, "revision": previousRevision,
		"status": externalDeletionStatusRunning, "lease_token": leaseToken,
	}, operation)
	if err != nil {
		return fmt.Errorf("persist external deletion operation receipt: %w", err)
	}
	if result.MatchedCount != 1 {
		return agentdeletion.ErrExternalDeletionOperationBusy
	}
	return nil
}

func (j *MongoExternalDeletionJournal) get(ctx context.Context, operationKey string) (externalDeletionOperation, bool, error) {
	var operation externalDeletionOperation
	err := j.db.Collection(CollectionAgentDataDeletionOperations).FindOne(ctx, bson.M{"_id": operationKey}).Decode(&operation)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return externalDeletionOperation{}, false, nil
	}
	if err != nil {
		return externalDeletionOperation{}, false, fmt.Errorf("read external deletion operation: %w", err)
	}
	return operation, true, nil
}

func newExternalDeletionOperation(
	request agentdeletion.AdapterRequest,
	before agentdeletion.PostState,
	token string,
	leaseUntil time.Time,
	now time.Time,
) externalDeletionOperation {
	operation := externalDeletionOperation{
		ID: request.OperationKey, SchemaVersion: externalDeletionOperationSchema,
		RequestID: request.RequestID, StageID: request.StageID, SubjectDigest: request.SubjectDigest,
		PlanDigest: request.PlanDigest, CapabilityDigest: request.CapabilityDigest,
		Capability: request.Capability, ScopeDigest: request.ScopeDigest,
		GovernanceDigest: request.Governance.Digest, BeforeState: before,
		Status: externalDeletionStatusRunning, LeaseToken: token, LeaseUntil: leaseUntil.UTC(),
		Revision: 1, CreatedAt: now.UTC(), UpdatedAt: now.UTC(),
	}
	operation.RecordDigest = externalDeletionOperationDigest(operation)
	return operation
}

func validateExternalDeletionOperation(operation externalDeletionOperation, request agentdeletion.AdapterRequest) error {
	if operation.SchemaVersion != externalDeletionOperationSchema || operation.ID != request.OperationKey ||
		operation.RequestID != request.RequestID || operation.StageID != request.StageID ||
		operation.SubjectDigest != request.SubjectDigest || operation.PlanDigest != request.PlanDigest ||
		operation.CapabilityDigest != request.CapabilityDigest || operation.Capability != request.Capability ||
		operation.ScopeDigest != request.ScopeDigest || operation.Revision < 1 || operation.CreatedAt.IsZero() ||
		operation.UpdatedAt.IsZero() || operation.RecordDigest != externalDeletionOperationDigest(operation) {
		return errors.New("persisted external deletion operation does not match the request")
	}
	if err := agentdeletion.ValidatePostState(operation.BeforeState); err != nil {
		return err
	}
	if operation.BeforeState.Capability != request.Capability || operation.BeforeState.SubjectDigest != request.SubjectDigest ||
		operation.BeforeState.ScopeDigest != request.ScopeDigest {
		return errors.New("persisted external deletion before-state does not match the request")
	}
	switch operation.Status {
	case externalDeletionStatusRunning:
		if operation.LeaseToken == "" || operation.LeaseUntil.IsZero() || operation.Receipt != nil || !operation.CompletedAt.IsZero() {
			return errors.New("persisted external deletion running state is invalid")
		}
	case externalDeletionStatusCompleted:
		if operation.LeaseToken != "" || !operation.LeaseUntil.IsZero() || operation.Receipt == nil || operation.CompletedAt.IsZero() {
			return errors.New("persisted external deletion completed state is invalid")
		}
		if err := validateExternalDeletionReceipt(request, *operation.Receipt); err != nil {
			return err
		}
		if operation.CompletedAt != operation.Receipt.CompletedAt {
			return errors.New("persisted external deletion completion time is invalid")
		}
	default:
		return errors.New("persisted external deletion status is invalid")
	}
	return nil
}

func validateExternalDeletionReceipt(request agentdeletion.AdapterRequest, receipt agentdeletion.ExecutionReceipt) error {
	if err := agentdeletion.ValidateExecutionReceipt(receipt); err != nil {
		return err
	}
	if receipt.RequestID != request.RequestID || receipt.StageID != request.StageID ||
		receipt.SubjectDigest != request.SubjectDigest || receipt.PlanDigest != request.PlanDigest ||
		receipt.CapabilityDigest != request.CapabilityDigest || receipt.Capability != request.Capability ||
		receipt.ScopeDigest != request.ScopeDigest || receipt.OperationKey != request.OperationKey {
		return errors.New("external deletion receipt does not match the operation")
	}
	return nil
}

func externalDeletionOperationDigest(operation externalDeletionOperation) string {
	receiptDigest := ""
	if operation.Receipt != nil {
		receiptDigest = operation.Receipt.ReceiptDigest
	}
	return deletionSHA256(strings.Join([]string{
		operation.SchemaVersion, operation.ID, operation.RequestID, operation.StageID,
		operation.SubjectDigest, operation.PlanDigest, operation.CapabilityDigest,
		operation.Capability.ID, operation.Capability.Adapter, operation.Capability.Version,
		operation.ScopeDigest, operation.GovernanceDigest, operation.BeforeState.StateDigest,
		operation.Status, operation.LeaseToken, operation.LeaseUntil.UTC().Format(time.RFC3339Nano),
		fmt.Sprintf("%d", operation.Revision), receiptDigest,
		operation.CreatedAt.UTC().Format(time.RFC3339Nano), operation.UpdatedAt.UTC().Format(time.RFC3339Nano),
		operation.CompletedAt.UTC().Format(time.RFC3339Nano),
	}, "\x00"))
}

func newExternalDeletionLeaseToken() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate external deletion lease token: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}
