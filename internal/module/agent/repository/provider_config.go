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
	ProviderConfigStatusActive  = "active"
	ProviderConfigStatusRevoked = "revoked"
	ProviderConfigKindLLM       = "llm"
	ProviderConfigKindWebSearch = "web_search"
)

var (
	ErrProviderConfigNotFound = errors.New("provider config not found")
	ErrProviderConfigConflict = errors.New("provider config revision conflict")
)

type ProviderConfig struct {
	ID                primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID            uint64             `bson:"user_id" json:"user_id"`
	Kind              string             `bson:"kind,omitempty" json:"kind"`
	Name              string             `bson:"name" json:"name"`
	Provider          string             `bson:"provider" json:"provider"`
	BaseURL           string             `bson:"base_url" json:"base_url"`
	Model             string             `bson:"model" json:"model"`
	Status            string             `bson:"status" json:"status"`
	HasSecret         bool               `bson:"has_secret" json:"has_secret"`
	EncryptionKeyID   string             `bson:"encryption_key_id,omitempty" json:"-"`
	SecretNonce       string             `bson:"secret_nonce,omitempty" json:"-"`
	EncryptedAPIKey   string             `bson:"encrypted_api_key,omitempty" json:"-"`
	CredentialVersion int64              `bson:"credential_version" json:"credential_version"`
	Revision          int64              `bson:"revision" json:"revision"`
	CreatedAt         time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt         time.Time          `bson:"updated_at" json:"updated_at"`
}

// ProviderConfigRepository is additive so existing AgentRepository test
// doubles and compatibility adapters do not need to implement unrelated CRUD.
type ProviderConfigRepository interface {
	CreateProviderConfig(ctx context.Context, config *ProviderConfig) error
	UpdateProviderConfig(ctx context.Context, config *ProviderConfig, expectedRevision int64) error
	ListProviderConfigs(ctx context.Context, userID uint64, page, pageSize int) ([]*ProviderConfig, int64, error)
	GetProviderConfig(ctx context.Context, id primitive.ObjectID, userID uint64) (*ProviderConfig, error)
	RevokeProviderConfig(ctx context.Context, id primitive.ObjectID, userID uint64, expectedRevision int64) error
}

// ProviderConfigKindRepository is optional so older test doubles and adapters
// remain source-compatible while Mongo can provide correct filtered paging.
type ProviderConfigKindRepository interface {
	ListProviderConfigsByKind(ctx context.Context, userID uint64, kind string, page, pageSize int) ([]*ProviderConfig, int64, error)
}

func (r *MongoAgentRepository) CreateProviderConfig(ctx context.Context, config *ProviderConfig) error {
	if r == nil || r.providerConfigColl == nil {
		return errors.New("provider config collection is unavailable")
	}
	if config == nil {
		return errors.New("provider config is required")
	}
	now := time.Now().UTC()
	if config.ID.IsZero() {
		config.ID = primitive.NewObjectID()
	}
	if config.Status == "" {
		config.Status = ProviderConfigStatusActive
	}
	if config.Kind == "" {
		config.Kind = ProviderConfigKindLLM
	}
	if config.CredentialVersion <= 0 {
		config.CredentialVersion = 1
	}
	if config.Revision <= 0 {
		config.Revision = 1
	}
	config.CreatedAt = now
	config.UpdatedAt = now
	if _, err := r.providerConfigColl.InsertOne(ctx, config); err != nil {
		return fmt.Errorf("insert provider config failed: %w", err)
	}
	return nil
}

