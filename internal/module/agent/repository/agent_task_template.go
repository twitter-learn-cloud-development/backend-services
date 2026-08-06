package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	CollectionAgentTaskTemplates = "agent_task_templates"

	AgentTaskTemplateContractVersion = "agent.task_template.v1"
	AgentTaskTemplateActive          = "active"
	AgentTaskTemplateArchived        = "archived"

	MaxAgentTaskTemplateNameRunes        = 80
	MaxAgentTaskTemplateDescriptionRunes = 500
	MaxAgentTaskTemplateInstructionBytes = 12 * 1024
	MaxAgentTaskTemplateIdempotencyBytes = 128
)

var (
	ErrAgentTaskTemplateNotFound = errors.New("agent task template not found")
	ErrAgentTaskTemplateConflict = errors.New("agent task template revision conflict")
)

// AgentTaskTemplate is an explicitly authored, reusable RunAgent preset
// derived from one completed authoritative run. It stores no conversation or
// model output; SourceResultDigest is evidence that the source run completed.
type AgentTaskTemplate struct {
	ID                     primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	ContractVersion        string             `bson:"contract_version" json:"contract_version"`
	UserID                 uint64             `bson:"user_id" json:"user_id"`
	Name                   string             `bson:"name" json:"name"`
	Description            string             `bson:"description,omitempty" json:"description,omitempty"`
	InstructionTemplate    string             `bson:"instruction_template" json:"instruction_template"`
	Status                 string             `bson:"status" json:"status"`
	Revision               int64              `bson:"revision" json:"revision"`
	IdempotencyKey         string             `bson:"idempotency_key" json:"-"`
	SourceRunID            string             `bson:"source_run_id" json:"source_run_id"`
	SourceRunRevision      int64              `bson:"source_run_revision" json:"source_run_revision"`
	SourceResultDigest     string             `bson:"source_result_digest" json:"source_result_digest"`
	SourceExecutionProfile string             `bson:"source_execution_profile" json:"source_execution_profile"`
	CapabilityIDs          []string           `bson:"capability_ids" json:"capability_ids"`
	SkillID                string             `bson:"skill_id,omitempty" json:"skill_id,omitempty"`
	SkillVersion           string             `bson:"skill_version,omitempty" json:"skill_version,omitempty"`
	SourceModel            string             `bson:"source_model,omitempty" json:"source_model,omitempty"`
	AgentProfileID         string             `bson:"agent_profile_id,omitempty" json:"agent_profile_id,omitempty"`
	AgentProfileVersion    string             `bson:"agent_profile_version,omitempty" json:"agent_profile_version,omitempty"`
	PromptTemplateID       string             `bson:"prompt_template_id,omitempty" json:"prompt_template_id,omitempty"`
	PromptTemplateVersion  string             `bson:"prompt_template_version,omitempty" json:"prompt_template_version,omitempty"`
	CreatedAt              time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt              time.Time          `bson:"updated_at" json:"updated_at"`
	ArchivedAt             time.Time          `bson:"archived_at,omitempty" json:"archived_at,omitempty"`
}

// AgentTaskTemplateStore is isolated from AgentRepository so template
// lifecycle changes do not expand every dialogue and workflow repository fake.
type AgentTaskTemplateStore interface {
	CreateAgentTaskTemplate(context.Context, *AgentTaskTemplate) error
	GetAgentTaskTemplate(context.Context, primitive.ObjectID, uint64) (*AgentTaskTemplate, error)
	GetAgentTaskTemplateByIdempotencyKey(context.Context, uint64, string) (*AgentTaskTemplate, error)
	ListActiveAgentTaskTemplates(context.Context, uint64, int) ([]*AgentTaskTemplate, error)
	ArchiveAgentTaskTemplate(
		context.Context,
		primitive.ObjectID,
		uint64,
		int64,
		time.Time,
	) (*AgentTaskTemplate, error)
}

type MongoAgentTaskTemplateRepository struct {
	collection *mongo.Collection
}

func NewMongoAgentTaskTemplateRepository(db *mongo.Database) *MongoAgentTaskTemplateRepository {
	if db == nil {
		return &MongoAgentTaskTemplateRepository{}
	}
	return &MongoAgentTaskTemplateRepository{
		collection: db.Collection(CollectionAgentTaskTemplates),
	}
}

