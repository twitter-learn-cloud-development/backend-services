package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"twitter-clone/internal/module/agent/marketplace"
)

const (
	CollectionExtensionMarketplacePublishers = "agent_extension_marketplace_publishers"
	CollectionExtensionMarketplaceReleases   = "agent_extension_marketplace_releases"
	CollectionExtensionMarketplaceAudit      = "agent_extension_marketplace_audit_events"
)

var (
	ErrExtensionMarketplacePublisherNotFound = marketplace.ErrPublisherNotFound
	ErrExtensionMarketplacePublisherConflict = marketplace.ErrPublisherConflict
	ErrExtensionMarketplaceReleaseNotFound   = marketplace.ErrReleaseNotFound
	ErrExtensionMarketplaceReleaseConflict   = marketplace.ErrReleaseConflict
	ErrExtensionMarketplaceRevisionConflict  = marketplace.ErrRevisionConflict
)

type extensionMarketplaceSigningKeyRecord struct {
	KeyID           string `bson:"key_id"`
	Algorithm       string `bson:"algorithm"`
	PublicKeyBase64 string `bson:"public_key_base64"`
	Status          string `bson:"status"`
}

type extensionMarketplacePublisherRecord struct {
	PublisherID     string                                 `bson:"_id"`
	ContractVersion string                                 `bson:"contract_version"`
	DisplayName     string                                 `bson:"display_name"`
	Verification    string                                 `bson:"verification"`
	SigningKeys     []extensionMarketplaceSigningKeyRecord `bson:"signing_keys"`
	VerifiedAt      time.Time                              `bson:"verified_at"`
	OwnerUserIDs    []uint64                               `bson:"owner_user_ids,omitempty"`
	Revision        int64                                  `bson:"revision"`
	CreatedBy       uint64                                 `bson:"created_by,omitempty"`
	UpdatedBy       uint64                                 `bson:"updated_by,omitempty"`
	CreatedAt       time.Time                              `bson:"created_at"`
	UpdatedAt       time.Time                              `bson:"updated_at"`
}

type extensionMarketplaceManifestRecord struct {
	ContractVersion      string   `bson:"contract_version"`
	PackageID            string   `bson:"package_id"`
	Kind                 string   `bson:"kind"`
	Version              string   `bson:"version"`
	PublisherID          string   `bson:"publisher_id"`
	DisplayName          string   `bson:"display_name"`
	Description          string   `bson:"description,omitempty"`
	ArtifactDigestSHA256 string   `bson:"artifact_digest_sha256"`
	CapabilityIDs        []string `bson:"capability_ids"`
	RequestedPermissions []string `bson:"requested_permissions"`
}

type extensionMarketplaceReleaseRecord struct {
	ReleaseID            string                             `bson:"_id"`
	ContractVersion      string                             `bson:"contract_version"`
	Manifest             extensionMarketplaceManifestRecord `bson:"manifest"`
	SignatureKeyID       string                             `bson:"signature_key_id"`
	SignatureBase64      string                             `bson:"signature_base64"`
	Status               string                             `bson:"status"`
	PublishedAt          time.Time                          `bson:"published_at"`
	Revision             int64                              `bson:"revision"`
	PublishedBy          uint64                             `bson:"published_by,omitempty"`
	WithdrawnBy          uint64                             `bson:"withdrawn_by,omitempty"`
	WithdrawalReasonCode string                             `bson:"withdrawal_reason_code,omitempty"`
	WithdrawnAt          time.Time                          `bson:"withdrawn_at,omitempty"`
	CreatedAt            time.Time                          `bson:"created_at"`
	UpdatedAt            time.Time                          `bson:"updated_at"`
}

type extensionMarketplaceAuditRecord struct {
	EventID     string    `bson:"_id"`
	OperationID string    `bson:"operation_id"`
	Action      string    `bson:"action"`
	Outcome     string    `bson:"outcome"`
	ActorUserID uint64    `bson:"actor_user_id"`
	PublisherID string    `bson:"publisher_id"`
	PackageID   string    `bson:"package_id,omitempty"`
	Version     string    `bson:"version,omitempty"`
	KeyID       string    `bson:"key_id,omitempty"`
	Revision    int64     `bson:"revision,omitempty"`
	ReasonCode  string    `bson:"reason_code,omitempty"`
	ErrorCode   string    `bson:"error_code,omitempty"`
	CreatedAt   time.Time `bson:"created_at"`
}