func (r *MongoAgentRepository) UpdateProviderConfig(ctx context.Context, config *ProviderConfig, expectedRevision int64) error {
	if r == nil || r.providerConfigColl == nil {
		return errors.New("provider config collection is unavailable")
	}
	if config == nil {
		return errors.New("provider config is required")
	}
	now := time.Now().UTC()
	result, err := r.providerConfigColl.UpdateOne(ctx,
		bson.M{"_id": config.ID, "user_id": config.UserID, "status": ProviderConfigStatusActive, "revision": expectedRevision},
		bson.M{
			"$set": bson.M{
				"kind": config.Kind, "name": config.Name, "provider": config.Provider, "base_url": config.BaseURL,
				"model": config.Model, "has_secret": config.HasSecret,
				"encryption_key_id": config.EncryptionKeyID, "secret_nonce": config.SecretNonce,
				"encrypted_api_key":  config.EncryptedAPIKey,
				"credential_version": config.CredentialVersion, "updated_at": now,
			},
			"$inc": bson.M{"revision": 1},
		},
	)
	if err != nil {
		return fmt.Errorf("update provider config failed: %w", err)
	}
	if result.MatchedCount == 0 {
		return ErrProviderConfigConflict
	}
	config.Revision = expectedRevision + 1
	config.UpdatedAt = now
	return nil
}

func (r *MongoAgentRepository) ListProviderConfigs(ctx context.Context, userID uint64, page, pageSize int) ([]*ProviderConfig, int64, error) {
	return r.ListProviderConfigsByKind(ctx, userID, "", page, pageSize)
}

func (r *MongoAgentRepository) ListProviderConfigsByKind(
	ctx context.Context,
	userID uint64,
	kind string,
	page, pageSize int,
) ([]*ProviderConfig, int64, error) {
	if r == nil || r.providerConfigColl == nil {
		return nil, 0, errors.New("provider config collection is unavailable")
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	filter := bson.M{"user_id": userID}
	switch kind {
	case ProviderConfigKindLLM:
		filter["$or"] = bson.A{
			bson.M{"kind": ProviderConfigKindLLM},
			bson.M{"kind": bson.M{"$exists": false}},
			bson.M{"kind": ""},
		}
	case "":
	default:
		filter["kind"] = kind
	}
	total, err := r.providerConfigColl.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("count provider configs failed: %w", err)
	}
	cursor, err := r.providerConfigColl.Find(ctx, filter, options.Find().
		SetSort(bson.D{{Key: "updated_at", Value: -1}}).
		SetSkip(int64((page-1)*pageSize)).SetLimit(int64(pageSize)))
	if err != nil {
		return nil, 0, fmt.Errorf("find provider configs failed: %w", err)
	}
	defer cursor.Close(ctx)
	var configs []*ProviderConfig
	if err := cursor.All(ctx, &configs); err != nil {
		return nil, 0, fmt.Errorf("decode provider configs failed: %w", err)
	}
	return configs, total, nil
}

func (r *MongoAgentRepository) GetProviderConfig(ctx context.Context, id primitive.ObjectID, userID uint64) (*ProviderConfig, error) {
	if r == nil || r.providerConfigColl == nil {
		return nil, errors.New("provider config collection is unavailable")
	}
	var config ProviderConfig
	if err := r.providerConfigColl.FindOne(ctx, bson.M{"_id": id, "user_id": userID}).Decode(&config); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrProviderConfigNotFound
		}
		return nil, fmt.Errorf("find provider config failed: %w", err)
	}
	return &config, nil
}

func (r *MongoAgentRepository) RevokeProviderConfig(
	ctx context.Context,
	id primitive.ObjectID,
	userID uint64,
	expectedRevision int64,
) error {
	if r == nil || r.providerConfigColl == nil {
		return errors.New("provider config collection is unavailable")
	}
	result, err := r.providerConfigColl.UpdateOne(ctx,
		bson.M{"_id": id, "user_id": userID, "status": ProviderConfigStatusActive, "revision": expectedRevision},
		bson.M{
			"$set": bson.M{
				"status": ProviderConfigStatusRevoked, "has_secret": false, "updated_at": time.Now().UTC(),
			},
			"$unset": bson.M{
				"encryption_key_id": "", "secret_nonce": "", "encrypted_api_key": "",
			},
			"$inc": bson.M{"revision": 1},
		},
	)
	if err != nil {
		return fmt.Errorf("revoke provider config failed: %w", err)
	}
	if result.MatchedCount == 0 {
		return ErrProviderConfigConflict
	}
	return nil
}
