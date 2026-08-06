package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"twitter-clone/internal/module/agent/profile"
)

const (
	CollectionProfileVersions         = "agent_profile_versions"
	CollectionProfileReleases         = "agent_profile_releases"
	CollectionProfileAudits           = "agent_profile_audit_events"
	CollectionProfilePublishApprovals = "agent_profile_publish_approvals"
	CollectionProfileExperiments      = "agent_profile_experiments"
	CollectionProfileExperimentRuns   = "agent_profile_experiment_observations"

	ProfileVersionStatusDraft     = "draft"
	ProfileVersionStatusPublished = "published"
	ProfileSnapshotSchemaV1       = "agent_profile_v1"

	maxProfileIdentityLength = 128
	maxProfileSnapshotBytes   = 1 << 20
	maxProfileReleaseSalt     = 128
)

var (
	ErrProfileVersionNotFound = errors.New("profile version not found")
	ErrProfileVersionConflict = errors.New("profile version conflict")
	ErrProfileReleaseNotFound = errors.New("profile release not found")
	ErrProfileReleaseConflict = errors.New("profile release revision conflict")
	ErrProfileRepositoryUnavailable = errors.New("profile repository is unavailable")
)

const (
	ProfileAuditActionCreateDraft        = "create_draft"
	ProfileAuditActionPublishVersion     = "publish_version"
	ProfileAuditActionRequestPublish     = "request_publish_approval"
	ProfileAuditActionDecidePublish      = "decide_publish_approval"
	ProfileAuditActionRetryPublish       = "retry_publish_approval"
	ProfileAuditActionUpsertRelease      = "upsert_release"
	ProfileAuditActionStartExperiment    = "start_experiment"
	ProfileAuditActionEvaluateExperiment = "evaluate_experiment"
	ProfileAuditActionStopExperiment     = "stop_experiment"

	ProfileAuditOutcomeRequested         = "requested"
	ProfileAuditOutcomeSucceeded         = "succeeded"
	ProfileAuditOutcomeFailed            = "failed"
	ProfileAuditOutcomeActivationFailed  = "activation_failed"
	ProfileAuditOutcomePropagationFailed = "propagation_failed"
)

// ProfileVersionRecord stores one immutable AgentProfile payload. Publishing
// only changes lifecycle metadata; SnapshotJSON and SnapshotHash are never
// updated after insertion.
type ProfileVersionRecord struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	ProfileID      string             `bson:"profile_id" json:"profile_id"`
	Version        string             `bson:"version" json:"version"`
	Status         string             `bson:"status" json:"status"`
	SnapshotSchema string             `bson:"snapshot_schema" json:"snapshot_schema"`
	SnapshotJSON   string             `bson:"snapshot_json" json:"snapshot_json"`
	SnapshotHash   string             `bson:"snapshot_hash" json:"snapshot_hash"`
	Revision       int64              `bson:"revision" json:"revision"`
	CreatedBy      uint64             `bson:"created_by" json:"created_by"`
	PublishedBy    uint64             `bson:"published_by,omitempty" json:"published_by,omitempty"`
	CreatedAt      time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt      time.Time          `bson:"updated_at" json:"updated_at"`
	PublishedAt    time.Time          `bson:"published_at,omitempty" json:"published_at,omitempty"`
}

// ProfileReleaseRecord is the current mutable pointer for one Profile. It is
// updated with optimistic concurrency and converted to profile.Release only
// after all referenced published versions have been loaded.
type ProfileReleaseRecord struct {
	ID                   primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	ProfileID            string             `bson:"profile_id" json:"profile_id"`
	StableVersion        string             `bson:"stable_version" json:"stable_version"`
	CandidateVersion     string             `bson:"candidate_version" json:"candidate_version"`
	CandidateBasisPoints int                `bson:"candidate_basis_points" json:"candidate_basis_points"`
	Salt                 string             `bson:"salt,omitempty" json:"salt,omitempty"`
	Revision             int64              `bson:"revision" json:"revision"`
	CreatedBy            uint64             `bson:"created_by" json:"created_by"`
	UpdatedBy            uint64             `bson:"updated_by" json:"updated_by"`
	CreatedAt            time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt            time.Time          `bson:"updated_at" json:"updated_at"`
}