// MongoExtensionMarketplaceRepository owns public marketplace metadata only.
// It stores no private key, credential, executable artifact or installation.
type MongoExtensionMarketplaceRepository struct {
	publisherCollection *mongo.Collection
	releaseCollection   *mongo.Collection
	auditCollection     *mongo.Collection
}

func NewMongoExtensionMarketplaceRepository(db *mongo.Database) *MongoExtensionMarketplaceRepository {
	if db == nil {
		return &MongoExtensionMarketplaceRepository{}
	}
	return &MongoExtensionMarketplaceRepository{
		publisherCollection: db.Collection(CollectionExtensionMarketplacePublishers),
		releaseCollection:   db.Collection(CollectionExtensionMarketplaceReleases),
		auditCollection:     db.Collection(CollectionExtensionMarketplaceAudit),
	}
}

func (r *MongoExtensionMarketplaceRepository) EnsureIndexes(ctx context.Context) error {
	if r == nil || r.publisherCollection == nil || r.releaseCollection == nil || r.auditCollection == nil {
		return errors.New("extension marketplace repository is unavailable")
	}
	if _, err := r.publisherCollection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "verification", Value: 1}, {Key: "display_name", Value: 1}},
			Options: options.Index().SetName("idx_extension_marketplace_publisher_status"),
		},
		{
			Keys:    bson.D{{Key: "signing_keys.key_id", Value: 1}},
			Options: options.Index().SetName("idx_extension_marketplace_publisher_key"),
		},
		{
			Keys:    bson.D{{Key: "owner_user_ids", Value: 1}, {Key: "updated_at", Value: -1}},
			Options: options.Index().SetName("idx_extension_marketplace_publisher_owner"),
		},
	}); err != nil {
		return fmt.Errorf("create extension marketplace publisher indexes: %w", err)
	}
	if _, err := r.releaseCollection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "manifest.package_id", Value: 1}, {Key: "manifest.version", Value: 1}},
			Options: options.Index().
				SetName("uniq_extension_marketplace_package_version").
				SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "status", Value: 1}, {Key: "manifest.kind", Value: 1},
				{Key: "published_at", Value: -1}, {Key: "_id", Value: 1},
			},
			Options: options.Index().SetName("idx_extension_marketplace_public_catalog"),
		},
		{
			Keys: bson.D{
				{Key: "status", Value: 1}, {Key: "manifest.publisher_id", Value: 1},
				{Key: "published_at", Value: -1}, {Key: "_id", Value: 1},
			},
			Options: options.Index().SetName("idx_extension_marketplace_publisher_catalog"),
		},
		{
			Keys: bson.D{
				{Key: "manifest.package_id", Value: "text"},
				{Key: "manifest.display_name", Value: "text"},
				{Key: "manifest.description", Value: "text"},
				{Key: "manifest.capability_ids", Value: "text"},
			},
			Options: options.Index().SetName("text_extension_marketplace_catalog"),
		},
	}); err != nil {
		return fmt.Errorf("create extension marketplace release indexes: %w", err)
	}
	if _, err := r.auditCollection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "publisher_id", Value: 1}, {Key: "created_at", Value: -1}, {Key: "_id", Value: 1}},
			Options: options.Index().SetName("idx_extension_marketplace_audit_publisher"),
		},
		{
			Keys:    bson.D{{Key: "operation_id", Value: 1}, {Key: "created_at", Value: 1}},
			Options: options.Index().SetName("idx_extension_marketplace_audit_operation"),
		},
	}); err != nil {
		return fmt.Errorf("create extension marketplace audit indexes: %w", err)
	}
	return nil
}

// RegisterPublisher is deliberately not part of marketplace.CatalogStore.
// It is reserved for a future authenticated control plane or offline importer.
func (r *MongoExtensionMarketplaceRepository) RegisterPublisher(
	ctx context.Context,
	publisher marketplace.Publisher,
) error {
	if r == nil || r.publisherCollection == nil {
		return errors.New("extension marketplace repository is unavailable")
	}
	normalized, err := marketplace.NormalizePublisher(publisher)
	if err != nil {
		return err
	}
	record := marketplacePublisherRecordFromDomain(normalized)
	record.Revision = 1
	record.CreatedAt = time.Now().UTC()
	record.UpdatedAt = record.CreatedAt
	if _, err := r.publisherCollection.InsertOne(ctx, record); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrExtensionMarketplacePublisherConflict
		}
		return fmt.Errorf("insert extension marketplace publisher: %w", err)
	}
	return nil
}

