package marketplace

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	ControlContractVersion = "agent.extension_marketplace_control.v1"
	AuditContractVersion   = "agent.extension_marketplace_audit.v1"

	AdministrationEnabledEnv = "AGENT_EXTENSION_MARKETPLACE_ADMIN_ENABLED"
	AdministrationTokenEnv   = "AGENT_EXTENSION_MARKETPLACE_ADMIN_TOKEN"
	AdministratorUserIDsEnv  = "AGENT_EXTENSION_MARKETPLACE_ADMIN_USER_IDS"
	AdminTokenMetadataKey    = "x-agent-extension-marketplace-admin-token"

	AuditOutcomeRequested = "requested"
	AuditOutcomeSucceeded = "succeeded"
	AuditOutcomeFailed    = "failed"

	AuditActionRegisterPublisher = "publisher.register"
	AuditActionRotateKey         = "publisher.key.rotate"
	AuditActionRevokeKey         = "publisher.key.revoke"
	AuditActionSetVerification   = "publisher.verification.set"
	AuditActionPublishRelease    = "release.publish"
	AuditActionWithdrawRelease   = "release.withdraw"

	WithdrawalReasonSecurity       = "security_incident"
	WithdrawalReasonPublisher      = "publisher_request"
	WithdrawalReasonPolicy         = "policy_violation"
	WithdrawalReasonSuperseded     = "superseded"
	WithdrawalReasonArtifactBroken = "artifact_unavailable"

	DefaultManagementPageSize = 20
	MaxManagementPageSize     = 50
	MaxPublisherOwners        = 16
)

var (
	ErrControlDisabled      = errors.New("extension marketplace administration is disabled")
	ErrControlForbidden     = errors.New("extension marketplace administration is forbidden")
	ErrPublisherNotFound    = errors.New("extension marketplace publisher not found")
	ErrPublisherConflict    = errors.New("extension marketplace publisher already exists")
	ErrReleaseNotFound      = errors.New("extension marketplace release not found")
	ErrReleaseConflict      = errors.New("extension marketplace release already exists")
	ErrRevisionConflict     = errors.New("extension marketplace revision conflict")
	ErrInvalidControlRecord = errors.New("invalid extension marketplace control record")
	ErrInvalidAuditEvent    = errors.New("invalid extension marketplace audit event")
	ErrInvalidWithdrawal    = errors.New("invalid extension marketplace withdrawal")
)