func (r *MongoAgentTaskTemplateRepository) EnsureIndexes(ctx context.Context) error {
	if r == nil || r.collection == nil {
		return errors.New("agent task template repository is unavailable")
	}
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "user_id", Value: 1},
				{Key: "idempotency_key", Value: 1},
			},
			Options: options.Index().
				SetName("uniq_agent_task_template_user_idempotency").
				SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "user_id", Value: 1},
				{Key: "status", Value: 1},
				{Key: "updated_at", Value: -1},
			},
			Options: options.Index().SetName("idx_agent_task_template_user_active"),
		},
		{
			Keys: bson.D{
				{Key: "user_id", Value: 1},
				{Key: "source_run_id", Value: 1},
			},
			Options: options.Index().SetName("idx_agent_task_template_source_run"),
		},
	})
	if err != nil {
		return fmt.Errorf("create agent task template indexes: %w", err)
	}
	return nil
}

func (r *MongoAgentTaskTemplateRepository) CreateAgentTaskTemplate(
	ctx context.Context,
	template *AgentTaskTemplate,
) error {
	if r == nil || r.collection == nil {
		return errors.New("agent task template repository is unavailable")
	}
	if err := normalizeNewAgentTaskTemplate(template, time.Now()); err != nil {
		return err
	}
	if _, err := r.collection.InsertOne(ctx, template); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrAgentTaskTemplateConflict
		}
		return fmt.Errorf("insert agent task template: %w", err)
	}
	return nil
}

func (r *MongoAgentTaskTemplateRepository) GetAgentTaskTemplate(
	ctx context.Context,
	templateID primitive.ObjectID,
	userID uint64,
) (*AgentTaskTemplate, error) {
	if r == nil || r.collection == nil {
		return nil, errors.New("agent task template repository is unavailable")
	}
	return r.findOne(ctx, bson.M{"_id": templateID, "user_id": userID})
}

func (r *MongoAgentTaskTemplateRepository) GetAgentTaskTemplateByIdempotencyKey(
	ctx context.Context,
	userID uint64,
	idempotencyKey string,
) (*AgentTaskTemplate, error) {
	if r == nil || r.collection == nil {
		return nil, errors.New("agent task template repository is unavailable")
	}
	return r.findOne(ctx, bson.M{
		"user_id":         userID,
		"idempotency_key": strings.TrimSpace(idempotencyKey),
	})
}

func (r *MongoAgentTaskTemplateRepository) ListActiveAgentTaskTemplates(
	ctx context.Context,
	userID uint64,
	limit int,
) ([]*AgentTaskTemplate, error) {
	if r == nil || r.collection == nil {
		return nil, errors.New("agent task template repository is unavailable")
	}
	if userID == 0 {
		return nil, errors.New("agent task template user is required")
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	cursor, err := r.collection.Find(
		ctx,
		bson.M{"user_id": userID, "status": AgentTaskTemplateActive},
		options.Find().
			SetSort(bson.D{{Key: "updated_at", Value: -1}, {Key: "_id", Value: -1}}).
			SetLimit(int64(limit)),
	)
	if err != nil {
		return nil, fmt.Errorf("list agent task templates: %w", err)
	}
	defer cursor.Close(ctx)
	var templates []*AgentTaskTemplate
	if err := cursor.All(ctx, &templates); err != nil {
		return nil, fmt.Errorf("decode agent task templates: %w", err)
	}
	return templates, nil
}

func (r *MongoAgentTaskTemplateRepository) ArchiveAgentTaskTemplate(
	ctx context.Context,
	templateID primitive.ObjectID,
	userID uint64,
	expectedRevision int64,
	now time.Time,
) (*AgentTaskTemplate, error) {
	if r == nil || r.collection == nil {
		return nil, errors.New("agent task template repository is unavailable")
	}
	if templateID.IsZero() || userID == 0 || expectedRevision <= 0 {
		return nil, errors.New("agent task template archive identity is incomplete")
	}
	if now.IsZero() {
		now = time.Now()
	}
	var template AgentTaskTemplate
	err := r.collection.FindOneAndUpdate(
		ctx,
		bson.M{
			"_id":      templateID,
			"user_id":  userID,
			"status":   AgentTaskTemplateActive,
			"revision": expectedRevision,
		},
		bson.M{
			"$set": bson.M{
				"status":      AgentTaskTemplateArchived,
				"archived_at": now,
				"updated_at":  now,
			},
			"$inc": bson.M{"revision": 1},
		},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&template)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			if _, getErr := r.GetAgentTaskTemplate(ctx, templateID, userID); getErr != nil {
				return nil, getErr
			}
			return nil, ErrAgentTaskTemplateConflict
		}
		return nil, fmt.Errorf("archive agent task template: %w", err)
	}
	return &template, nil
}