// PublishRelease validates publisher trust and the Ed25519 signature before
// an immutable package version can enter the public catalog.
func (r *MongoExtensionMarketplaceRepository) PublishRelease(
	ctx context.Context,
	release marketplace.SignedRelease,
) error {
	if r == nil || r.releaseCollection == nil {
		return errors.New("extension marketplace repository is unavailable")
	}
	publishers, err := r.GetPublishers(ctx, []string{release.Manifest.PublisherID})
	if err != nil {
		return err
	}
	publisher, exists := publishers[strings.ToLower(strings.TrimSpace(release.Manifest.PublisherID))]
	if !exists {
		return ErrExtensionMarketplacePublisherNotFound
	}
	if _, err := marketplace.VerifyNewRelease(publisher, release); err != nil {
		return err
	}
	record := marketplaceReleaseRecordFromDomain(release)
	record.Revision = 1
	record.CreatedAt = time.Now().UTC()
	record.UpdatedAt = record.CreatedAt
	if _, err := r.releaseCollection.InsertOne(ctx, record); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrExtensionMarketplaceReleaseConflict
		}
		return fmt.Errorf("insert extension marketplace release: %w", err)
	}
	return nil
}

func (r *MongoExtensionMarketplaceRepository) CreatePublisher(ctx context.Context, record marketplace.PublisherControl) error {
	if r == nil || r.publisherCollection == nil {
		return errors.New("extension marketplace repository is unavailable")
	}
	normalized, err := marketplace.NormalizePublisherControl(record)
	if err != nil {
		return err
	}
	if _, err := r.publisherCollection.InsertOne(ctx, marketplacePublisherControlRecordFromDomain(normalized)); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return marketplace.ErrPublisherConflict
		}
		return fmt.Errorf("create extension marketplace publisher: %w", err)
	}
	return nil
}

func (r *MongoExtensionMarketplaceRepository) GetPublisherControl(ctx context.Context, publisherID string) (marketplace.PublisherControl, error) {
	if r == nil || r.publisherCollection == nil {
		return marketplace.PublisherControl{}, errors.New("extension marketplace repository is unavailable")
	}
	publisherID = strings.ToLower(strings.TrimSpace(publisherID))
	var record extensionMarketplacePublisherRecord
	if err := r.publisherCollection.FindOne(ctx, bson.M{"_id": publisherID}).Decode(&record); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return marketplace.PublisherControl{}, marketplace.ErrPublisherNotFound
		}
		return marketplace.PublisherControl{}, fmt.Errorf("get extension marketplace publisher: %w", err)
	}
	result := marketplacePublisherControlRecordToDomain(record)
	if len(result.OwnerUserIDs) == 0 || result.Revision < 1 {
		return marketplace.PublisherControl{}, fmt.Errorf("%w: publisher requires control-plane migration", marketplace.ErrInvalidControlRecord)
	}
	return marketplace.NormalizePublisherControl(result)
}

func (r *MongoExtensionMarketplaceRepository) ListPublisherControls(
	ctx context.Context,
	ownerUserID uint64,
	includeAll bool,
	page marketplace.ManagementPage,
) ([]marketplace.PublisherControl, int64, error) {
	if r == nil || r.publisherCollection == nil {
		return nil, 0, errors.New("extension marketplace repository is unavailable")
	}
	page, err := marketplace.NormalizeManagementPage(page)
	if err != nil {
		return nil, 0, err
	}
	filter := bson.M{"revision": bson.M{"$gte": 1}, "owner_user_ids.0": bson.M{"$exists": true}}
	if !includeAll {
		if ownerUserID == 0 {
			return []marketplace.PublisherControl{}, 0, nil
		}
		filter["owner_user_ids"] = ownerUserID
	}
	total, err := r.publisherCollection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("count extension marketplace publishers: %w", err)
	}
	cursor, err := r.publisherCollection.Find(ctx, filter, options.Find().
		SetSort(bson.D{{Key: "updated_at", Value: -1}, {Key: "_id", Value: 1}}).
		SetSkip(int64((page.Page-1)*page.PageSize)).SetLimit(int64(page.PageSize)))
	if err != nil {
		return nil, 0, fmt.Errorf("list extension marketplace publishers: %w", err)
	}
	defer cursor.Close(ctx)
	var records []extensionMarketplacePublisherRecord
	if err := cursor.All(ctx, &records); err != nil {
		return nil, 0, fmt.Errorf("decode extension marketplace publishers: %w", err)
	}
	result := make([]marketplace.PublisherControl, 0, len(records))
	for _, record := range records {
		converted, convertErr := marketplace.NormalizePublisherControl(marketplacePublisherControlRecordToDomain(record))
		if convertErr != nil {
			return nil, 0, convertErr
		}
		result = append(result, converted)
	}
	return result, total, nil
}

