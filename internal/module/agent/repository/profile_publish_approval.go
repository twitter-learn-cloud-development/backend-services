package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"twitter-clone/internal/module/agent/profile"
)

const (
	ProfilePublishApprovalStatusPending     = "pending"
	ProfilePublishApprovalStatusApplying    = "applying"
	ProfilePublishApprovalStatusApplied     = "applied"
	ProfilePublishApprovalStatusRejected    = "rejected"
	ProfilePublishApprovalStatusApplyFailed = "apply_failed"

	ProfilePublishDecisionApproved = "approved"
	ProfilePublishDecisionRejected = "rejected"

	defaultProfilePublishApplyLease = time.Minute
	maxProfilePublishApplyLease     = 15 * time.Minute
)

var (
	ErrProfilePublishApprovalNotFound = errors.New("profile publish approval not found")
	ErrProfilePublishApprovalConflict = errors.New("profile publish approval conflict")
	ErrProfilePublishSelfApproval     = errors.New("profile publish request cannot be approved by its requester")
)

// ProfilePublishApprovalRecord binds a decision to the exact immutable draft
// payload and revision that were reviewed. Applying is lease protected so a
// crashed publisher can be resumed without allowing concurrent publication.
type ProfilePublishApprovalRecord struct {
	ID                      primitive.ObjectID       `bson:"_id,omitempty" json:"id"`
	RequestKey              string                   `bson:"request_key" json:"request_key"`
	ProfileID               string                   `bson:"profile_id" json:"profile_id"`
	Version                 string                   `bson:"version" json:"version"`
	SnapshotHash            string                   `bson:"snapshot_hash" json:"snapshot_hash"`
	ExpectedVersionRevision int64                    `bson:"expected_version_revision" json:"expected_version_revision"`
	Status                  string                   `bson:"status" json:"status"`
	Decision                string                   `bson:"decision,omitempty" json:"decision,omitempty"`
	Reason                  string                   `bson:"reason,omitempty" json:"reason,omitempty"`
	Revision                int64                    `bson:"revision" json:"revision"`
	RequestedBy             uint64                   `bson:"requested_by" json:"requested_by"`
	DecidedBy               uint64                   `bson:"decided_by,omitempty" json:"decided_by,omitempty"`
	ApplyingBy              uint64                   `bson:"applying_by,omitempty" json:"applying_by,omitempty"`
	ErrorCode               string                   `bson:"error_code,omitempty" json:"error_code,omitempty"`
	RequestedAt             time.Time                `bson:"requested_at" json:"requested_at"`
	DecidedAt               time.Time                `bson:"decided_at,omitempty" json:"decided_at,omitempty"`
	ApplyLeaseUntil         time.Time                `bson:"apply_lease_until,omitempty" json:"apply_lease_until,omitempty"`
	AppliedAt               time.Time                `bson:"applied_at,omitempty" json:"applied_at,omitempty"`
	UpdatedAt               time.Time                `bson:"updated_at" json:"updated_at"`
	QualityEvidence         *profile.QualityEvidence `bson:"quality_evidence,omitempty" json:"quality_evidence,omitempty"`
}

type ProfilePublishApprovalRepository interface {
	CreateProfilePublishApproval(ctx context.Context, approval *ProfilePublishApprovalRecord) error
	GetProfilePublishApproval(ctx context.Context, approvalID string) (*ProfilePublishApprovalRecord, error)
	ListProfilePublishApprovals(ctx context.Context, profileID, status string, page, pageSize int) ([]*ProfilePublishApprovalRecord, int64, error)
	DecideProfilePublishApproval(ctx context.Context, approvalID string, expectedRevision int64, actorUserID uint64, decision, reason string, lease time.Duration) (*ProfilePublishApprovalRecord, error)
	ClaimProfilePublishApprovalRetry(ctx context.Context, approvalID string, expectedRevision int64, actorUserID uint64, lease time.Duration) (*ProfilePublishApprovalRecord, error)
	CompleteProfilePublishApproval(ctx context.Context, approvalID string, expectedRevision int64, applied bool, errorCode string) (*ProfilePublishApprovalRecord, error)
}

