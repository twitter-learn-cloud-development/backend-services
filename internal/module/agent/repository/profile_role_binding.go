package repository

import (
	"context"
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
	CollectionProfileRoleBindings = "agent_profile_role_bindings"
	CollectionProfileRoleAudits   = "agent_profile_role_audit_events"

	ProfileRoleAuditActionUpsert = "upsert_profile_role_binding"
	ProfileRoleAuditActionDelete = "delete_profile_role_binding"
)

var (
	ErrProfileRoleBindingNotFound = errors.New("profile role binding not found")
	ErrProfileRoleBindingConflict = errors.New("profile role binding revision conflict")
)

type ProfileRoleBindingRecord struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID    uint64             `bson:"user_id" json:"user_id"`
	Roles     []string           `bson:"roles" json:"roles"`
	Revision  int64              `bson:"revision" json:"revision"`
	CreatedBy uint64             `bson:"created_by" json:"created_by"`
	UpdatedBy uint64             `bson:"updated_by" json:"updated_by"`
	CreatedAt time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time          `bson:"updated_at" json:"updated_at"`
}

type ProfileRoleAuditEvent struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	OperationID   string             `bson:"operation_id" json:"operation_id"`
	Action        string             `bson:"action" json:"action"`
	Outcome       string             `bson:"outcome" json:"outcome"`
	ActorUserID   uint64             `bson:"actor_user_id" json:"actor_user_id"`
	SubjectUserID uint64             `bson:"subject_user_id" json:"subject_user_id"`
	Roles         []string           `bson:"roles,omitempty" json:"roles,omitempty"`
	Revision      int64              `bson:"revision,omitempty" json:"revision,omitempty"`
	ErrorCode     string             `bson:"error_code,omitempty" json:"error_code,omitempty"`
	CreatedAt     time.Time          `bson:"created_at" json:"created_at"`
}

type ProfileRoleBindingRepository interface {
	GetProfileRoleBinding(ctx context.Context, userID uint64) (*ProfileRoleBindingRecord, error)
	ListProfileRoleBindings(ctx context.Context, page, pageSize int) ([]*ProfileRoleBindingRecord, int64, error)
	UpsertProfileRoleBinding(ctx context.Context, binding *ProfileRoleBindingRecord, expectedRevision int64) error
	DeleteProfileRoleBinding(ctx context.Context, userID uint64, expectedRevision int64) error
	AppendProfileRoleAuditEvent(ctx context.Context, event *ProfileRoleAuditEvent) error
	ListProfileRoleAuditEvents(ctx context.Context, page, pageSize int) ([]*ProfileRoleAuditEvent, int64, error)
}

func (r *MongoProfileRepository) ensureProfileRoleBindingIndexes(ctx context.Context) error {
	if err := r.requireProfileRepository(ctx, r.roleBindingColl, r.roleAuditColl); err != nil {
		return err
	}
	bindingIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}},
			Options: options.Index().SetName("uniq_profile_role_user").SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "updated_at", Value: -1}, {Key: "_id", Value: -1}},
			Options: options.Index().SetName("idx_profile_role_updated"),
		},
	}
	if _, err := r.roleBindingColl.Indexes().CreateMany(ctx, bindingIndexes); err != nil {
		return fmt.Errorf("create profile role binding indexes failed: %w", err)
	}
	auditIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "subject_user_id", Value: 1}, {Key: "created_at", Value: -1}, {Key: "_id", Value: -1}},
			Options: options.Index().SetName("idx_profile_role_audit_subject_created"),
		},
		{
			Keys:    bson.D{{Key: "operation_id", Value: 1}, {Key: "created_at", Value: 1}},
			Options: options.Index().SetName("idx_profile_role_audit_operation_created"),
		},
	}
	if _, err := r.roleAuditColl.Indexes().CreateMany(ctx, auditIndexes); err != nil {
		return fmt.Errorf("create profile role audit indexes failed: %w", err)
	}
	return nil
}