func (r *MongoExtensionMarketplaceRepository) ListOwnedPublisherIDs(ctx context.Context, ownerUserID uint64) ([]string, error) {
	if r == nil || r.publisherCollection == nil {
		return nil, errors.New("extension marketplace repository is unavailable")
	}
	if ownerUserID == 0 {
		return []string{}, nil
	}
	cursor, err := r.publisherCollection.Find(ctx, bson.M{"owner_user_ids": ownerUserID}, options.Find().
		SetProjection(bson.M{"_id": 1}).SetSort(bson.D{{Key: "_id", Value: 1}}).SetLimit(marketplace.MaxPageSize))
	if err != nil {
		return nil, fmt.Errorf("list owned extension marketplace publishers: %w", err)
	}
	defer cursor.Close(ctx)
	var records []struct {
		PublisherID string `bson:"_id"`
	}
	if err := cursor.All(ctx, &records); err != nil {
		return nil, fmt.Errorf("decode owned extension marketplace publishers: %w", err)
	}
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.PublisherID)
	}
	return ids, nil
}

func (r *MongoExtensionMarketplaceRepository) UpdatePublisherControl(
	ctx context.Context,
	record marketplace.PublisherControl,
	expectedRevision int64,
) error {
	if r == nil || r.publisherCollection == nil {
		return errors.New("extension marketplace repository is unavailable")
	}
	normalized, err := marketplace.NormalizePublisherControl(record)
	if err != nil {
		return err
	}
	if expectedRevision < 1 || normalized.Revision != expectedRevision+1 {
		return marketplace.ErrRevisionConflict
	}
	value := marketplacePublisherControlRecordFromDomain(normalized)
	result, err := r.publisherCollection.UpdateOne(ctx, bson.M{"_id": value.PublisherID, "revision": expectedRevision}, bson.M{"$set": bson.M{
		"contract_version": value.ContractVersion, "display_name": value.DisplayName,
		"verification": value.Verification, "signing_keys": value.SigningKeys,
		"verified_at": value.VerifiedAt, "owner_user_ids": value.OwnerUserIDs,
		"revision": value.Revision, "updated_by": value.UpdatedBy, "updated_at": value.UpdatedAt,
	}})
	if err != nil {
		return fmt.Errorf("update extension marketplace publisher: %w", err)
	}
	if result.MatchedCount == 0 {
		return marketplace.ErrRevisionConflict
	}
	return nil
}

func (r *MongoExtensionMarketplaceRepository) CreateRelease(ctx context.Context, record marketplace.ReleaseControl) error {
	if r == nil || r.releaseCollection == nil {
		return errors.New("extension marketplace repository is unavailable")
	}
	normalized, err := marketplace.NormalizeReleaseControl(record)
	if err != nil {
		return err
	}
	publisher, err := r.GetPublisherControl(ctx, normalized.Release.Manifest.PublisherID)
	if err != nil {
		return err
	}
	if _, err := marketplace.VerifyNewRelease(publisher.Publisher, normalized.Release); err != nil {
		return err
	}
	if _, err := r.releaseCollection.InsertOne(ctx, marketplaceReleaseControlRecordFromDomain(normalized)); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return marketplace.ErrReleaseConflict
		}
		return fmt.Errorf("create extension marketplace release: %w", err)
	}
	return nil
}

