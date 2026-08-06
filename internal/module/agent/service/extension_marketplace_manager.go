package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"twitter-clone/internal/module/agent/marketplace"
)

type ExtensionMarketplaceAccess struct {
	Enabled           bool
	PlatformAdmin     bool
	OwnedPublisherIDs []string
}

type RegisterExtensionPublisherRequest struct {
	PublisherID     string
	DisplayName     string
	OwnerUserIDs    []uint64
	InitialKeyID    string
	PublicKeyBase64 string
}

type PublishExtensionReleaseRequest struct {
	Manifest                  marketplace.Manifest
	SignatureKeyID            string
	SignatureBase64           string
	ExpectedPublisherRevision int64
}

type ExtensionMarketplaceManagerOption func(*ExtensionMarketplaceManager)

func WithExtensionMarketplaceClock(clock func() time.Time) ExtensionMarketplaceManagerOption {
	return func(manager *ExtensionMarketplaceManager) {
		if clock != nil {
			manager.now = clock
		}
	}
}

func WithExtensionMarketplaceIDGenerator(generator func(string) (string, error)) ExtensionMarketplaceManagerOption {
	return func(manager *ExtensionMarketplaceManager) {
		if generator != nil {
			manager.newID = generator
		}
	}
}

// ExtensionMarketplaceManager is the authenticated write boundary for public
// extension metadata. It never accepts private keys, artifacts or installation
// grants and does not share the Agent Profile RBAC namespace.
type ExtensionMarketplaceManager struct {
	store          marketplace.ControlStore
	enabled        bool
	administrators map[uint64]struct{}
	now            func() time.Time
	newID          func(string) (string, error)
}

func NewExtensionMarketplaceManager(
	store marketplace.ControlStore,
	enabled bool,
	administratorUserIDs []uint64,
	options ...ExtensionMarketplaceManagerOption,
) *ExtensionMarketplaceManager {
	manager := &ExtensionMarketplaceManager{
		store: store, enabled: enabled, administrators: make(map[uint64]struct{}),
		now: time.Now, newID: newExtensionMarketplaceID,
	}
	for _, userID := range administratorUserIDs {
		if userID != 0 {
			manager.administrators[userID] = struct{}{}
		}
	}
	for _, option := range options {
		if option != nil {
			option(manager)
		}
	}
	return manager
}

func (m *ExtensionMarketplaceManager) ResolveAccess(ctx context.Context, actorUserID uint64) (ExtensionMarketplaceAccess, error) {
	if err := m.requireEnabled(actorUserID); err != nil {
		return ExtensionMarketplaceAccess{}, err
	}
	owned, err := m.store.ListOwnedPublisherIDs(ctx, actorUserID)
	if err != nil {
		return ExtensionMarketplaceAccess{}, fmt.Errorf("resolve extension publisher ownership: %w", err)
	}
	return ExtensionMarketplaceAccess{
		Enabled: true, PlatformAdmin: m.isAdministrator(actorUserID),
		OwnedPublisherIDs: append([]string(nil), owned...),
	}, nil
}

func (m *ExtensionMarketplaceManager) ListPublishers(
	ctx context.Context,
	actorUserID uint64,
	page marketplace.ManagementPage,
) ([]marketplace.PublisherControl, int64, error) {
	if err := m.requireEnabled(actorUserID); err != nil {
		return nil, 0, err
	}
	return m.store.ListPublisherControls(ctx, actorUserID, m.isAdministrator(actorUserID), page)
}