// ProfileAuditEvent is append-only and intentionally excludes snapshot JSON,
// prompts, provider credentials and request bodies.
type ProfileAuditEvent struct {
	ID              primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	OperationID     string             `bson:"operation_id" json:"operation_id"`
	Action          string             `bson:"action" json:"action"`
	Outcome         string             `bson:"outcome" json:"outcome"`
	ProfileID       string             `bson:"profile_id" json:"profile_id"`
	Version         string             `bson:"version,omitempty" json:"version,omitempty"`
	ApprovalID      string             `bson:"approval_id,omitempty" json:"approval_id,omitempty"`
	ExperimentID    string             `bson:"experiment_id,omitempty" json:"experiment_id,omitempty"`
	ActorUserID     uint64             `bson:"actor_user_id" json:"actor_user_id"`
	VersionRevision int64              `bson:"version_revision,omitempty" json:"version_revision,omitempty"`
	ReleaseRevision int64              `bson:"release_revision,omitempty" json:"release_revision,omitempty"`
	SnapshotHash    string             `bson:"snapshot_hash,omitempty" json:"snapshot_hash,omitempty"`
	ErrorCode       string             `bson:"error_code,omitempty" json:"error_code,omitempty"`
	CreatedAt       time.Time          `bson:"created_at" json:"created_at"`
}

type ProfileCatalogSnapshot struct {
	Versions []*ProfileVersionRecord
	Releases []*ProfileReleaseRecord
}

// ProfileCatalogRepository is independent from AgentRepository so dialogue
// test doubles and the main conversation persistence boundary stay unchanged.
type ProfileCatalogRepository interface {
	CreateProfileVersion(ctx context.Context, version *ProfileVersionRecord) error
	PublishProfileVersion(ctx context.Context, profileID, version string, expectedRevision int64, publishedBy uint64) error
	GetProfileVersion(ctx context.Context, profileID, version string) (*ProfileVersionRecord, error)
	ListProfileVersions(ctx context.Context, profileID string, page, pageSize int) ([]*ProfileVersionRecord, int64, error)
	GetProfileRelease(ctx context.Context, profileID string) (*ProfileReleaseRecord, error)
	UpsertProfileRelease(ctx context.Context, release *ProfileReleaseRecord, expectedRevision int64) error
	LoadPublishedProfileCatalog(ctx context.Context) (*ProfileCatalogSnapshot, error)
}

type ProfileCatalogAuditRepository interface {
	AppendProfileAuditEvent(ctx context.Context, event *ProfileAuditEvent) error
	ListProfileAuditEvents(ctx context.Context, profileID string, page, pageSize int) ([]*ProfileAuditEvent, int64, error)
}

type ProfileCatalogStore interface {
	ProfileCatalogRepository
	ProfileCatalogAuditRepository
}

type MongoProfileRepository struct {
	versionColl       *mongo.Collection
	releaseColl       *mongo.Collection
	auditColl         *mongo.Collection
	approvalColl      *mongo.Collection
	roleBindingColl   *mongo.Collection
	roleAuditColl     *mongo.Collection
	experimentColl    *mongo.Collection
	experimentRunColl *mongo.Collection
}

func NewMongoProfileRepository(db *mongo.Database) *MongoProfileRepository {
	if db == nil {
		return &MongoProfileRepository{}
	}
	return &MongoProfileRepository{
		versionColl:       db.Collection(CollectionProfileVersions),
		releaseColl:       db.Collection(CollectionProfileReleases),
		auditColl:         db.Collection(CollectionProfileAudits),
		approvalColl:      db.Collection(CollectionProfilePublishApprovals),
		roleBindingColl:   db.Collection(CollectionProfileRoleBindings),
		roleAuditColl:     db.Collection(CollectionProfileRoleAudits),
		experimentColl:    db.Collection(CollectionProfileExperiments),
		experimentRunColl: db.Collection(CollectionProfileExperimentRuns),
	}
}

func (r *MongoProfileRepository) requireProfileRepository(ctx context.Context, collections ...*mongo.Collection) error {
	if ctx == nil {
		return errors.New("profile repository context is required")
	}
	if r == nil {
		return ErrProfileRepositoryUnavailable
	}
	for _, collection := range collections {
		if collection == nil {
			return ErrProfileRepositoryUnavailable
		}
	}
	return nil
}