func (r *MongoExtensionMarketplaceRepository) GetReleaseControl(ctx context.Context, releaseID string) (marketplace.ReleaseControl, error) {
	if r == nil || r.releaseCollection == nil {
		return marketplace.ReleaseControl{}, errors.New("extension marketplace repository is unavailable")
	}
	var record extensionMarketplaceReleaseRecord
	if err := r.releaseCollection.FindOne(ctx, bson.M{"_id": strings.TrimSpace(releaseID)}).Decode(&record); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return marketplace.ReleaseControl{}, marketplace.ErrReleaseNotFound
		}
		return marketplace.ReleaseControl{}, fmt.Errorf("get extension marketplace release: %w", err)
	}
	result := marketplaceReleaseControlRecordToDomain(record)
	if result.Revision < 1 || result.PublishedBy == 0 {
		return marketplace.ReleaseControl{}, fmt.Errorf("%w: release requires control-plane migration", marketplace.ErrInvalidControlRecord)
	}
	return marketplace.NormalizeReleaseControl(result)
}

func (r *MongoExtensionMarketplaceRepository) ListReleaseControls(
	ctx context.Context,
	publisherIDs []string,
	status string,
	page marketplace.ManagementPage,
) ([]marketplace.ReleaseControl, int64, error) {
	if r == nil || r.releaseCollection == nil {
		return nil, 0, errors.New("extension marketplace repository is unavailable")
	}
	page, err := marketplace.NormalizeManagementPage(page)
	if err != nil {
		return nil, 0, err
	}
	status = strings.ToLower(strings.TrimSpace(status))
	if status != "" && status != marketplace.ReleasePublished && status != marketplace.ReleaseWithdrawn {
		return nil, 0, marketplace.ErrInvalidQuery
	}
	filter := bson.M{"revision": bson.M{"$gte": 1}, "published_by": bson.M{"$gt": 0}}
	if publisherIDs != nil {
		if len(publisherIDs) == 0 {
			return []marketplace.ReleaseControl{}, 0, nil
		}
		filter["manifest.publisher_id"] = bson.M{"$in": publisherIDs}
	}
	if status != "" {
		filter["status"] = status
	}
	total, err := r.releaseCollection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("count extension marketplace releases: %w", err)
	}
	cursor, err := r.releaseCollection.Find(ctx, filter, options.Find().
		SetSort(bson.D{{Key: "updated_at", Value: -1}, {Key: "_id", Value: 1}}).
		SetSkip(int64((page.Page-1)*page.PageSize)).SetLimit(int64(page.PageSize)))
	if err != nil {
		return nil, 0, fmt.Errorf("list managed extension marketplace releases: %w", err)
	}
	defer cursor.Close(ctx)
	var records []extensionMarketplaceReleaseRecord
	if err := cursor.All(ctx, &records); err != nil {
		return nil, 0, fmt.Errorf("decode managed extension marketplace releases: %w", err)
	}
	result := make([]marketplace.ReleaseControl, 0, len(records))
	for _, record := range records {
		converted, convertErr := marketplace.NormalizeReleaseControl(marketplaceReleaseControlRecordToDomain(record))
		if convertErr != nil {
			return nil, 0, convertErr
		}
		result = append(result, converted)
	}
	return result, total, nil
}

func (r *MongoExtensionMarketplaceRepository) UpdateReleaseControl(
	ctx context.Context,
	record marketplace.ReleaseControl,
	expectedRevision int64,
) error {
	if r == nil || r.releaseCollection == nil {
		return errors.New("extension marketplace repository is unavailable")
	}
	normalized, err := marketplace.NormalizeReleaseControl(record)
	if err != nil {
		return err
	}
	if expectedRevision < 1 || normalized.Revision != expectedRevision+1 {
		return marketplace.ErrRevisionConflict
	}
	value := marketplaceReleaseControlRecordFromDomain(normalized)
	result, err := r.releaseCollection.UpdateOne(ctx, bson.M{"_id": value.ReleaseID, "revision": expectedRevision, "status": marketplace.ReleasePublished}, bson.M{"$set": bson.M{
		"status": value.Status, "revision": value.Revision, "withdrawn_by": value.WithdrawnBy,
		"withdrawal_reason_code": value.WithdrawalReasonCode, "withdrawn_at": value.WithdrawnAt,
		"updated_at": value.UpdatedAt,
	}})
	if err != nil {
		return fmt.Errorf("update extension marketplace release: %w", err)
	}
	if result.MatchedCount == 0 {
		return marketplace.ErrRevisionConflict
	}
	return nil
}