func (r *MongoProfileRepository) ensureProfilePublishApprovalIndexes(ctx context.Context) error {
	if err := r.requireProfileRepository(ctx, r.approvalColl); err != nil {
		return err
	}
	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "request_key", Value: 1}},
			Options: options.Index().SetName("uniq_profile_publish_request").SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "status", Value: 1}, {Key: "requested_at", Value: -1}, {Key: "_id", Value: -1}},
			Options: options.Index().SetName("idx_profile_publish_status_requested"),
		},
		{
			Keys:    bson.D{{Key: "profile_id", Value: 1}, {Key: "requested_at", Value: -1}, {Key: "_id", Value: -1}},
			Options: options.Index().SetName("idx_profile_publish_profile_requested"),
		},
	}
	if _, err := r.approvalColl.Indexes().CreateMany(ctx, indexes); err != nil {
		return fmt.Errorf("create profile publish approval indexes failed: %w", err)
	}
	return nil
}

func (r *MongoProfileRepository) CreateProfilePublishApproval(ctx context.Context, approval *ProfilePublishApprovalRecord) error {
	if err := r.requireProfileRepository(ctx, r.approvalColl); err != nil {
		return err
	}
	if err := prepareProfilePublishApproval(approval, time.Now().UTC()); err != nil {
		return err
	}
	if _, err := r.approvalColl.InsertOne(ctx, approval); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrProfilePublishApprovalConflict
		}
		return fmt.Errorf("insert profile publish approval failed: %w", err)
	}
	return nil
}

func (r *MongoProfileRepository) GetProfilePublishApproval(ctx context.Context, approvalID string) (*ProfilePublishApprovalRecord, error) {
	if err := r.requireProfileRepository(ctx, r.approvalColl); err != nil {
		return nil, err
	}
	id, err := primitive.ObjectIDFromHex(strings.TrimSpace(approvalID))
	if err != nil {
		return nil, ErrProfilePublishApprovalNotFound
	}
	var record ProfilePublishApprovalRecord
	if err := r.approvalColl.FindOne(ctx, bson.M{"_id": id}).Decode(&record); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrProfilePublishApprovalNotFound
		}
		return nil, fmt.Errorf("find profile publish approval failed: %w", err)
	}
	return &record, nil
}

func (r *MongoProfileRepository) ListProfilePublishApprovals(
	ctx context.Context,
	profileID, status string,
	page, pageSize int,
) ([]*ProfilePublishApprovalRecord, int64, error) {
	if err := r.requireProfileRepository(ctx, r.approvalColl); err != nil {
		return nil, 0, err
	}
	_, pageSize, skip, err := normalizeProfilePagination(page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	filter := bson.M{}
	if profileID = strings.TrimSpace(profileID); profileID != "" {
		filter["profile_id"] = profileID
	}
	if status = strings.TrimSpace(status); status != "" {
		if !validProfilePublishApprovalStatus(status) {
			return nil, 0, errors.New("invalid profile publish approval status")
		}
		filter["status"] = status
	}
	total, err := r.approvalColl.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("count profile publish approvals failed: %w", err)
	}
	cursor, err := r.approvalColl.Find(ctx, filter, options.Find().
		SetSort(bson.D{{Key: "requested_at", Value: -1}, {Key: "_id", Value: -1}}).
		SetSkip(skip).SetLimit(int64(pageSize)))
	if err != nil {
		return nil, 0, fmt.Errorf("find profile publish approvals failed: %w", err)
	}
	defer cursor.Close(ctx)
	var approvals []*ProfilePublishApprovalRecord
	if err := cursor.All(ctx, &approvals); err != nil {
		return nil, 0, fmt.Errorf("decode profile publish approvals failed: %w", err)
	}
	return approvals, total, nil
}

func (r *MongoProfileRepository) DecideProfilePublishApproval(
	ctx context.Context,
	approvalID string,
	expectedRevision int64,
	actorUserID uint64,
	decision, reason string,
	lease time.Duration,
) (*ProfilePublishApprovalRecord, error) {
	if err := r.requireProfileRepository(ctx, r.approvalColl); err != nil {
		return nil, err
	}
	id, err := profilePublishApprovalObjectID(approvalID)
	if err != nil || expectedRevision < 1 || actorUserID == 0 {
		return nil, errors.New("approval identity, expected revision and actor are required")
	}
	decision = strings.TrimSpace(decision)
	reason = strings.TrimSpace(reason)
	if decision != ProfilePublishDecisionApproved && decision != ProfilePublishDecisionRejected {
		return nil, errors.New("profile publish decision must be approved or rejected")
	}
	if len(reason) > 500 {
		return nil, errors.New("profile publish decision reason is too long")
	}
	lease, err = normalizeProfilePublishApplyLease(lease)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	statusValue := ProfilePublishApprovalStatusRejected
	set := bson.M{
		"status": statusValue, "decision": decision, "reason": reason,
		"decided_by": actorUserID, "decided_at": now, "updated_at": now,
	}
	if decision == ProfilePublishDecisionApproved {
		set["status"] = ProfilePublishApprovalStatusApplying
		set["applying_by"] = actorUserID
		set["apply_lease_until"] = now.Add(lease)
	}
	result, err := r.approvalColl.UpdateOne(ctx, bson.M{
		"_id": id, "revision": expectedRevision, "status": ProfilePublishApprovalStatusPending,
		"requested_by": bson.M{"$ne": actorUserID},
	}, bson.M{"$set": set, "$inc": bson.M{"revision": 1}})
	if err != nil {
		return nil, fmt.Errorf("decide profile publish approval failed: %w", err)
	}
	if result.MatchedCount == 0 {
		return nil, r.profilePublishApprovalMutationError(ctx, id, actorUserID)
	}
	return r.GetProfilePublishApproval(ctx, id.Hex())
}