func (m *ExtensionMarketplaceManager) RegisterPublisher(
	ctx context.Context,
	actorUserID uint64,
	request RegisterExtensionPublisherRequest,
) (marketplace.PublisherControl, error) {
	if err := m.requireAdministrator(actorUserID); err != nil {
		return marketplace.PublisherControl{}, err
	}
	now := m.now().UTC()
	record, err := marketplace.NormalizePublisherControl(marketplace.PublisherControl{
		Publisher: marketplace.Publisher{
			ContractVersion: marketplace.PublisherContractVersion,
			PublisherID:     request.PublisherID, DisplayName: request.DisplayName,
			Verification: marketplace.PublisherVerified, VerifiedAt: now,
			SigningKeys: []marketplace.SigningKey{{
				KeyID: request.InitialKeyID, Algorithm: marketplace.SignatureAlgorithmEd25519,
				PublicKeyBase64: request.PublicKeyBase64, Status: marketplace.KeyActive,
			}},
		},
		OwnerUserIDs: request.OwnerUserIDs, Revision: 1,
		CreatedBy: actorUserID, UpdatedBy: actorUserID, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return marketplace.PublisherControl{}, err
	}
	audit, err := m.beginAudit(ctx, marketplace.AuditEvent{
		Action: marketplace.AuditActionRegisterPublisher, ActorUserID: actorUserID,
		PublisherID: record.Publisher.PublisherID, KeyID: record.Publisher.SigningKeys[0].KeyID,
	})
	if err != nil {
		return marketplace.PublisherControl{}, err
	}
	if err := m.store.CreatePublisher(ctx, record); err != nil {
		return marketplace.PublisherControl{}, m.finishFailedAudit(ctx, audit, err)
	}
	audit.Revision = record.Revision
	if err := m.finishAudit(ctx, audit, marketplace.AuditOutcomeSucceeded, ""); err != nil {
		return marketplace.PublisherControl{}, err
	}
	return record, nil
}

func (m *ExtensionMarketplaceManager) RotatePublisherKey(
	ctx context.Context,
	actorUserID uint64,
	publisherID string,
	key marketplace.SigningKey,
	expectedRevision int64,
) (marketplace.PublisherControl, error) {
	current, err := m.requirePublisherAccess(ctx, actorUserID, publisherID)
	if err != nil {
		return marketplace.PublisherControl{}, err
	}
	if current.Revision != expectedRevision {
		return marketplace.PublisherControl{}, marketplace.ErrRevisionConflict
	}
	updated, err := marketplace.RotatePublisherKey(current, key, actorUserID, m.now())
	if err != nil {
		return marketplace.PublisherControl{}, err
	}
	return m.updatePublisherWithAudit(ctx, current, updated, actorUserID, marketplace.AuditActionRotateKey, key.KeyID)
}

func (m *ExtensionMarketplaceManager) RevokePublisherKey(
	ctx context.Context,
	actorUserID uint64,
	publisherID, keyID string,
	expectedRevision int64,
) (marketplace.PublisherControl, error) {
	current, err := m.requirePublisherAccess(ctx, actorUserID, publisherID)
	if err != nil {
		return marketplace.PublisherControl{}, err
	}
	if current.Revision != expectedRevision {
		return marketplace.PublisherControl{}, marketplace.ErrRevisionConflict
	}
	updated, err := marketplace.RevokePublisherKey(current, keyID, actorUserID, m.now())
	if err != nil {
		return marketplace.PublisherControl{}, err
	}
	return m.updatePublisherWithAudit(ctx, current, updated, actorUserID, marketplace.AuditActionRevokeKey, keyID)
}

func (m *ExtensionMarketplaceManager) SetPublisherVerification(
	ctx context.Context,
	actorUserID uint64,
	publisherID, verification string,
	expectedRevision int64,
) (marketplace.PublisherControl, error) {
	if err := m.requireAdministrator(actorUserID); err != nil {
		return marketplace.PublisherControl{}, err
	}
	current, err := m.store.GetPublisherControl(ctx, publisherID)
	if err != nil {
		return marketplace.PublisherControl{}, err
	}
	if current.Revision != expectedRevision {
		return marketplace.PublisherControl{}, marketplace.ErrRevisionConflict
	}
	updated, err := marketplace.SetPublisherVerification(current, verification, actorUserID, m.now())
	if err != nil {
		return marketplace.PublisherControl{}, err
	}
	return m.updatePublisherWithAudit(ctx, current, updated, actorUserID, marketplace.AuditActionSetVerification, "")
}

func (m *ExtensionMarketplaceManager) ListReleases(
	ctx context.Context,
	actorUserID uint64,
	publisherID, releaseStatus string,
	page marketplace.ManagementPage,
) ([]marketplace.ReleaseControl, int64, error) {
	if err := m.requireEnabled(actorUserID); err != nil {
		return nil, 0, err
	}
	publisherIDs, err := m.authorizedPublisherFilter(ctx, actorUserID, publisherID)
	if err != nil {
		return nil, 0, err
	}
	return m.store.ListReleaseControls(ctx, publisherIDs, releaseStatus, page)
}

func (m *ExtensionMarketplaceManager) PublishRelease(
	ctx context.Context,
	actorUserID uint64,
	request PublishExtensionReleaseRequest,
) (marketplace.ReleaseControl, error) {
	publisher, err := m.requirePublisherAccess(ctx, actorUserID, request.Manifest.PublisherID)
	if err != nil {
		return marketplace.ReleaseControl{}, err
	}
	if publisher.Revision != request.ExpectedPublisherRevision {
		return marketplace.ReleaseControl{}, marketplace.ErrRevisionConflict
	}
	now := m.now().UTC()
	manifest, err := marketplace.NormalizeManifest(request.Manifest)
	if err != nil {
		return marketplace.ReleaseControl{}, err
	}
	release := marketplace.SignedRelease{
		ContractVersion: marketplace.ReleaseContractVersion,
		ReleaseID:       marketplace.StableReleaseID(manifest.PublisherID, manifest.PackageID, manifest.Version),
		Manifest:        manifest, SignatureKeyID: strings.TrimSpace(request.SignatureKeyID),
		SignatureBase64: strings.TrimSpace(request.SignatureBase64),
		Status:          marketplace.ReleasePublished, PublishedAt: now,
	}
	if _, err := marketplace.VerifyNewRelease(publisher.Publisher, release); err != nil {
		return marketplace.ReleaseControl{}, err
	}
	record, err := marketplace.NormalizeReleaseControl(marketplace.ReleaseControl{
		Release: release, Revision: 1, PublishedBy: actorUserID, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return marketplace.ReleaseControl{}, err
	}
	audit, err := m.beginAudit(ctx, marketplace.AuditEvent{
		Action: marketplace.AuditActionPublishRelease, ActorUserID: actorUserID,
		PublisherID: manifest.PublisherID, PackageID: manifest.PackageID,
		Version: manifest.Version, KeyID: release.SignatureKeyID,
	})
	if err != nil {
		return marketplace.ReleaseControl{}, err
	}
	if err := m.store.CreateRelease(ctx, record); err != nil {
		return marketplace.ReleaseControl{}, m.finishFailedAudit(ctx, audit, err)
	}
	audit.Revision = record.Revision
	if err := m.finishAudit(ctx, audit, marketplace.AuditOutcomeSucceeded, ""); err != nil {
		return marketplace.ReleaseControl{}, err
	}
	return record, nil
}

func (m *ExtensionMarketplaceManager) WithdrawRelease(
	ctx context.Context,
	actorUserID uint64,
	releaseID, reasonCode string,
	expectedRevision int64,
) (marketplace.ReleaseControl, error) {
	if err := m.requireEnabled(actorUserID); err != nil {
		return marketplace.ReleaseControl{}, err
	}
	current, err := m.store.GetReleaseControl(ctx, releaseID)
	if err != nil {
		return marketplace.ReleaseControl{}, err
	}
	if _, err := m.requirePublisherAccess(ctx, actorUserID, current.Release.Manifest.PublisherID); err != nil {
		return marketplace.ReleaseControl{}, err
	}
	if current.Revision != expectedRevision {
		return marketplace.ReleaseControl{}, marketplace.ErrRevisionConflict
	}
	updated, err := marketplace.WithdrawRelease(current, reasonCode, actorUserID, m.now())
	if err != nil {
		return marketplace.ReleaseControl{}, err
	}
	audit, err := m.beginAudit(ctx, marketplace.AuditEvent{
		Action: marketplace.AuditActionWithdrawRelease, ActorUserID: actorUserID,
		PublisherID: current.Release.Manifest.PublisherID,
		PackageID:   current.Release.Manifest.PackageID, Version: current.Release.Manifest.Version,
		KeyID: current.Release.SignatureKeyID, Revision: current.Revision, ReasonCode: reasonCode,
	})
	if err != nil {
		return marketplace.ReleaseControl{}, err
	}
	if err := m.store.UpdateReleaseControl(ctx, updated, current.Revision); err != nil {
		return marketplace.ReleaseControl{}, m.finishFailedAudit(ctx, audit, err)
	}
	audit.Revision = updated.Revision
	if err := m.finishAudit(ctx, audit, marketplace.AuditOutcomeSucceeded, ""); err != nil {
		return marketplace.ReleaseControl{}, err
	}
	return updated, nil
}

func (m *ExtensionMarketplaceManager) ListAuditEvents(
	ctx context.Context,
	actorUserID uint64,
	publisherID, action, outcome string,
	page marketplace.ManagementPage,
) ([]marketplace.AuditEvent, int64, error) {
	if err := m.requireEnabled(actorUserID); err != nil {
		return nil, 0, err
	}
	publisherIDs, err := m.authorizedPublisherFilter(ctx, actorUserID, publisherID)
	if err != nil {
		return nil, 0, err
	}
	return m.store.ListAuditEvents(ctx, publisherIDs, action, outcome, page)
}

func (m *ExtensionMarketplaceManager) updatePublisherWithAudit(
	ctx context.Context,
	current, updated marketplace.PublisherControl,
	actorUserID uint64,
	action, keyID string,
) (marketplace.PublisherControl, error) {
	audit, err := m.beginAudit(ctx, marketplace.AuditEvent{
		Action: action, ActorUserID: actorUserID, PublisherID: current.Publisher.PublisherID,
		KeyID: strings.TrimSpace(keyID), Revision: current.Revision,
	})
	if err != nil {
		return marketplace.PublisherControl{}, err
	}
	if err := m.store.UpdatePublisherControl(ctx, updated, current.Revision); err != nil {
		return marketplace.PublisherControl{}, m.finishFailedAudit(ctx, audit, err)
	}
	audit.Revision = updated.Revision
	if err := m.finishAudit(ctx, audit, marketplace.AuditOutcomeSucceeded, ""); err != nil {
		return marketplace.PublisherControl{}, err
	}
	return updated, nil
}

func (m *ExtensionMarketplaceManager) authorizedPublisherFilter(
	ctx context.Context,
	actorUserID uint64,
	publisherID string,
) ([]string, error) {
	publisherID = strings.ToLower(strings.TrimSpace(publisherID))
	if publisherID != "" {
		if _, err := m.requirePublisherAccess(ctx, actorUserID, publisherID); err != nil {
			return nil, err
		}
		return []string{publisherID}, nil
	}
	if m.isAdministrator(actorUserID) {
		return nil, nil
	}
	return m.store.ListOwnedPublisherIDs(ctx, actorUserID)
}

func (m *ExtensionMarketplaceManager) requirePublisherAccess(
	ctx context.Context,
	actorUserID uint64,
	publisherID string,
) (marketplace.PublisherControl, error) {
	if err := m.requireEnabled(actorUserID); err != nil {
		return marketplace.PublisherControl{}, err
	}
	record, err := m.store.GetPublisherControl(ctx, publisherID)
	if err != nil {
		return marketplace.PublisherControl{}, err
	}
	if !m.isAdministrator(actorUserID) && !marketplace.PublisherOwnedBy(record, actorUserID) {
		return marketplace.PublisherControl{}, marketplace.ErrControlForbidden
	}
	return record, nil
}

func (m *ExtensionMarketplaceManager) requireEnabled(actorUserID uint64) error {
	if m == nil || !m.enabled || m.store == nil {
		return marketplace.ErrControlDisabled
	}
	if actorUserID == 0 {
		return marketplace.ErrControlForbidden
	}
	return nil
}

func (m *ExtensionMarketplaceManager) requireAdministrator(actorUserID uint64) error {
	if err := m.requireEnabled(actorUserID); err != nil {
		return err
	}
	if !m.isAdministrator(actorUserID) {
		return marketplace.ErrControlForbidden
	}
	return nil
}

func (m *ExtensionMarketplaceManager) isAdministrator(userID uint64) bool {
	if m == nil || userID == 0 {
		return false
	}
	_, exists := m.administrators[userID]
	return exists
}

func (m *ExtensionMarketplaceManager) beginAudit(ctx context.Context, event marketplace.AuditEvent) (marketplace.AuditEvent, error) {
	operationID, err := m.newID("marketop")
	if err != nil {
		return marketplace.AuditEvent{}, err
	}
	event.OperationID = operationID
	if err := m.finishAudit(ctx, event, marketplace.AuditOutcomeRequested, ""); err != nil {
		return marketplace.AuditEvent{}, fmt.Errorf("initial extension marketplace audit failed: %w", err)
	}
	return event, nil
}

func (m *ExtensionMarketplaceManager) finishAudit(
	ctx context.Context,
	event marketplace.AuditEvent,
	outcome, errorCode string,
) error {
	eventID, err := m.newID("marketevt")
	if err != nil {
		return err
	}
	event.EventID = eventID
	event.Outcome = outcome
	event.ErrorCode = errorCode
	event.CreatedAt = m.now().UTC()
	return m.store.AppendAuditEvent(ctx, event)
}

func (m *ExtensionMarketplaceManager) finishFailedAudit(
	ctx context.Context,
	event marketplace.AuditEvent,
	mutationErr error,
) error {
	if auditErr := m.finishAudit(ctx, event, marketplace.AuditOutcomeFailed, extensionMarketplaceErrorCode(mutationErr)); auditErr != nil {
		return errors.Join(mutationErr, fmt.Errorf("final extension marketplace audit failed: %w", auditErr))
	}
	return mutationErr
}

func extensionMarketplaceErrorCode(err error) string {
	switch {
	case errors.Is(err, marketplace.ErrPublisherNotFound), errors.Is(err, marketplace.ErrReleaseNotFound):
		return "not_found"
	case errors.Is(err, marketplace.ErrPublisherConflict), errors.Is(err, marketplace.ErrReleaseConflict):
		return "identity_conflict"
	case errors.Is(err, marketplace.ErrRevisionConflict):
		return "revision_conflict"
	case errors.Is(err, marketplace.ErrSignatureVerification):
		return "signature_invalid"
	case errors.Is(err, marketplace.ErrInvalidPublisher), errors.Is(err, marketplace.ErrInvalidManifest),
		errors.Is(err, marketplace.ErrInvalidRelease), errors.Is(err, marketplace.ErrInvalidControlRecord),
		errors.Is(err, marketplace.ErrInvalidWithdrawal):
		return "invalid_input"
	default:
		return "persistence_failed"
	}
}

func newExtensionMarketplaceID(prefix string) (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate extension marketplace identifier: %w", err)
	}
	return strings.TrimSpace(prefix) + "_" + hex.EncodeToString(value[:]), nil
}