func (r *MongoExtensionMarketplaceRepository) AppendAuditEvent(ctx context.Context, event marketplace.AuditEvent) error {
	if r == nil || r.auditCollection == nil {
		return errors.New("extension marketplace repository is unavailable")
	}
	normalized, err := marketplace.NormalizeAuditEvent(event)
	if err != nil {
		return err
	}
	if _, err := r.auditCollection.InsertOne(ctx, marketplaceAuditRecordFromDomain(normalized)); err != nil {
		return fmt.Errorf("append extension marketplace audit event: %w", err)
	}
	return nil
}

func (r *MongoExtensionMarketplaceRepository) ListAuditEvents(
	ctx context.Context,
	publisherIDs []string,
	action string,
	outcome string,
	page marketplace.ManagementPage,
) ([]marketplace.AuditEvent, int64, error) {
	if r == nil || r.auditCollection == nil {
		return nil, 0, errors.New("extension marketplace repository is unavailable")
	}
	page, err := marketplace.NormalizeManagementPage(page)
	if err != nil {
		return nil, 0, err
	}
	filter := bson.M{}
	if publisherIDs != nil {
		if len(publisherIDs) == 0 {
			return []marketplace.AuditEvent{}, 0, nil
		}
		filter["publisher_id"] = bson.M{"$in": publisherIDs}
	}
	if value := strings.ToLower(strings.TrimSpace(action)); value != "" {
		filter["action"] = value
	}
	if value := strings.ToLower(strings.TrimSpace(outcome)); value != "" {
		filter["outcome"] = value
	}
	total, err := r.auditCollection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("count extension marketplace audit events: %w", err)
	}
	cursor, err := r.auditCollection.Find(ctx, filter, options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: 1}}).
		SetSkip(int64((page.Page-1)*page.PageSize)).SetLimit(int64(page.PageSize)))
	if err != nil {
		return nil, 0, fmt.Errorf("list extension marketplace audit events: %w", err)
	}
	defer cursor.Close(ctx)
	var records []extensionMarketplaceAuditRecord
	if err := cursor.All(ctx, &records); err != nil {
		return nil, 0, fmt.Errorf("decode extension marketplace audit events: %w", err)
	}
	result := make([]marketplace.AuditEvent, 0, len(records))
	for _, record := range records {
		result = append(result, marketplaceAuditRecordToDomain(record))
	}
	return result, total, nil
}

func (r *MongoExtensionMarketplaceRepository) ListPublished(
	ctx context.Context,
	request marketplace.ListRequest,
) ([]marketplace.SignedRelease, bool, error) {
	if r == nil || r.releaseCollection == nil {
		return nil, false, errors.New("extension marketplace repository is unavailable")
	}
	if request.PageSize < 1 || request.PageSize > marketplace.MaxPageSize {
		return nil, false, marketplace.ErrInvalidQuery
	}
	filter := marketplaceReleaseFilter(request)
	cursor, err := r.releaseCollection.Find(
		ctx,
		filter,
		options.Find().
			SetSort(bson.D{{Key: "published_at", Value: -1}, {Key: "_id", Value: 1}}).
			SetLimit(int64(request.PageSize+1)),
	)
	if err != nil {
		return nil, false, fmt.Errorf("list extension marketplace releases: %w", err)
	}
	defer cursor.Close(ctx)
	var records []extensionMarketplaceReleaseRecord
	if err := cursor.All(ctx, &records); err != nil {
		return nil, false, fmt.Errorf("decode extension marketplace releases: %w", err)
	}
	hasMore := len(records) > request.PageSize
	if hasMore {
		records = records[:request.PageSize]
	}
	releases := make([]marketplace.SignedRelease, 0, len(records))
	for _, record := range records {
		releases = append(releases, marketplaceReleaseRecordToDomain(record))
	}
	return releases, hasMore, nil
}