func (r *MongoProfileRepository) EnsureIndexes(ctx context.Context) error {
	if err := r.requireProfileRepository(ctx,
		r.versionColl, r.releaseColl, r.auditColl, r.approvalColl,
		r.roleBindingColl, r.roleAuditColl, r.experimentColl, r.experimentRunColl,
	); err != nil {
		return err
	}
	versionIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "profile_id", Value: 1}, {Key: "version", Value: 1}},
			Options: options.Index().SetName("uniq_profile_version").SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "status", Value: 1}, {Key: "profile_id", Value: 1}, {Key: "created_at", Value: -1}},
			Options: options.Index().SetName("idx_profile_version_status_created"),
		},
	}
	if _, err := r.versionColl.Indexes().CreateMany(ctx, versionIndexes); err != nil {
		return fmt.Errorf("create profile version indexes failed: %w", err)
	}
	releaseIndexes := []mongo.IndexModel{{
		Keys:    bson.D{{Key: "profile_id", Value: 1}},
		Options: options.Index().SetName("uniq_profile_release").SetUnique(true),
	}}
	if _, err := r.releaseColl.Indexes().CreateMany(ctx, releaseIndexes); err != nil {
		return fmt.Errorf("create profile release indexes failed: %w", err)
	}
	auditIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "operation_id", Value: 1}, {Key: "created_at", Value: 1}},
			Options: options.Index().SetName("idx_profile_audit_operation_created"),
		},
		{
			Keys:    bson.D{{Key: "profile_id", Value: 1}, {Key: "created_at", Value: -1}, {Key: "_id", Value: -1}},
			Options: options.Index().SetName("idx_profile_audit_profile_created"),
		},
	}
	if _, err := r.auditColl.Indexes().CreateMany(ctx, auditIndexes); err != nil {
		return fmt.Errorf("create profile audit indexes failed: %w", err)
	}
	if err := r.ensureProfilePublishApprovalIndexes(ctx); err != nil {
		return err
	}
	if err := r.ensureProfileRoleBindingIndexes(ctx); err != nil {
		return err
	}
	if err := r.ensureProfileExperimentIndexes(ctx); err != nil {
		return err
	}
	return nil
}

func (r *MongoProfileRepository) CreateProfileVersion(ctx context.Context, version *ProfileVersionRecord) error {
	if err := r.requireProfileRepository(ctx, r.versionColl); err != nil {
		return err
	}
	if err := prepareProfileVersionForCreate(version, time.Now().UTC()); err != nil {
		return err
	}
	if _, err := r.versionColl.InsertOne(ctx, version); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrProfileVersionConflict
		}
		return fmt.Errorf("insert profile version failed: %w", err)
	}
	return nil
}

func (r *MongoProfileRepository) PublishProfileVersion(
	ctx context.Context,
	profileID, version string,
	expectedRevision int64,
	publishedBy uint64,
) error {
	if err := r.requireProfileRepository(ctx, r.versionColl); err != nil {
		return err
	}
	profileID = strings.TrimSpace(profileID)
	version = strings.TrimSpace(version)
	if profileID == "" || version == "" || len(profileID) > maxProfileIdentityLength || len(version) > maxProfileIdentityLength || expectedRevision < 1 || publishedBy == 0 {
		return errors.New("profile identity, expected revision and publisher are required")
	}
	now := time.Now().UTC()
	result, err := r.versionColl.UpdateOne(ctx, bson.M{
		"profile_id": profileID, "version": version,
		"status": ProfileVersionStatusDraft, "revision": expectedRevision,
	}, bson.M{
		"$set": bson.M{
			"status": ProfileVersionStatusPublished, "published_by": publishedBy,
			"published_at": now, "updated_at": now,
		},
		"$inc": bson.M{"revision": 1},
	})
	if err != nil {
		return fmt.Errorf("publish profile version failed: %w", err)
	}
	if result.MatchedCount == 0 {
		return r.profileVersionMutationError(ctx, profileID, version)
	}
	return nil
}

func (r *MongoProfileRepository) GetProfileVersion(ctx context.Context, profileID, version string) (*ProfileVersionRecord, error) {
	if err := r.requireProfileRepository(ctx, r.versionColl); err != nil {
		return nil, err
	}
	var record ProfileVersionRecord
	err := r.versionColl.FindOne(ctx, bson.M{
		"profile_id": strings.TrimSpace(profileID), "version": strings.TrimSpace(version),
	}).Decode(&record)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrProfileVersionNotFound
		}
		return nil, fmt.Errorf("find profile version failed: %w", err)
	}
	return &record, nil
}