// PublisherControl adds ownership and optimistic concurrency metadata to the
// public publisher identity. Owners are immutable in this increment so an
// ordinary key operation cannot become an account-takeover path.
type PublisherControl struct {
	Publisher    Publisher
	OwnerUserIDs []uint64
	Revision     int64
	CreatedBy    uint64
	UpdatedBy    uint64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ReleaseControl retains immutable signed release evidence while allowing one
// terminal published -> withdrawn lifecycle transition.
type ReleaseControl struct {
	Release              SignedRelease
	Revision             int64
	PublishedBy          uint64
	WithdrawnBy          uint64
	WithdrawalReasonCode string
	WithdrawnAt          time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type AuditEvent struct {
	EventID     string
	OperationID string
	Action      string
	Outcome     string
	ActorUserID uint64
	PublisherID string
	PackageID   string
	Version     string
	KeyID       string
	Revision    int64
	ReasonCode  string
	ErrorCode   string
	CreatedAt   time.Time
}

type ManagementPage struct {
	Page     int
	PageSize int
}

// ControlStore is deliberately separate from the public CatalogStore. It
// persists governance metadata but owns no private key or artifact bytes.
type ControlStore interface {
	CreatePublisher(context.Context, PublisherControl) error
	GetPublisherControl(context.Context, string) (PublisherControl, error)
	ListPublisherControls(context.Context, uint64, bool, ManagementPage) ([]PublisherControl, int64, error)
	ListOwnedPublisherIDs(context.Context, uint64) ([]string, error)
	UpdatePublisherControl(context.Context, PublisherControl, int64) error

	CreateRelease(context.Context, ReleaseControl) error
	GetReleaseControl(context.Context, string) (ReleaseControl, error)
	ListReleaseControls(context.Context, []string, string, ManagementPage) ([]ReleaseControl, int64, error)
	UpdateReleaseControl(context.Context, ReleaseControl, int64) error

	AppendAuditEvent(context.Context, AuditEvent) error
	ListAuditEvents(context.Context, []string, string, string, ManagementPage) ([]AuditEvent, int64, error)
}

func NormalizeManagementPage(page ManagementPage) (ManagementPage, error) {
	if page.Page == 0 {
		page.Page = 1
	}
	if page.PageSize == 0 {
		page.PageSize = DefaultManagementPageSize
	}
	if page.Page < 1 || page.PageSize < 1 || page.PageSize > MaxManagementPageSize {
		return ManagementPage{}, fmt.Errorf("%w: invalid pagination", ErrInvalidControlRecord)
	}
	return page, nil
}

func NormalizePublisherControl(record PublisherControl) (PublisherControl, error) {
	publisher, err := NormalizePublisher(record.Publisher)
	if err != nil {
		return PublisherControl{}, err
	}
	owners, err := normalizeOwnerUserIDs(record.OwnerUserIDs)
	if err != nil {
		return PublisherControl{}, err
	}
	record.Publisher = publisher
	record.OwnerUserIDs = owners
	record.CreatedAt = record.CreatedAt.UTC()
	record.UpdatedAt = record.UpdatedAt.UTC()
	if record.Revision < 1 || record.CreatedBy == 0 || record.UpdatedBy == 0 ||
		record.CreatedAt.IsZero() || record.UpdatedAt.IsZero() || record.UpdatedAt.Before(record.CreatedAt) {
		return PublisherControl{}, fmt.Errorf("%w: publisher metadata is incomplete", ErrInvalidControlRecord)
	}
	return record, nil
}

func NormalizeReleaseControl(record ReleaseControl) (ReleaseControl, error) {
	record.Release.ContractVersion = strings.TrimSpace(record.Release.ContractVersion)
	record.Release.ReleaseID = strings.TrimSpace(record.Release.ReleaseID)
	record.Release.SignatureKeyID = strings.TrimSpace(record.Release.SignatureKeyID)
	record.Release.SignatureBase64 = strings.TrimSpace(record.Release.SignatureBase64)
	record.Release.Status = strings.ToLower(strings.TrimSpace(record.Release.Status))
	record.Release.PublishedAt = record.Release.PublishedAt.UTC()
	manifest, err := NormalizeManifest(record.Release.Manifest)
	if err != nil {
		return ReleaseControl{}, err
	}
	record.Release.Manifest = manifest
	record.WithdrawalReasonCode = strings.ToLower(strings.TrimSpace(record.WithdrawalReasonCode))
	record.WithdrawnAt = record.WithdrawnAt.UTC()
	record.CreatedAt = record.CreatedAt.UTC()
	record.UpdatedAt = record.UpdatedAt.UTC()
	if record.Release.ContractVersion != ReleaseContractVersion ||
		record.Release.ReleaseID != StableReleaseID(manifest.PublisherID, manifest.PackageID, manifest.Version) ||
		record.Release.SignatureKeyID == "" || record.Release.SignatureBase64 == "" ||
		record.Release.PublishedAt.IsZero() || record.Revision < 1 || record.PublishedBy == 0 ||
		record.CreatedAt.IsZero() || record.UpdatedAt.IsZero() || record.UpdatedAt.Before(record.CreatedAt) {
		return ReleaseControl{}, fmt.Errorf("%w: release metadata is incomplete", ErrInvalidControlRecord)
	}
	switch record.Release.Status {
	case ReleasePublished:
		if record.WithdrawnBy != 0 || record.WithdrawalReasonCode != "" || !record.WithdrawnAt.IsZero() {
			return ReleaseControl{}, fmt.Errorf("%w: published release contains withdrawal metadata", ErrInvalidControlRecord)
		}
	case ReleaseWithdrawn:
		if record.WithdrawnBy == 0 || !validWithdrawalReason(record.WithdrawalReasonCode) || record.WithdrawnAt.IsZero() {
			return ReleaseControl{}, fmt.Errorf("%w: withdrawal metadata is incomplete", ErrInvalidControlRecord)
		}
	default:
		return ReleaseControl{}, fmt.Errorf("%w: unsupported release status", ErrInvalidControlRecord)
	}
	return record, nil
}

func RotatePublisherKey(record PublisherControl, key SigningKey, actorUserID uint64, now time.Time) (PublisherControl, error) {
	normalized, err := NormalizePublisherControl(record)
	if err != nil {
		return PublisherControl{}, err
	}
	key.Status = KeyActive
	key, err = NormalizeSigningKey(key)
	if err != nil {
		return PublisherControl{}, err
	}
	if actorUserID == 0 || now.IsZero() || len(normalized.Publisher.SigningKeys) >= maxPublisherSigningKeys {
		return PublisherControl{}, fmt.Errorf("%w: key rotation input is invalid", ErrInvalidControlRecord)
	}
	for index := range normalized.Publisher.SigningKeys {
		if normalized.Publisher.SigningKeys[index].KeyID == key.KeyID {
			return PublisherControl{}, fmt.Errorf("%w: duplicate signing key", ErrInvalidPublisher)
		}
		if normalized.Publisher.SigningKeys[index].Status == KeyActive {
			normalized.Publisher.SigningKeys[index].Status = KeyRetired
		}
	}
	normalized.Publisher.SigningKeys = append(normalized.Publisher.SigningKeys, key)
	normalized.Revision++
	normalized.UpdatedBy = actorUserID
	normalized.UpdatedAt = now.UTC()
	return NormalizePublisherControl(normalized)
}

func RevokePublisherKey(record PublisherControl, keyID string, actorUserID uint64, now time.Time) (PublisherControl, error) {
	normalized, err := NormalizePublisherControl(record)
	if err != nil {
		return PublisherControl{}, err
	}
	keyID = strings.TrimSpace(keyID)
	if actorUserID == 0 || now.IsZero() || keyID == "" {
		return PublisherControl{}, fmt.Errorf("%w: key revocation input is invalid", ErrInvalidControlRecord)
	}
	found := false
	for index := range normalized.Publisher.SigningKeys {
		if normalized.Publisher.SigningKeys[index].KeyID != keyID {
			continue
		}
		found = true
		if normalized.Publisher.SigningKeys[index].Status == KeyRevoked {
			return PublisherControl{}, fmt.Errorf("%w: signing key is already revoked", ErrRevisionConflict)
		}
		normalized.Publisher.SigningKeys[index].Status = KeyRevoked
	}
	if !found {
		return PublisherControl{}, fmt.Errorf("%w: signing key not found", ErrPublisherNotFound)
	}
	normalized.Revision++
	normalized.UpdatedBy = actorUserID
	normalized.UpdatedAt = now.UTC()
	return NormalizePublisherControl(normalized)
}

func SetPublisherVerification(record PublisherControl, verification string, actorUserID uint64, now time.Time) (PublisherControl, error) {
	normalized, err := NormalizePublisherControl(record)
	if err != nil {
		return PublisherControl{}, err
	}
	verification = strings.ToLower(strings.TrimSpace(verification))
	if actorUserID == 0 || now.IsZero() || (verification != PublisherVerified && verification != PublisherSuspended) {
		return PublisherControl{}, fmt.Errorf("%w: verification transition is invalid", ErrInvalidControlRecord)
	}
	if normalized.Publisher.Verification == verification {
		return PublisherControl{}, fmt.Errorf("%w: verification state is unchanged", ErrRevisionConflict)
	}
	normalized.Publisher.Verification = verification
	if verification == PublisherVerified {
		normalized.Publisher.VerifiedAt = now.UTC()
	}
	normalized.Revision++
	normalized.UpdatedBy = actorUserID
	normalized.UpdatedAt = now.UTC()
	return NormalizePublisherControl(normalized)
}

func WithdrawRelease(record ReleaseControl, reasonCode string, actorUserID uint64, now time.Time) (ReleaseControl, error) {
	normalized, err := NormalizeReleaseControl(record)
	if err != nil {
		return ReleaseControl{}, err
	}
	reasonCode = strings.ToLower(strings.TrimSpace(reasonCode))
	if normalized.Release.Status != ReleasePublished {
		return ReleaseControl{}, fmt.Errorf("%w: release is not published", ErrRevisionConflict)
	}
	if actorUserID == 0 || now.IsZero() || !validWithdrawalReason(reasonCode) {
		return ReleaseControl{}, ErrInvalidWithdrawal
	}
	normalized.Release.Status = ReleaseWithdrawn
	normalized.Revision++
	normalized.WithdrawnBy = actorUserID
	normalized.WithdrawalReasonCode = reasonCode
	normalized.WithdrawnAt = now.UTC()
	normalized.UpdatedAt = now.UTC()
	return NormalizeReleaseControl(normalized)
}

func NormalizeAuditEvent(event AuditEvent) (AuditEvent, error) {
	event.EventID = strings.TrimSpace(event.EventID)
	event.OperationID = strings.TrimSpace(event.OperationID)
	event.Action = strings.ToLower(strings.TrimSpace(event.Action))
	event.Outcome = strings.ToLower(strings.TrimSpace(event.Outcome))
	event.PublisherID = strings.ToLower(strings.TrimSpace(event.PublisherID))
	event.PackageID = strings.ToLower(strings.TrimSpace(event.PackageID))
	event.Version = strings.TrimSpace(event.Version)
	event.KeyID = strings.TrimSpace(event.KeyID)
	event.ReasonCode = strings.ToLower(strings.TrimSpace(event.ReasonCode))
	event.ErrorCode = strings.ToLower(strings.TrimSpace(event.ErrorCode))
	event.CreatedAt = event.CreatedAt.UTC()
	if event.EventID == "" || event.OperationID == "" || event.ActorUserID == 0 ||
		!validStableID(event.PublisherID) || event.CreatedAt.IsZero() || !validAuditAction(event.Action) ||
		(event.Outcome != AuditOutcomeRequested && event.Outcome != AuditOutcomeSucceeded && event.Outcome != AuditOutcomeFailed) {
		return AuditEvent{}, ErrInvalidAuditEvent
	}
	return event, nil
}

func PublisherOwnedBy(record PublisherControl, userID uint64) bool {
	if userID == 0 {
		return false
	}
	index := sort.Search(len(record.OwnerUserIDs), func(index int) bool { return record.OwnerUserIDs[index] >= userID })
	return index < len(record.OwnerUserIDs) && record.OwnerUserIDs[index] == userID
}

func normalizeOwnerUserIDs(values []uint64) ([]uint64, error) {
	if len(values) == 0 || len(values) > MaxPublisherOwners {
		return nil, fmt.Errorf("%w: publisher owners are invalid", ErrInvalidControlRecord)
	}
	seen := make(map[uint64]struct{}, len(values))
	result := make([]uint64, 0, len(values))
	for _, value := range values {
		if value == 0 {
			return nil, fmt.Errorf("%w: publisher owner is invalid", ErrInvalidControlRecord)
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result, nil
}

func validWithdrawalReason(value string) bool {
	switch value {
	case WithdrawalReasonSecurity, WithdrawalReasonPublisher, WithdrawalReasonPolicy,
		WithdrawalReasonSuperseded, WithdrawalReasonArtifactBroken:
		return true
	default:
		return false
	}
}

func validAuditAction(value string) bool {
	switch value {
	case AuditActionRegisterPublisher, AuditActionRotateKey, AuditActionRevokeKey,
		AuditActionSetVerification, AuditActionPublishRelease, AuditActionWithdrawRelease:
		return true
	default:
		return false
	}
}
