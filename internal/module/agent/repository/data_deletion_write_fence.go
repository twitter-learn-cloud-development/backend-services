package repository

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	agentdeletion "twitter-clone/internal/module/agent/deletion"
)

const (
	mongoSubjectWriteFenceAdapterName    = "repository.mongo-subject-write-fence"
	mongoSubjectWriteFenceAdapterVersion = "v1"
	mongoSubjectWriteFenceSchema         = "agent-data-deletion-subject-write-fence/v1"
)

var (
	ErrDataDeletionSubjectWriteFenced = errors.New("data deletion subject writes are fenced")
	ErrDataDeletionWriteFenceConflict = errors.New("data deletion subject write fence conflicts with an existing request")
)

// dataDeletionSubjectWriteFenceDocument is the durable identity of a fence.
// Governance decisions are intentionally not persisted here: every enforce
// or replay call must present and revalidate a fresh, short-lived decision.
type dataDeletionSubjectWriteFenceDocument struct {
	SchemaVersion    string    `bson:"schema_version"`
	RequestID        string    `bson:"request_id"`
	SubjectDigest    string    `bson:"subject_digest"`
	PlanDigest       string    `bson:"plan_digest"`
	CapabilityDigest string    `bson:"capability_digest"`
	FenceRevision    int64     `bson:"fence_revision"`
	EnforcedAt       time.Time `bson:"enforced_at"`
}

type mongoSubjectWriteFencePersistence interface {
	Enforce(context.Context, agentdeletion.WriteFenceRequest, time.Time) (agentdeletion.WriteFenceReceipt, error)
	AuthorizeWrite(context.Context, uint64) error
}

// MongoSubjectWriteFence is the Agent-owned durable fence authority. It does
// not claim to cover Tweet or other service-local writes; those services must
// persist and honor their own fence before this adapter may be registered in a
// production deletion worker.
type MongoSubjectWriteFence struct {
	store mongoSubjectWriteFencePersistence
	now   func() time.Time
}

func NewMongoSubjectWriteFence(db *mongo.Database) *MongoSubjectWriteFence {
	return &MongoSubjectWriteFence{
		store: &mongoSubjectWriteFenceStore{db: db},
		now:   time.Now,
	}
}

func (f *MongoSubjectWriteFence) DeletionCapability() agentdeletion.CapabilityDescriptor {
	return agentdeletion.CapabilityDescriptor{
		ID: agentdeletion.CapabilitySubjectWriteFence, Adapter: mongoSubjectWriteFenceAdapterName,
		Version: mongoSubjectWriteFenceAdapterVersion,
	}
}

func (f *MongoSubjectWriteFence) EnforceDataDeletionFence(
	ctx context.Context,
	request agentdeletion.WriteFenceRequest,
) (agentdeletion.WriteFenceReceipt, error) {
	if f == nil || f.store == nil || f.now == nil {
		return agentdeletion.WriteFenceReceipt{}, errors.New("data deletion subject write fence is unavailable")
	}
	now := f.now().UTC()
	request.RequestID = strings.TrimSpace(request.RequestID)
	if err := agentdeletion.ValidateWriteFenceRequest(request, now); err != nil {
		return agentdeletion.WriteFenceReceipt{}, err
	}
	return f.store.Enforce(ctx, request, now)
}

// AuthorizeSubjectWrite is the fail-closed guard for Agent write paths. A
// missing retention subject means no deletion fence has ever been installed;
// an invalid persisted fence is an error, never an implicit allow.
func (f *MongoSubjectWriteFence) AuthorizeSubjectWrite(ctx context.Context, userID uint64) error {
	if f == nil || f.store == nil || userID == 0 {
		return errors.New("data deletion subject write authorization is invalid")
	}
	return f.store.AuthorizeWrite(ctx, userID)
}

type mongoSubjectWriteFenceStore struct {
	db *mongo.Database
}