func (r *MongoProfileRepository) ClaimProfilePublishApprovalRetry(
	ctx context.Context,
	approvalID string,
	expectedRevision int64,
	actorUserID uint64,
	lease time.Duration,
) (*ProfilePublishApprovalRecord, error) {
	if err := r.requireProfileRepository(ctx, r.approvalColl); err != nil {
		return nil, err
	}
	id, err := profilePublishApprovalObjectID(approvalID)
	if err != nil || expectedRevision < 1 || actorUserID == 0 {
		return nil, errors.New("approval identity, expected revision and actor are required")
	}
	lease, err = normalizeProfilePublishApplyLease(lease)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	result, err := r.approvalColl.UpdateOne(ctx, bson.M{
		"_id": id, "revision": expectedRevision, "requested_by": bson.M{"$ne": actorUserID},
		"$or": bson.A{
			bson.M{"status": ProfilePublishApprovalStatusApplyFailed},
			bson.M{"status": ProfilePublishApprovalStatusApplying, "apply_lease_until": bson.M{"$lte": now}},
		},
	}, bson.M{
		"$set": bson.M{
			"status": ProfilePublishApprovalStatusApplying, "applying_by": actorUserID,
			"apply_lease_until": now.Add(lease), "updated_at": now,
		},
		"$unset": bson.M{"error_code": ""},
		"$inc":   bson.M{"revision": 1},
	})
	if err != nil {
		return nil, fmt.Errorf("claim profile publish approval retry failed: %w", err)
	}
	if result.MatchedCount == 0 {
		return nil, r.profilePublishApprovalMutationError(ctx, id, actorUserID)
	}
	return r.GetProfilePublishApproval(ctx, id.Hex())
}

func (r *MongoProfileRepository) CompleteProfilePublishApproval(
	ctx context.Context,
	approvalID string,
	expectedRevision int64,
	applied bool,
	errorCode string,
) (*ProfilePublishApprovalRecord, error) {
	if err := r.requireProfileRepository(ctx, r.approvalColl); err != nil {
		return nil, err
	}
	id, err := profilePublishApprovalObjectID(approvalID)
	if err != nil || expectedRevision < 1 {
		return nil, errors.New("approval identity and expected revision are required")
	}
	errorCode = strings.TrimSpace(errorCode)
	if len(errorCode) > 64 {
		return nil, errors.New("profile publish approval error code is too long")
	}
	if !applied && errorCode == "" {
		return nil, errors.New("profile publish approval failure error code is required")
	}
	now := time.Now().UTC()
	statusValue := ProfilePublishApprovalStatusApplyFailed
	set := bson.M{"status": statusValue, "error_code": errorCode, "updated_at": now}
	if applied {
		set["status"] = ProfilePublishApprovalStatusApplied
		set["applied_at"] = now
		set["error_code"] = ""
	}
	result, err := r.approvalColl.UpdateOne(ctx, bson.M{
		"_id": id, "revision": expectedRevision, "status": ProfilePublishApprovalStatusApplying,
	}, bson.M{
		"$set": set, "$unset": bson.M{"apply_lease_until": ""}, "$inc": bson.M{"revision": 1},
	})
	if err != nil {
		return nil, fmt.Errorf("complete profile publish approval failed: %w", err)
	}
	if result.MatchedCount == 0 {
		return nil, ErrProfilePublishApprovalConflict
	}
	return r.GetProfilePublishApproval(ctx, id.Hex())
}