func (r *MongoExtensionMarketplaceRepository) GetPublishers(
	ctx context.Context,
	publisherIDs []string,
) (map[string]marketplace.Publisher, error) {
	if r == nil || r.publisherCollection == nil {
		return nil, errors.New("extension marketplace repository is unavailable")
	}
	ids := make([]string, 0, len(publisherIDs))
	seen := make(map[string]struct{}, len(publisherIDs))
	for _, publisherID := range publisherIDs {
		publisherID = strings.ToLower(strings.TrimSpace(publisherID))
		if publisherID == "" {
			continue
		}
		if _, exists := seen[publisherID]; exists {
			continue
		}
		seen[publisherID] = struct{}{}
		ids = append(ids, publisherID)
	}
	if len(ids) == 0 {
		return map[string]marketplace.Publisher{}, nil
	}
	if len(ids) > marketplace.MaxPageSize {
		return nil, marketplace.ErrInvalidQuery
	}
	cursor, err := r.publisherCollection.Find(ctx, bson.M{"_id": bson.M{"$in": ids}})
	if err != nil {
		return nil, fmt.Errorf("list extension marketplace publishers: %w", err)
	}
	defer cursor.Close(ctx)
	var records []extensionMarketplacePublisherRecord
	if err := cursor.All(ctx, &records); err != nil {
		return nil, fmt.Errorf("decode extension marketplace publishers: %w", err)
	}
	publishers := make(map[string]marketplace.Publisher, len(records))
	for _, record := range records {
		publisher := marketplacePublisherRecordToDomain(record)
		publishers[publisher.PublisherID] = publisher
	}
	return publishers, nil
}

func marketplaceReleaseFilter(request marketplace.ListRequest) bson.M {
	base := bson.M{"status": marketplace.ReleasePublished}
	if request.Kind != "" {
		base["manifest.kind"] = request.Kind
	}
	if request.PublisherID != "" {
		base["manifest.publisher_id"] = request.PublisherID
	}
	if request.Search != "" {
		base["$text"] = bson.M{"$search": request.Search}
	}
	if request.After == nil {
		return base
	}
	position := bson.M{"$or": []bson.M{
		{"published_at": bson.M{"$lt": request.After.PublishedAt}},
		{"published_at": request.After.PublishedAt, "_id": bson.M{"$gt": request.After.ReleaseID}},
	}}
	return bson.M{"$and": []bson.M{base, position}}
}

func marketplacePublisherRecordFromDomain(publisher marketplace.Publisher) extensionMarketplacePublisherRecord {
	keys := make([]extensionMarketplaceSigningKeyRecord, 0, len(publisher.SigningKeys))
	for _, key := range publisher.SigningKeys {
		keys = append(keys, extensionMarketplaceSigningKeyRecord{
			KeyID: key.KeyID, Algorithm: key.Algorithm,
			PublicKeyBase64: key.PublicKeyBase64, Status: key.Status,
		})
	}
	return extensionMarketplacePublisherRecord{
		PublisherID: publisher.PublisherID, ContractVersion: publisher.ContractVersion,
		DisplayName: publisher.DisplayName, Verification: publisher.Verification,
		SigningKeys: keys, VerifiedAt: publisher.VerifiedAt,
	}
}

func marketplacePublisherControlRecordFromDomain(record marketplace.PublisherControl) extensionMarketplacePublisherRecord {
	result := marketplacePublisherRecordFromDomain(record.Publisher)
	result.OwnerUserIDs = append([]uint64(nil), record.OwnerUserIDs...)
	result.Revision = record.Revision
	result.CreatedBy = record.CreatedBy
	result.UpdatedBy = record.UpdatedBy
	result.CreatedAt = record.CreatedAt
	result.UpdatedAt = record.UpdatedAt
	return result
}

func marketplacePublisherRecordToDomain(record extensionMarketplacePublisherRecord) marketplace.Publisher {
	keys := make([]marketplace.SigningKey, 0, len(record.SigningKeys))
	for _, key := range record.SigningKeys {
		keys = append(keys, marketplace.SigningKey{
			KeyID: key.KeyID, Algorithm: key.Algorithm,
			PublicKeyBase64: key.PublicKeyBase64, Status: key.Status,
		})
	}
	return marketplace.Publisher{
		ContractVersion: record.ContractVersion, PublisherID: record.PublisherID,
		DisplayName: record.DisplayName, Verification: record.Verification,
		SigningKeys: keys, VerifiedAt: record.VerifiedAt,
	}
}