func (s *mongoSubjectWriteFenceStore) Enforce(
	ctx context.Context,
	request agentdeletion.WriteFenceRequest,
	now time.Time,
) (agentdeletion.WriteFenceReceipt, error) {
	if s == nil || s.db == nil {
		return agentdeletion.WriteFenceReceipt{}, errors.New("data deletion subject write fence store is unavailable")
	}
	collection := s.db.Collection(CollectionAgentRetentionSubjects)
	for attempt := 0; attempt < 2; attempt++ {
		var subject dataRetentionSubjectDocument
		if err := collection.FindOne(ctx, bson.M{"_id": dataRetentionSubjectID(request.UserID)}).Decode(&subject); err != nil {
			if errors.Is(err, mongo.ErrNoDocuments) {
				return agentdeletion.WriteFenceReceipt{}, errors.New("data deletion governance subject is missing")
			}
			return agentdeletion.WriteFenceReceipt{}, fmt.Errorf("read data deletion write fence subject: %w", err)
		}
		fence, existing, err := planSubjectWriteFence(subject, request, now)
		if err != nil {
			return agentdeletion.WriteFenceReceipt{}, err
		}
		if existing {
			return agentdeletion.NewWriteFenceReceipt(request, fence.FenceRevision, now)
		}

		result, err := collection.UpdateOne(ctx, bson.M{
			"_id": requestSubjectID(request), "user_id": request.UserID,
			"revision":      request.Governance.Revision,
			"write_fence":   bson.M{"$exists": false},
			"active_writes": bson.M{"$in": bson.A{int64(0), nil}},
			"$or": bson.A{
				bson.M{"held": bson.M{"$ne": true}},
				bson.M{"hold_until": bson.M{"$lte": now}},
			},
		}, bson.M{"$set": bson.M{"write_fence": fence, "updated_at": now}})
		if err != nil {
			return agentdeletion.WriteFenceReceipt{}, fmt.Errorf("persist data deletion subject write fence: %w", err)
		}
		if result.ModifiedCount == 1 {
			return agentdeletion.NewWriteFenceReceipt(request, fence.FenceRevision, now)
		}
	}
	return agentdeletion.WriteFenceReceipt{}, ErrDataDeletionWriteFenceConflict
}

func (s *mongoSubjectWriteFenceStore) AuthorizeWrite(ctx context.Context, userID uint64) error {
	if s == nil || s.db == nil || userID == 0 {
		return errors.New("data deletion subject write authorization store is unavailable")
	}
	var subject dataRetentionSubjectDocument
	err := s.db.Collection(CollectionAgentRetentionSubjects).FindOne(
		ctx, bson.M{"_id": dataRetentionSubjectID(userID)},
		options.FindOne().SetProjection(bson.M{"user_id": 1, "write_fence": 1}),
	).Decode(&subject)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read data deletion subject write authorization: %w", err)
	}
	if subject.WriteFence == nil {
		if subject.UserID != userID {
			return errors.New("data deletion subject write authorization identity is invalid")
		}
		return nil
	}
	if subject.UserID != userID {
		return errors.New("data deletion subject write authorization identity is invalid")
	}
	if err := validateSubjectWriteFenceDocument(*subject.WriteFence, userID); err != nil {
		return fmt.Errorf("validate data deletion subject write fence: %w", err)
	}
	return ErrDataDeletionSubjectWriteFenced
}

// AcquireSubjectWrite pins a user write before its owned data is changed.
// A crashed writer leaves a positive count, so fence installation stops until
// an operator reconciles the unfinished write instead of risking resurrection.
func (r *MongoAgentRepository) AcquireSubjectWrite(ctx context.Context, userID uint64) error {
	if r == nil || r.retentionSubjectColl == nil || userID == 0 {
		return errors.New("agent subject write lease is unavailable")
	}
	filter := bson.M{
		"_id": dataRetentionSubjectID(userID), "user_id": userID,
		"write_fence": bson.M{"$exists": false},
		"$or": bson.A{
			bson.M{"active_writes": bson.M{"$exists": false}},
			bson.M{"active_writes": bson.M{"$gte": int64(0)}},
		},
	}
	update := bson.M{
		"$inc":         bson.M{"active_writes": 1},
		"$setOnInsert": bson.M{"revision": int64(0)},
	}
	for attempt := 0; attempt < 2; attempt++ {
		result := r.retentionSubjectColl.FindOneAndUpdate(ctx, filter, update,
			options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After))
		var subject dataRetentionSubjectDocument
		if err := result.Decode(&subject); err == nil {
			if subject.UserID != userID || subject.ActiveWrites < 1 {
				return errors.New("agent subject write lease state is invalid")
			}
			return nil
		} else if !mongo.IsDuplicateKeyError(err) {
			return fmt.Errorf("acquire agent subject write lease: %w", err)
		}
		var current dataRetentionSubjectDocument
		if err := r.retentionSubjectColl.FindOne(ctx, bson.M{"_id": dataRetentionSubjectID(userID)}).Decode(&current); err != nil {
			return fmt.Errorf("inspect agent subject write lease conflict: %w", err)
		}
		if current.UserID != userID {
			return errors.New("agent subject write lease identity is invalid")
		}
		if current.WriteFence != nil {
			if err := validateSubjectWriteFenceDocument(*current.WriteFence, userID); err != nil {
				return err
			}
			return ErrDataDeletionSubjectWriteFenced
		}
	}
	return errors.New("agent subject write lease is busy")
}