func (r *MongoProfileRepository) GetProfileRoleBinding(ctx context.Context, userID uint64) (*ProfileRoleBindingRecord, error) {
	if err := r.requireProfileRepository(ctx, r.roleBindingColl); err != nil {
		return nil, err
	}
	if userID == 0 {
		return nil, errors.New("profile role user_id is required")
	}
	var record ProfileRoleBindingRecord
	if err := r.roleBindingColl.FindOne(ctx, bson.M{"user_id": userID}).Decode(&record); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrProfileRoleBindingNotFound
		}
		return nil, fmt.Errorf("find profile role binding failed: %w", err)
	}
	return &record, nil
}

func (r *MongoProfileRepository) ListProfileRoleBindings(ctx context.Context, page, pageSize int) ([]*ProfileRoleBindingRecord, int64, error) {
	if err := r.requireProfileRepository(ctx, r.roleBindingColl); err != nil {
		return nil, 0, err
	}
	_, pageSize, skip, err := normalizeProfilePagination(page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	total, err := r.roleBindingColl.CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, 0, fmt.Errorf("count profile role bindings failed: %w", err)
	}
	cursor, err := r.roleBindingColl.Find(ctx, bson.M{}, options.Find().
		SetSort(bson.D{{Key: "updated_at", Value: -1}, {Key: "_id", Value: -1}}).
		SetSkip(skip).SetLimit(int64(pageSize)))
	if err != nil {
		return nil, 0, fmt.Errorf("find profile role bindings failed: %w", err)
	}
	defer cursor.Close(ctx)
	var records []*ProfileRoleBindingRecord
	if err := cursor.All(ctx, &records); err != nil {
		return nil, 0, fmt.Errorf("decode profile role bindings failed: %w", err)
	}
	return records, total, nil
}

func (r *MongoProfileRepository) UpsertProfileRoleBinding(ctx context.Context, binding *ProfileRoleBindingRecord, expectedRevision int64) error {
	if err := r.requireProfileRepository(ctx, r.roleBindingColl); err != nil {
		return err
	}
	if err := prepareProfileRoleBinding(binding, expectedRevision, time.Now().UTC()); err != nil {
		return err
	}
	if expectedRevision == 0 {
		if _, err := r.roleBindingColl.InsertOne(ctx, binding); err != nil {
			if mongo.IsDuplicateKeyError(err) {
				return ErrProfileRoleBindingConflict
			}
			return fmt.Errorf("insert profile role binding failed: %w", err)
		}
		return nil
	}
	result, err := r.roleBindingColl.UpdateOne(ctx, bson.M{
		"user_id": binding.UserID, "revision": expectedRevision,
	}, bson.M{
		"$set": bson.M{"roles": binding.Roles, "updated_by": binding.UpdatedBy, "updated_at": binding.UpdatedAt},
		"$inc": bson.M{"revision": 1},
	})
	if err != nil {
		return fmt.Errorf("update profile role binding failed: %w", err)
	}
	if result.MatchedCount == 0 {
		return r.profileRoleBindingMutationError(ctx, binding.UserID)
	}
	binding.Revision = expectedRevision + 1
	return nil
}

func (r *MongoProfileRepository) DeleteProfileRoleBinding(ctx context.Context, userID uint64, expectedRevision int64) error {
	if err := r.requireProfileRepository(ctx, r.roleBindingColl); err != nil {
		return err
	}
	if userID == 0 || expectedRevision < 1 {
		return errors.New("profile role user_id and expected revision are required")
	}
	result, err := r.roleBindingColl.DeleteOne(ctx, bson.M{"user_id": userID, "revision": expectedRevision})
	if err != nil {
		return fmt.Errorf("delete profile role binding failed: %w", err)
	}
	if result.DeletedCount == 0 {
		return r.profileRoleBindingMutationError(ctx, userID)
	}
	return nil
}