func (r *MongoProfileRepository) ListProfileVersions(
	ctx context.Context,
	profileID string,
	page, pageSize int,
) ([]*ProfileVersionRecord, int64, error) {
	if err := r.requireProfileRepository(ctx, r.versionColl); err != nil {
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
	total, err := r.versionColl.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("count profile versions failed: %w", err)
	}
	cursor, err := r.versionColl.Find(ctx, filter, options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: -1}}).
		SetSkip(skip).SetLimit(int64(pageSize)))
	if err != nil {
		return nil, 0, fmt.Errorf("find profile versions failed: %w", err)
	}
	defer cursor.Close(ctx)
	var versions []*ProfileVersionRecord
	if err := cursor.All(ctx, &versions); err != nil {
		return nil, 0, fmt.Errorf("decode profile versions failed: %w", err)
	}
	return versions, total, nil
}

func (r *MongoProfileRepository) UpsertProfileRelease(
	ctx context.Context,
	release *ProfileReleaseRecord,
	expectedRevision int64,
) error {
	if err := r.requireProfileRepository(ctx, r.releaseColl); err != nil {
		return err
	}
	if err := prepareProfileRelease(release, expectedRevision, time.Now().UTC()); err != nil {
		return err
	}
	if expectedRevision == 0 {
		if _, err := r.releaseColl.InsertOne(ctx, release); err != nil {
			if mongo.IsDuplicateKeyError(err) {
				return ErrProfileReleaseConflict
			}
			return fmt.Errorf("insert profile release failed: %w", err)
		}
		return nil
	}
	now := release.UpdatedAt
	result, err := r.releaseColl.UpdateOne(ctx, bson.M{
		"profile_id": release.ProfileID, "revision": expectedRevision,
	}, bson.M{
		"$set": bson.M{
			"stable_version": release.StableVersion, "candidate_version": release.CandidateVersion,
			"candidate_basis_points": release.CandidateBasisPoints, "salt": release.Salt,
			"updated_by": release.UpdatedBy, "updated_at": now,
		},
		"$inc": bson.M{"revision": 1},
	})
	if err != nil {
		return fmt.Errorf("update profile release failed: %w", err)
	}
	if result.MatchedCount == 0 {
		return ErrProfileReleaseConflict
	}
	release.Revision = expectedRevision + 1
	return nil
}

func (r *MongoProfileRepository) GetProfileRelease(ctx context.Context, profileID string) (*ProfileReleaseRecord, error) {
	if err := r.requireProfileRepository(ctx, r.releaseColl); err != nil {
		return nil, err
	}
	var record ProfileReleaseRecord
	err := r.releaseColl.FindOne(ctx, bson.M{"profile_id": strings.TrimSpace(profileID)}).Decode(&record)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrProfileReleaseNotFound
		}
		return nil, fmt.Errorf("find profile release failed: %w", err)
	}
	return &record, nil
}

func (r *MongoProfileRepository) AppendProfileAuditEvent(ctx context.Context, event *ProfileAuditEvent) error {
	if err := r.requireProfileRepository(ctx, r.auditColl); err != nil {
		return err
	}
	if err := prepareProfileAuditEvent(event, time.Now().UTC()); err != nil {
		return err
	}
	if _, err := r.auditColl.InsertOne(ctx, event); err != nil {
		return fmt.Errorf("insert profile audit event failed: %w", err)
	}
	return nil
}

func (r *MongoProfileRepository) ListProfileAuditEvents(
	ctx context.Context,
	profileID string,
	page, pageSize int,
) ([]*ProfileAuditEvent, int64, error) {
	if err := r.requireProfileRepository(ctx, r.auditColl); err != nil {
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
	total, err := r.auditColl.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("count profile audit events failed: %w", err)
	}
	cursor, err := r.auditColl.Find(ctx, filter, options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: -1}}).
		SetSkip(skip).SetLimit(int64(pageSize)))
	if err != nil {
		return nil, 0, fmt.Errorf("find profile audit events failed: %w", err)
	}
	defer cursor.Close(ctx)
	var events []*ProfileAuditEvent
	if err := cursor.All(ctx, &events); err != nil {
		return nil, 0, fmt.Errorf("decode profile audit events failed: %w", err)
	}
	return events, total, nil
}