func prepareProfilePublishApproval(approval *ProfilePublishApprovalRecord, now time.Time) error {
	if approval == nil {
		return errors.New("profile publish approval is required")
	}
	approval.ProfileID = strings.TrimSpace(approval.ProfileID)
	approval.Version = strings.TrimSpace(approval.Version)
	approval.SnapshotHash = strings.TrimSpace(approval.SnapshotHash)
	if approval.ProfileID == "" || approval.Version == "" || approval.SnapshotHash == "" ||
		len(approval.ProfileID) > maxProfileIdentityLength || len(approval.Version) > maxProfileIdentityLength ||
		approval.ExpectedVersionRevision < 1 || approval.RequestedBy == 0 {
		return errors.New("profile publish approval target, revision and requester are required")
	}
	if len(approval.SnapshotHash) != sha256.Size*2 {
		return errors.New("profile publish approval snapshot hash is invalid")
	}
	if _, err := hex.DecodeString(approval.SnapshotHash); err != nil {
		return errors.New("profile publish approval snapshot hash is invalid")
	}
	evidenceIdentity := ""
	if approval.QualityEvidence != nil {
		approval.QualityEvidence.Reference = profile.NormalizeQualityEvidenceReference(approval.QualityEvidence.Reference)
		if err := profile.ValidateQualityEvidence(*approval.QualityEvidence, approval.ProfileID, approval.Version, now, true); err != nil {
			return err
		}
		evidenceIdentity = profile.QualityEvidenceIdentity(approval.QualityEvidence.Reference)
	}
	if approval.ID.IsZero() {
		approval.ID = primitive.NewObjectID()
	}
	approval.RequestKey = profilePublishApprovalRequestKey(approval.ProfileID, approval.Version, approval.SnapshotHash, evidenceIdentity)
	approval.Status = ProfilePublishApprovalStatusPending
	approval.Revision = 1
	now = now.UTC()
	approval.RequestedAt = now
	approval.UpdatedAt = now
	approval.Decision = ""
	approval.Reason = ""
	approval.DecidedBy = 0
	approval.ApplyingBy = 0
	approval.ErrorCode = ""
	approval.DecidedAt = time.Time{}
	approval.ApplyLeaseUntil = time.Time{}
	approval.AppliedAt = time.Time{}
	return nil
}

func profilePublishApprovalRequestKey(profileID, version, snapshotHash string, evidenceIdentity ...string) string {
	qualityEvidenceIdentity := ""
	if len(evidenceIdentity) > 0 {
		qualityEvidenceIdentity = strings.TrimSpace(evidenceIdentity[0])
	}
	digest := sha256.Sum256([]byte(strings.TrimSpace(profileID) + "\x00" + strings.TrimSpace(version) + "\x00" + strings.TrimSpace(snapshotHash) + "\x00" + qualityEvidenceIdentity))
	return hex.EncodeToString(digest[:])
}

func profilePublishApprovalObjectID(value string) (primitive.ObjectID, error) {
	id, err := primitive.ObjectIDFromHex(strings.TrimSpace(value))
	if err != nil {
		return primitive.NilObjectID, ErrProfilePublishApprovalNotFound
	}
	return id, nil
}

func validProfilePublishApprovalStatus(value string) bool {
	switch value {
	case ProfilePublishApprovalStatusPending,
		ProfilePublishApprovalStatusApplying,
		ProfilePublishApprovalStatusApplied,
		ProfilePublishApprovalStatusRejected,
		ProfilePublishApprovalStatusApplyFailed:
		return true
	default:
		return false
	}
}

func normalizeProfilePublishApplyLease(lease time.Duration) (time.Duration, error) {
	if lease <= 0 {
		return defaultProfilePublishApplyLease, nil
	}
	if lease > maxProfilePublishApplyLease {
		return 0, fmt.Errorf("profile publish apply lease exceeds %s", maxProfilePublishApplyLease)
	}
	return lease, nil
}

func (r *MongoProfileRepository) profilePublishApprovalMutationError(ctx context.Context, id primitive.ObjectID, actorUserID uint64) error {
	var record ProfilePublishApprovalRecord
	if err := r.approvalColl.FindOne(ctx, bson.M{"_id": id}).Decode(&record); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return ErrProfilePublishApprovalNotFound
		}
		return fmt.Errorf("verify profile publish approval mutation failed: %w", err)
	}
	if actorUserID != 0 && record.RequestedBy == actorUserID {
		return ErrProfilePublishSelfApproval
	}
	return ErrProfilePublishApprovalConflict
}