func (r *MongoProfileRepository) AppendProfileRoleAuditEvent(ctx context.Context, event *ProfileRoleAuditEvent) error {
	if err := r.requireProfileRepository(ctx, r.roleAuditColl); err != nil {
		return err
	}
	if err := prepareProfileRoleAuditEvent(event, time.Now().UTC()); err != nil {
		return err
	}
	if _, err := r.roleAuditColl.InsertOne(ctx, event); err != nil {
		return fmt.Errorf("insert profile role audit event failed: %w", err)
	}
	return nil
}

func (r *MongoProfileRepository) ListProfileRoleAuditEvents(ctx context.Context, page, pageSize int) ([]*ProfileRoleAuditEvent, int64, error) {
	if err := r.requireProfileRepository(ctx, r.roleAuditColl); err != nil {
		return nil, 0, err
	}
	_, pageSize, skip, err := normalizeProfilePagination(page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	total, err := r.roleAuditColl.CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, 0, fmt.Errorf("count profile role audit events failed: %w", err)
	}
	cursor, err := r.roleAuditColl.Find(ctx, bson.M{}, options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: -1}}).
		SetSkip(skip).SetLimit(int64(pageSize)))
	if err != nil {
		return nil, 0, fmt.Errorf("find profile role audit events failed: %w", err)
	}
	defer cursor.Close(ctx)
	var records []*ProfileRoleAuditEvent
	if err := cursor.All(ctx, &records); err != nil {
		return nil, 0, fmt.Errorf("decode profile role audit events failed: %w", err)
	}
	return records, total, nil
}

func prepareProfileRoleBinding(binding *ProfileRoleBindingRecord, expectedRevision int64, now time.Time) error {
	if binding == nil || binding.UserID == 0 || binding.UpdatedBy == 0 || len(binding.Roles) == 0 {
		return errors.New("profile role user, roles and updater are required")
	}
	if expectedRevision < 0 {
		return errors.New("profile role expected revision cannot be negative")
	}
	roles, err := profile.NormalizeManagementRoles(binding.Roles)
	if err != nil {
		return err
	}
	binding.Roles = roles
	now = now.UTC()
	binding.UpdatedAt = now
	if expectedRevision == 0 {
		if binding.ID.IsZero() {
			binding.ID = primitive.NewObjectID()
		}
		binding.Revision = 1
		binding.CreatedBy = binding.UpdatedBy
		binding.CreatedAt = now
	} else {
		binding.Revision = expectedRevision
	}
	return nil
}

func prepareProfileRoleAuditEvent(event *ProfileRoleAuditEvent, now time.Time) error {
	if event == nil {
		return errors.New("profile role audit event is required")
	}
	event.OperationID = strings.TrimSpace(event.OperationID)
	event.Action = strings.TrimSpace(event.Action)
	event.Outcome = strings.TrimSpace(event.Outcome)
	event.ErrorCode = strings.TrimSpace(event.ErrorCode)
	if event.OperationID == "" || event.Action == "" || event.Outcome == "" || event.ActorUserID == 0 || event.SubjectUserID == 0 ||
		len(event.OperationID) > maxProfileIdentityLength {
		return errors.New("profile role audit identity, action, outcome and users are required")
	}
	if event.Action != ProfileRoleAuditActionUpsert && event.Action != ProfileRoleAuditActionDelete {
		return errors.New("profile role audit action is invalid")
	}
	if len(event.Roles) > 0 {
		roles, err := profile.NormalizeManagementRoles(event.Roles)
		if err != nil {
			return err
		}
		event.Roles = roles
	}
	if event.Revision < 0 || len(event.ErrorCode) > 64 {
		return errors.New("profile role audit revision or error code is invalid")
	}
	if event.ID.IsZero() {
		event.ID = primitive.NewObjectID()
	}
	event.CreatedAt = now.UTC()
	return nil
}

func (r *MongoProfileRepository) profileRoleBindingMutationError(ctx context.Context, userID uint64) error {
	count, err := r.roleBindingColl.CountDocuments(ctx, bson.M{"user_id": userID})
	if err != nil {
		return fmt.Errorf("verify profile role binding mutation failed: %w", err)
	}
	if count == 0 {
		return ErrProfileRoleBindingNotFound
	}
	return ErrProfileRoleBindingConflict
}