func (r *MongoAgentRepository) ReleaseSubjectWrite(ctx context.Context, userID uint64) error {
	if r == nil || r.retentionSubjectColl == nil || userID == 0 {
		return errors.New("agent subject write lease is unavailable")
	}
	result, err := r.retentionSubjectColl.UpdateOne(ctx, bson.M{
		"_id": dataRetentionSubjectID(userID), "user_id": userID,
		"active_writes": bson.M{"$gte": 1},
	}, bson.M{"$inc": bson.M{"active_writes": -1}})
	if err != nil {
		return fmt.Errorf("release agent subject write lease: %w", err)
	}
	if result.MatchedCount != 1 {
		return errors.New("agent subject write lease release mismatch")
	}
	return nil
}

func planSubjectWriteFence(
	subject dataRetentionSubjectDocument,
	request agentdeletion.WriteFenceRequest,
	now time.Time,
) (dataDeletionSubjectWriteFenceDocument, bool, error) {
	if err := agentdeletion.ValidateWriteFenceRequest(request, now); err != nil {
		return dataDeletionSubjectWriteFenceDocument{}, false, err
	}
	if subject.UserID != request.UserID || subject.Revision != request.Governance.Revision {
		return dataDeletionSubjectWriteFenceDocument{}, false, errors.New("data deletion write fence governance evidence changed")
	}
	if subject.ActiveWrites < 0 {
		return dataDeletionSubjectWriteFenceDocument{}, false, errors.New("data deletion subject write count is invalid")
	}
	if subject.ActiveWrites != 0 {
		return dataDeletionSubjectWriteFenceDocument{}, false, errors.New("data deletion subject has in-flight writes")
	}
	decision, err := mongoOwnedGovernanceDecision(
		subject, request.UserID, request.Governance.ObservedAt, request.Governance.ValidUntil,
	)
	if err != nil {
		return dataDeletionSubjectWriteFenceDocument{}, false, err
	}
	if decision.Status != agentdeletion.GovernanceClear ||
		decision.EvidenceDigest != request.Governance.EvidenceDigest ||
		decision.Digest != request.Governance.Digest {
		return dataDeletionSubjectWriteFenceDocument{}, false, errors.New("data deletion write fence governance evidence changed")
	}
	if subject.holdActive(now) {
		return dataDeletionSubjectWriteFenceDocument{}, false, agentdeletion.ErrDeletionLegalHold
	}
	if subject.WriteFence != nil {
		if err := validateSubjectWriteFenceDocument(*subject.WriteFence, request.UserID); err != nil {
			return dataDeletionSubjectWriteFenceDocument{}, false, err
		}
		if subject.WriteFence.RequestID != request.RequestID ||
			subject.WriteFence.SubjectDigest != request.SubjectDigest ||
			subject.WriteFence.PlanDigest != request.PlanDigest ||
			subject.WriteFence.CapabilityDigest != request.CapabilityDigest {
			return dataDeletionSubjectWriteFenceDocument{}, false, ErrDataDeletionWriteFenceConflict
		}
		return *subject.WriteFence, true, nil
	}
	return dataDeletionSubjectWriteFenceDocument{
		SchemaVersion: mongoSubjectWriteFenceSchema,
		RequestID:     request.RequestID, SubjectDigest: request.SubjectDigest,
		PlanDigest: request.PlanDigest, CapabilityDigest: request.CapabilityDigest,
		FenceRevision: 1, EnforcedAt: now.UTC(),
	}, false, nil
}

func validateSubjectWriteFenceDocument(fence dataDeletionSubjectWriteFenceDocument, userID uint64) error {
	if fence.SchemaVersion != mongoSubjectWriteFenceSchema || userID == 0 ||
		fence.SubjectDigest != agentdeletion.DeletionSubjectDigest(userID) ||
		!isBoundedWriteFenceIdentity(fence.RequestID, 64) ||
		!isLowerSHA256Digest(fence.PlanDigest) || !isLowerSHA256Digest(fence.CapabilityDigest) ||
		fence.FenceRevision < 1 || fence.EnforcedAt.IsZero() {
		return errors.New("data deletion subject write fence document is invalid")
	}
	return nil
}

func requestSubjectID(request agentdeletion.WriteFenceRequest) string {
	return dataRetentionSubjectID(request.UserID)
}

func isLowerSHA256Digest(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func isBoundedWriteFenceIdentity(value string, limit int) bool {
	if value == "" || len(value) > limit {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '_' || character == '-' ||
			character == '.' || character == '/' {
			continue
		}
		return false
	}
	return true
}