func (r *MongoProfileRepository) LoadPublishedProfileCatalog(ctx context.Context) (*ProfileCatalogSnapshot, error) {
	if err := r.requireProfileRepository(ctx, r.versionColl, r.releaseColl); err != nil {
		return nil, err
	}
	versionCursor, err := r.versionColl.Find(ctx, bson.M{"status": ProfileVersionStatusPublished},
		options.Find().SetSort(bson.D{{Key: "profile_id", Value: 1}, {Key: "version", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("find published profile versions failed: %w", err)
	}
	defer versionCursor.Close(ctx)
	var versions []*ProfileVersionRecord
	if err := versionCursor.All(ctx, &versions); err != nil {
		return nil, fmt.Errorf("decode published profile versions failed: %w", err)
	}

	releaseCursor, err := r.releaseColl.Find(ctx, bson.M{},
		options.Find().SetSort(bson.D{{Key: "profile_id", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("find profile releases failed: %w", err)
	}
	defer releaseCursor.Close(ctx)
	var releases []*ProfileReleaseRecord
	if err := releaseCursor.All(ctx, &releases); err != nil {
		return nil, fmt.Errorf("decode profile releases failed: %w", err)
	}
	return &ProfileCatalogSnapshot{Versions: versions, Releases: releases}, nil
}

func prepareProfileVersionForCreate(version *ProfileVersionRecord, now time.Time) error {
	if version == nil {
		return errors.New("profile version is required")
	}
	version.ProfileID = strings.TrimSpace(version.ProfileID)
	version.Version = strings.TrimSpace(version.Version)
	if version.ProfileID == "" || version.Version == "" || len(version.ProfileID) > maxProfileIdentityLength || len(version.Version) > maxProfileIdentityLength || version.CreatedBy == 0 {
		return errors.New("profile identity and creator are required")
	}
	if len(version.SnapshotJSON) > maxProfileSnapshotBytes {
		return fmt.Errorf("profile snapshot exceeds %d bytes", maxProfileSnapshotBytes)
	}
	if !json.Valid([]byte(version.SnapshotJSON)) {
		return errors.New("profile snapshot must be valid JSON")
	}
	if version.Status != "" && version.Status != ProfileVersionStatusDraft {
		return errors.New("new profile version must start as draft")
	}
	version.Status = ProfileVersionStatusDraft
	version.SnapshotSchema = ProfileSnapshotSchemaV1
	version.SnapshotHash = digestProfileSnapshot(version.SnapshotJSON)
	version.Revision = 1
	if version.ID.IsZero() {
		version.ID = primitive.NewObjectID()
	}
	now = now.UTC()
	version.CreatedAt = now
	version.UpdatedAt = now
	version.PublishedBy = 0
	version.PublishedAt = time.Time{}
	return nil
}

func prepareProfileRelease(release *ProfileReleaseRecord, expectedRevision int64, now time.Time) error {
	if release == nil {
		return errors.New("profile release is required")
	}
	release.ProfileID = strings.TrimSpace(release.ProfileID)
	release.StableVersion = strings.TrimSpace(release.StableVersion)
	release.CandidateVersion = strings.TrimSpace(release.CandidateVersion)
	release.Salt = strings.TrimSpace(release.Salt)
	if release.ProfileID == "" || release.StableVersion == "" || release.CandidateVersion == "" ||
		len(release.ProfileID) > maxProfileIdentityLength || len(release.StableVersion) > maxProfileIdentityLength ||
		len(release.CandidateVersion) > maxProfileIdentityLength || len(release.Salt) > maxProfileReleaseSalt || release.UpdatedBy == 0 {
		return errors.New("profile release identity, versions and updater are required")
	}
	if release.StableVersion == release.CandidateVersion {
		return errors.New("stable and candidate profile versions must differ")
	}
	if release.CandidateBasisPoints < 0 || release.CandidateBasisPoints > profile.MaxReleaseBasisPoints {
		return fmt.Errorf("candidate basis points must be within 0..%d", profile.MaxReleaseBasisPoints)
	}
	if release.CandidateBasisPoints > 0 && release.CandidateBasisPoints < profile.MaxReleaseBasisPoints && release.Salt == "" {
		return errors.New("profile release salt is required for a partial rollout")
	}
	if expectedRevision < 0 {
		return errors.New("profile release expected revision cannot be negative")
	}
	now = now.UTC()
	release.UpdatedAt = now
	if expectedRevision == 0 {
		if release.ID.IsZero() {
			release.ID = primitive.NewObjectID()
		}
		release.Revision = 1
		release.CreatedAt = now
		if release.CreatedBy == 0 {
			release.CreatedBy = release.UpdatedBy
		}
	} else {
		release.Revision = expectedRevision
	}
	return nil
}

func prepareProfileAuditEvent(event *ProfileAuditEvent, now time.Time) error {
	if event == nil {
		return errors.New("profile audit event is required")
	}
	event.OperationID = strings.TrimSpace(event.OperationID)
	event.Action = strings.TrimSpace(event.Action)
	event.Outcome = strings.TrimSpace(event.Outcome)
	event.ProfileID = strings.TrimSpace(event.ProfileID)
	event.Version = strings.TrimSpace(event.Version)
	event.ApprovalID = strings.TrimSpace(event.ApprovalID)
	event.ExperimentID = strings.TrimSpace(event.ExperimentID)
	event.SnapshotHash = strings.TrimSpace(event.SnapshotHash)
	event.ErrorCode = strings.TrimSpace(event.ErrorCode)
	if event.OperationID == "" || event.Action == "" || event.Outcome == "" || event.ProfileID == "" || event.ActorUserID == 0 ||
		len(event.OperationID) > maxProfileIdentityLength || len(event.ProfileID) > maxProfileIdentityLength ||
		len(event.Version) > maxProfileIdentityLength || len(event.ApprovalID) > maxProfileIdentityLength ||
		len(event.ExperimentID) > maxProfileIdentityLength {
		return errors.New("profile audit identity, action, outcome and actor are required")
	}
	if event.VersionRevision < 0 || event.ReleaseRevision < 0 {
		return errors.New("profile audit revisions cannot be negative")
	}
	if len(event.ErrorCode) > 64 {
		return errors.New("profile audit error code is too long")
	}
	if event.ID.IsZero() {
		event.ID = primitive.NewObjectID()
	}
	event.CreatedAt = now.UTC()
	return nil
}

func normalizeProfilePagination(page, pageSize int) (int, int, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	pageIndex := int64(page - 1)
	if pageIndex > math.MaxInt64/int64(pageSize) {
		return 0, 0, 0, errors.New("profile pagination offset is too large")
	}
	return page, pageSize, pageIndex * int64(pageSize), nil
}

func digestProfileSnapshot(snapshot string) string {
	digest := sha256.Sum256([]byte(snapshot))
	return hex.EncodeToString(digest[:])
}

// ProfileSnapshotHash returns the content digest stored alongside an immutable
// profile snapshot. It is exported so the application layer can reject a
// corrupted catalog before atomically activating it.
func ProfileSnapshotHash(snapshot string) string {
	return digestProfileSnapshot(snapshot)
}

// VerifySnapshot checks the persisted payload without interpreting its schema.
// Schema decoding stays in the profile application service.
func (v *ProfileVersionRecord) VerifySnapshot() error {
	if v == nil {
		return errors.New("profile version is required")
	}
	if !json.Valid([]byte(v.SnapshotJSON)) {
		return errors.New("profile snapshot must be valid JSON")
	}
	if strings.TrimSpace(v.SnapshotHash) == "" {
		return errors.New("profile snapshot hash is required")
	}
	if digestProfileSnapshot(v.SnapshotJSON) != strings.TrimSpace(v.SnapshotHash) {
		return errors.New("profile snapshot hash mismatch")
	}
	return nil
}

func (r *MongoProfileRepository) profileVersionMutationError(ctx context.Context, profileID, version string) error {
	count, err := r.versionColl.CountDocuments(ctx, bson.M{"profile_id": profileID, "version": version})
	if err != nil {
		return fmt.Errorf("verify profile version mutation failed: %w", err)
	}
	if count == 0 {
		return ErrProfileVersionNotFound
	}
	return ErrProfileVersionConflict
}