func marketplacePublisherControlRecordToDomain(record extensionMarketplacePublisherRecord) marketplace.PublisherControl {
	return marketplace.PublisherControl{
		Publisher:    marketplacePublisherRecordToDomain(record),
		OwnerUserIDs: append([]uint64(nil), record.OwnerUserIDs...),
		Revision:     record.Revision, CreatedBy: record.CreatedBy, UpdatedBy: record.UpdatedBy,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
}

func marketplaceReleaseRecordFromDomain(release marketplace.SignedRelease) extensionMarketplaceReleaseRecord {
	return extensionMarketplaceReleaseRecord{
		ReleaseID: release.ReleaseID, ContractVersion: release.ContractVersion,
		Manifest: extensionMarketplaceManifestRecord{
			ContractVersion: release.Manifest.ContractVersion, PackageID: release.Manifest.PackageID,
			Kind: release.Manifest.Kind, Version: release.Manifest.Version,
			PublisherID: release.Manifest.PublisherID, DisplayName: release.Manifest.DisplayName,
			Description:          release.Manifest.Description,
			ArtifactDigestSHA256: release.Manifest.ArtifactDigestSHA256,
			CapabilityIDs:        append([]string(nil), release.Manifest.CapabilityIDs...),
			RequestedPermissions: append([]string(nil), release.Manifest.RequestedPermissions...),
		},
		SignatureKeyID: release.SignatureKeyID, SignatureBase64: release.SignatureBase64,
		Status: release.Status, PublishedAt: release.PublishedAt,
	}
}

func marketplaceReleaseRecordToDomain(record extensionMarketplaceReleaseRecord) marketplace.SignedRelease {
	return marketplace.SignedRelease{
		ContractVersion: record.ContractVersion, ReleaseID: record.ReleaseID,
		Manifest: marketplace.Manifest{
			ContractVersion: record.Manifest.ContractVersion, PackageID: record.Manifest.PackageID,
			Kind: record.Manifest.Kind, Version: record.Manifest.Version,
			PublisherID: record.Manifest.PublisherID, DisplayName: record.Manifest.DisplayName,
			Description:          record.Manifest.Description,
			ArtifactDigestSHA256: record.Manifest.ArtifactDigestSHA256,
			CapabilityIDs:        append([]string(nil), record.Manifest.CapabilityIDs...),
			RequestedPermissions: append([]string(nil), record.Manifest.RequestedPermissions...),
		},
		SignatureKeyID: record.SignatureKeyID, SignatureBase64: record.SignatureBase64,
		Status: record.Status, PublishedAt: record.PublishedAt,
	}
}

func marketplaceReleaseControlRecordFromDomain(record marketplace.ReleaseControl) extensionMarketplaceReleaseRecord {
	result := marketplaceReleaseRecordFromDomain(record.Release)
	result.Revision = record.Revision
	result.PublishedBy = record.PublishedBy
	result.WithdrawnBy = record.WithdrawnBy
	result.WithdrawalReasonCode = record.WithdrawalReasonCode
	result.WithdrawnAt = record.WithdrawnAt
	result.CreatedAt = record.CreatedAt
	result.UpdatedAt = record.UpdatedAt
	return result
}

func marketplaceReleaseControlRecordToDomain(record extensionMarketplaceReleaseRecord) marketplace.ReleaseControl {
	return marketplace.ReleaseControl{
		Release: marketplaceReleaseRecordToDomain(record), Revision: record.Revision,
		PublishedBy: record.PublishedBy, WithdrawnBy: record.WithdrawnBy,
		WithdrawalReasonCode: record.WithdrawalReasonCode, WithdrawnAt: record.WithdrawnAt,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
}

func marketplaceAuditRecordFromDomain(event marketplace.AuditEvent) extensionMarketplaceAuditRecord {
	return extensionMarketplaceAuditRecord{
		EventID: event.EventID, OperationID: event.OperationID, Action: event.Action,
		Outcome: event.Outcome, ActorUserID: event.ActorUserID, PublisherID: event.PublisherID,
		PackageID: event.PackageID, Version: event.Version, KeyID: event.KeyID,
		Revision: event.Revision, ReasonCode: event.ReasonCode, ErrorCode: event.ErrorCode,
		CreatedAt: event.CreatedAt,
	}
}

func marketplaceAuditRecordToDomain(record extensionMarketplaceAuditRecord) marketplace.AuditEvent {
	return marketplace.AuditEvent{
		EventID: record.EventID, OperationID: record.OperationID, Action: record.Action,
		Outcome: record.Outcome, ActorUserID: record.ActorUserID, PublisherID: record.PublisherID,
		PackageID: record.PackageID, Version: record.Version, KeyID: record.KeyID,
		Revision: record.Revision, ReasonCode: record.ReasonCode, ErrorCode: record.ErrorCode,
		CreatedAt: record.CreatedAt,
	}
}

var _ marketplace.CatalogStore = (*MongoExtensionMarketplaceRepository)(nil)
var _ marketplace.ControlStore = (*MongoExtensionMarketplaceRepository)(nil)