func (r *MongoAgentTaskTemplateRepository) findOne(
	ctx context.Context,
	filter bson.M,
) (*AgentTaskTemplate, error) {
	var template AgentTaskTemplate
	if err := r.collection.FindOne(ctx, filter).Decode(&template); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrAgentTaskTemplateNotFound
		}
		return nil, fmt.Errorf("find agent task template: %w", err)
	}
	return &template, nil
}

func normalizeNewAgentTaskTemplate(template *AgentTaskTemplate, now time.Time) error {
	if template == nil {
		return errors.New("agent task template is required")
	}
	template.ContractVersion = strings.TrimSpace(template.ContractVersion)
	template.Name = strings.TrimSpace(template.Name)
	template.Description = strings.TrimSpace(template.Description)
	template.InstructionTemplate = strings.TrimSpace(template.InstructionTemplate)
	template.IdempotencyKey = strings.TrimSpace(template.IdempotencyKey)
	template.SourceRunID = strings.TrimSpace(template.SourceRunID)
	template.SourceResultDigest = strings.TrimSpace(template.SourceResultDigest)
	template.SourceExecutionProfile = strings.TrimSpace(template.SourceExecutionProfile)
	template.SkillID = strings.TrimSpace(template.SkillID)
	template.SkillVersion = strings.TrimSpace(template.SkillVersion)
	template.SourceModel = strings.TrimSpace(template.SourceModel)
	template.AgentProfileID = strings.TrimSpace(template.AgentProfileID)
	template.AgentProfileVersion = strings.TrimSpace(template.AgentProfileVersion)
	template.PromptTemplateID = strings.TrimSpace(template.PromptTemplateID)
	template.PromptTemplateVersion = strings.TrimSpace(template.PromptTemplateVersion)
	template.CapabilityIDs = normalizedTaskTemplateCapabilities(template.CapabilityIDs)
	if template.ID.IsZero() {
		template.ID = primitive.NewObjectID()
	}
	if template.Status == "" {
		template.Status = AgentTaskTemplateActive
	}
	if template.Revision == 0 {
		template.Revision = 1
	}
	if template.CreatedAt.IsZero() {
		template.CreatedAt = now
	}
	if template.UpdatedAt.IsZero() {
		template.UpdatedAt = now
	}
	return validateAgentTaskTemplate(template)
}

func validateAgentTaskTemplate(template *AgentTaskTemplate) error {
	if template.UserID == 0 || template.ID.IsZero() ||
		template.ContractVersion != AgentTaskTemplateContractVersion {
		return errors.New("agent task template identity is incomplete")
	}
	if template.Name == "" || utf8.RuneCountInString(template.Name) > MaxAgentTaskTemplateNameRunes {
		return errors.New("agent task template name is invalid")
	}
	if utf8.RuneCountInString(template.Description) > MaxAgentTaskTemplateDescriptionRunes {
		return errors.New("agent task template description is too long")
	}
	if template.InstructionTemplate == "" ||
		len([]byte(template.InstructionTemplate)) > MaxAgentTaskTemplateInstructionBytes {
		return errors.New("agent task template instruction is invalid")
	}
	if template.IdempotencyKey == "" ||
		len([]byte(template.IdempotencyKey)) > MaxAgentTaskTemplateIdempotencyBytes {
		return errors.New("agent task template idempotency key is invalid")
	}
	if template.SourceRunID == "" || template.SourceRunRevision <= 0 ||
		template.SourceResultDigest == "" || template.SourceExecutionProfile == "" ||
		len(template.CapabilityIDs) == 0 {
		return errors.New("agent task template source evidence is incomplete")
	}
	if (template.SkillID == "") != (template.SkillVersion == "") {
		return errors.New("agent task template skill identity is incomplete")
	}
	if template.Revision <= 0 || template.CreatedAt.IsZero() || template.UpdatedAt.IsZero() {
		return errors.New("agent task template lifecycle is incomplete")
	}
	switch template.Status {
	case AgentTaskTemplateActive:
		if !template.ArchivedAt.IsZero() {
			return errors.New("active agent task template cannot be archived")
		}
	case AgentTaskTemplateArchived:
		if template.ArchivedAt.IsZero() {
			return errors.New("archived agent task template requires archived_at")
		}
	default:
		return fmt.Errorf("invalid agent task template status %q", template.Status)
	}
	return nil
}

func normalizedTaskTemplateCapabilities(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
