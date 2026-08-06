package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"twitter-clone/internal/module/agent/profile"
	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

// ProfileCatalogManager owns the mutable management path. Runtime requests only
// see the AtomicResolver and never query MongoDB.
type ProfileCatalogManager struct {
	repository              repository.ProfileCatalogStore
	approvalRepository      repository.ProfilePublishApprovalRepository
	resolver                *profile.AtomicResolver
	overrideReleases        []profile.Release
	changePublisher         profile.CatalogChangePublisher
	qualityEvidenceVerifier profile.QualityEvidenceVerifier
	qualityEvidenceRequired bool
	reloadMu                sync.Mutex
}

type ProfileCatalogManagerOption func(*ProfileCatalogManager) error

func WithProfileCatalogChangePublisher(publisher profile.CatalogChangePublisher) ProfileCatalogManagerOption {
	return func(manager *ProfileCatalogManager) error {
		if publisher == nil {
			return errors.New("profile catalog change publisher is required")
		}
		manager.changePublisher = publisher
		return nil
	}
}

func WithProfileQualityEvidenceVerifier(verifier profile.QualityEvidenceVerifier, required bool) ProfileCatalogManagerOption {
	return func(manager *ProfileCatalogManager) error {
		if verifier == nil {
			return errors.New("profile quality evidence verifier is required")
		}
		manager.qualityEvidenceVerifier = verifier
		manager.qualityEvidenceRequired = required
		return nil
	}
}

func NewProfileCatalogManager(
	repo repository.ProfileCatalogStore,
	resolver *profile.AtomicResolver,
	overrideReleases []profile.Release,
	options ...ProfileCatalogManagerOption,
) (*ProfileCatalogManager, error) {
	if repo == nil {
		return nil, errors.New("profile catalog repository is required")
	}
	if resolver == nil {
		return nil, errors.New("atomic profile resolver is required")
	}
	manager := &ProfileCatalogManager{
		repository:       repo,
		resolver:         resolver,
		overrideReleases: append([]profile.Release(nil), overrideReleases...),
	}
	manager.approvalRepository, _ = repo.(repository.ProfilePublishApprovalRepository)
	for _, option := range options {
		if option == nil {
			continue
		}
		if err := option(manager); err != nil {
			return nil, err
		}
	}
	return manager, nil
}

// CreateDraft validates and persists one immutable version without activating
// it. A release can only refer to versions that have subsequently been
// published.
func (m *ProfileCatalogManager) CreateDraft(
	ctx context.Context,
	candidate profile.AgentProfile,
	createdBy uint64,
) (*repository.ProfileVersionRecord, error) {
	if createdBy == 0 {
		return nil, errors.New("profile draft creator is required")
	}
	candidate = canonicalProfile(candidate)
	if !candidate.Budget.Deadline.IsZero() {
		return nil, errors.New("profile budget deadline must be assigned per run")
	}
	if _, err := agentRuntime.NewBudgetTracker(candidate.Budget); err != nil {
		return nil, fmt.Errorf("invalid profile budget: %w", err)
	}
	if _, err := NewBuiltInProfileCatalog([]profile.AgentProfile{candidate}, nil, nil); err != nil {
		return nil, fmt.Errorf("invalid profile draft: %w", err)
	}
	snapshot, err := encodeProfileSnapshotV1(candidate)
	if err != nil {
		return nil, err
	}
	record := &repository.ProfileVersionRecord{
		ProfileID:    candidate.ID,
		Version:      candidate.Version,
		SnapshotJSON: snapshot,
		CreatedBy:    createdBy,
	}
	operationID, err := newProfileOperationID()
	if err != nil {
		return nil, err
	}
	audit := repository.ProfileAuditEvent{
		OperationID:  operationID,
		Action:       repository.ProfileAuditActionCreateDraft,
		ProfileID:    candidate.ID,
		Version:      candidate.Version,
		ActorUserID:  createdBy,
		SnapshotHash: repository.ProfileSnapshotHash(snapshot),
	}
	if err := m.appendProfileAudit(ctx, audit, repository.ProfileAuditOutcomeRequested, ""); err != nil {
		return nil, fmt.Errorf("profile draft audit failed before mutation: %w", err)
	}
	if err := m.repository.CreateProfileVersion(ctx, record); err != nil {
		return nil, m.finishFailedProfileMutation(ctx, audit, err)
	}
	audit.VersionRevision = record.Revision
	audit.SnapshotHash = record.SnapshotHash
	if err := m.appendProfileAudit(ctx, audit, repository.ProfileAuditOutcomeSucceeded, ""); err != nil {
		return record, fmt.Errorf("profile draft was stored but final audit failed: %w", err)
	}
	return record, nil
}

// PublishVersion performs the draft-to-published CAS and then attempts to
// activate a complete snapshot. If activation fails, the database mutation is
// intentionally retained and the previous runtime catalog remains active.
func (m *ProfileCatalogManager) PublishVersion(
	ctx context.Context,
	profileID, version string,
	expectedRevision int64,
	publishedBy uint64,
) error {
	if publishedBy == 0 {
		return errors.New("profile publisher is required")
	}
	record, err := m.repository.GetProfileVersion(ctx, profileID, version)
	if err != nil {
		return err
	}
	if _, err := decodeProfileVersion(record); err != nil {
		return fmt.Errorf("profile version cannot be published: %w", err)
	}
	operationID, err := newProfileOperationID()
	if err != nil {
		return err
	}
	audit := repository.ProfileAuditEvent{
		OperationID:     operationID,
		Action:          repository.ProfileAuditActionPublishVersion,
		ProfileID:       strings.TrimSpace(profileID),
		Version:         strings.TrimSpace(version),
		ActorUserID:     publishedBy,
		VersionRevision: expectedRevision,
		SnapshotHash:    record.SnapshotHash,
	}
	if err := m.appendProfileAudit(ctx, audit, repository.ProfileAuditOutcomeRequested, ""); err != nil {
		return fmt.Errorf("profile publish audit failed before mutation: %w", err)
	}
	if err := m.repository.PublishProfileVersion(
		ctx,
		profileID,
		version,
		expectedRevision,
		publishedBy,
	); err != nil {
		return m.finishFailedProfileMutation(ctx, audit, err)
	}
	audit.VersionRevision = expectedRevision + 1
	if err := m.Reload(ctx); err != nil {
		activationErr := fmt.Errorf("profile version was published but catalog activation failed: %w", err)
		if auditErr := m.appendProfileAudit(ctx, audit, repository.ProfileAuditOutcomeActivationFailed, "catalog_activation_failed"); auditErr != nil {
			return errors.Join(activationErr, fmt.Errorf("final profile audit failed: %w", auditErr))
		}
		return activationErr
	}
	if err := m.publishProfileChange(ctx, audit); err != nil {
		propagationErr := fmt.Errorf("profile version was activated locally but change notification failed: %w", err)
		if auditErr := m.appendProfileAudit(ctx, audit, repository.ProfileAuditOutcomePropagationFailed, "change_notification_failed"); auditErr != nil {
			return errors.Join(propagationErr, fmt.Errorf("final profile audit failed: %w", auditErr))
		}
		return propagationErr
	}
	if err := m.appendProfileAudit(ctx, audit, repository.ProfileAuditOutcomeSucceeded, ""); err != nil {
		return fmt.Errorf("profile version was published and activated but final audit failed: %w", err)
	}
	return nil
}

// UpsertRelease validates a proposed release against all published versions,
// persists it with optimistic concurrency, and atomically activates the result.
func (m *ProfileCatalogManager) UpsertRelease(
	ctx context.Context,
	release profile.Release,
	expectedRevision int64,
	updatedBy uint64,
) (*repository.ProfileReleaseRecord, error) {
	if updatedBy == 0 {
		return nil, errors.New("profile release updater is required")
	}
	release = canonicalRelease(release)
	snapshot, err := m.repository.LoadPublishedProfileCatalog(ctx)
	if err != nil {
		return nil, err
	}
	profiles, releases, err := decodeProfileCatalogSnapshot(snapshot)
	if err != nil {
		return nil, err
	}
	releases = replaceRelease(releases, release)
	if _, err := buildValidatedProfileCatalog(profiles, releases, nil); err != nil {
		return nil, fmt.Errorf("invalid profile release: %w", err)
	}
	if _, err := buildValidatedProfileCatalog(profiles, releases, m.overrideReleases); err != nil {
		return nil, fmt.Errorf("profile release conflicts with environment override: %w", err)
	}

	record := &repository.ProfileReleaseRecord{
		ProfileID:            release.ProfileID,
		StableVersion:        release.StableVersion,
		CandidateVersion:     release.CandidateVersion,
		CandidateBasisPoints: release.CandidateBasisPoints,
		Salt:                 release.Salt,
		CreatedBy:            updatedBy,
		UpdatedBy:            updatedBy,
	}
	operationID, err := newProfileOperationID()
	if err != nil {
		return nil, err
	}
	audit := repository.ProfileAuditEvent{
		OperationID:     operationID,
		Action:          repository.ProfileAuditActionUpsertRelease,
		ProfileID:       release.ProfileID,
		ActorUserID:     updatedBy,
		ReleaseRevision: expectedRevision,
	}
	if err := m.appendProfileAudit(ctx, audit, repository.ProfileAuditOutcomeRequested, ""); err != nil {
		return nil, fmt.Errorf("profile release audit failed before mutation: %w", err)
	}
	if err := m.repository.UpsertProfileRelease(ctx, record, expectedRevision); err != nil {
		return nil, m.finishFailedProfileMutation(ctx, audit, err)
	}
	audit.ReleaseRevision = record.Revision
	if err := m.Reload(ctx); err != nil {
		activationErr := fmt.Errorf("profile release was stored but catalog activation failed: %w", err)
		if auditErr := m.appendProfileAudit(ctx, audit, repository.ProfileAuditOutcomeActivationFailed, "catalog_activation_failed"); auditErr != nil {
			return record, errors.Join(activationErr, fmt.Errorf("final profile audit failed: %w", auditErr))
		}
		return record, activationErr
	}
	if err := m.publishProfileChange(ctx, audit); err != nil {
		propagationErr := fmt.Errorf("profile release was activated locally but change notification failed: %w", err)
		if auditErr := m.appendProfileAudit(ctx, audit, repository.ProfileAuditOutcomePropagationFailed, "change_notification_failed"); auditErr != nil {
			return record, errors.Join(propagationErr, fmt.Errorf("final profile audit failed: %w", auditErr))
		}
		return record, propagationErr
	}
	if err := m.appendProfileAudit(ctx, audit, repository.ProfileAuditOutcomeSucceeded, ""); err != nil {
		return record, fmt.Errorf("profile release was stored and activated but final audit failed: %w", err)
	}
	return record, nil
}

func (m *ProfileCatalogManager) GetVersion(ctx context.Context, profileID, version string) (*repository.ProfileVersionRecord, error) {
	return m.repository.GetProfileVersion(ctx, profileID, version)
}

func (m *ProfileCatalogManager) ListVersions(ctx context.Context, profileID string, page, pageSize int) ([]*repository.ProfileVersionRecord, int64, error) {
	return m.repository.ListProfileVersions(ctx, profileID, page, pageSize)
}

func (m *ProfileCatalogManager) GetRelease(ctx context.Context, profileID string) (*repository.ProfileReleaseRecord, error) {
	return m.repository.GetProfileRelease(ctx, profileID)
}

func (m *ProfileCatalogManager) IsReleaseOverridden(profileID string) bool {
	if m == nil {
		return false
	}
	profileID = strings.TrimSpace(profileID)
	for _, release := range m.overrideReleases {
		if strings.TrimSpace(release.ProfileID) == profileID {
			return true
		}
	}
	return false
}

func (m *ProfileCatalogManager) ListAuditEvents(ctx context.Context, profileID string, page, pageSize int) ([]*repository.ProfileAuditEvent, int64, error) {
	return m.repository.ListProfileAuditEvents(ctx, profileID, page, pageSize)
}

func (m *ProfileCatalogManager) DecodeVersion(record *repository.ProfileVersionRecord) (profile.AgentProfile, error) {
	return decodeProfileVersion(record)
}

func (m *ProfileCatalogManager) appendProfileAudit(
	ctx context.Context,
	base repository.ProfileAuditEvent,
	outcome, errorCode string,
) error {
	base.Outcome = outcome
	base.ErrorCode = errorCode
	return m.repository.AppendProfileAuditEvent(ctx, &base)
}

func (m *ProfileCatalogManager) finishFailedProfileMutation(
	ctx context.Context,
	audit repository.ProfileAuditEvent,
	mutationErr error,
) error {
	if auditErr := m.appendProfileAudit(ctx, audit, repository.ProfileAuditOutcomeFailed, profileMutationErrorCode(mutationErr)); auditErr != nil {
		return errors.Join(mutationErr, fmt.Errorf("final profile audit failed: %w", auditErr))
	}
	return mutationErr
}

func (m *ProfileCatalogManager) publishProfileChange(ctx context.Context, audit repository.ProfileAuditEvent) error {
	if m.changePublisher == nil {
		return nil
	}
	return m.changePublisher.PublishCatalogChange(ctx, profile.CatalogChangeEvent{
		Schema:               profile.CatalogChangeSchemaV1,
		OperationID:          audit.OperationID,
		ProfileID:            audit.ProfileID,
		VersionRevision:      audit.VersionRevision,
		ReleaseRevision:      audit.ReleaseRevision,
		OccurredAtUnixMillis: time.Now().UnixMilli(),
	})
}

func newProfileOperationID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate profile operation id: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}

func profileMutationErrorCode(err error) string {
	switch {
	case errors.Is(err, repository.ErrProfileVersionNotFound), errors.Is(err, repository.ErrProfileReleaseNotFound), errors.Is(err, repository.ErrProfilePublishApprovalNotFound):
		return "not_found"
	case errors.Is(err, repository.ErrProfileVersionConflict), errors.Is(err, repository.ErrProfileReleaseConflict), errors.Is(err, repository.ErrProfilePublishApprovalConflict):
		return "revision_conflict"
	case errors.Is(err, repository.ErrProfilePublishSelfApproval):
		return "self_approval_forbidden"
	default:
		return "persistence_failed"
	}
}

// Reload constructs and validates the entire next generation before swapping
// it into the lock-free request path.
func (m *ProfileCatalogManager) Reload(ctx context.Context) error {
	if m == nil || m.repository == nil || m.resolver == nil {
		return errors.New("profile catalog manager is not configured")
	}
	m.reloadMu.Lock()
	defer m.reloadMu.Unlock()

	snapshot, err := m.repository.LoadPublishedProfileCatalog(ctx)
	if err != nil {
		return err
	}
	profiles, releases, err := decodeProfileCatalogSnapshot(snapshot)
	if err != nil {
		return err
	}
	if _, err := buildValidatedProfileCatalog(profiles, releases, nil); err != nil {
		return fmt.Errorf("build persisted profile catalog: %w", err)
	}
	next, err := buildValidatedProfileCatalog(profiles, releases, m.overrideReleases)
	if err != nil {
		return fmt.Errorf("build profile catalog: %w", err)
	}
	return m.resolver.Replace(next)
}

type profileSnapshotV1 struct {
	ID           string                  `json:"id"`
	Version      string                  `json:"version"`
	Prompt       promptSnapshotV1        `json:"prompt"`
	Budget       profileBudgetSnapshotV1 `json:"budget"`
	AllowedTools []string                `json:"allowed_tools"`
}

type promptSnapshotV1 struct {
	ID           string `json:"id"`
	Version      string `json:"version"`
	SystemPrompt string `json:"system_prompt"`
}

type profileBudgetSnapshotV1 struct {
	MaxSteps               int    `json:"max_steps"`
	MaxInputTokens         int    `json:"max_input_tokens"`
	MaxOutputTokens        int    `json:"max_output_tokens"`
	MaxTotalTokens         int    `json:"max_total_tokens"`
	MaxEstimatedCostMicros int64  `json:"max_estimated_cost_micros"`
	Timeout                string `json:"timeout,omitempty"`
}

func encodeProfileSnapshotV1(candidate profile.AgentProfile) (string, error) {
	timeout := ""
	if candidate.Budget.Timeout != 0 {
		timeout = candidate.Budget.Timeout.String()
	}
	snapshot := profileSnapshotV1{
		ID:      candidate.ID,
		Version: candidate.Version,
		Prompt: promptSnapshotV1{
			ID:           candidate.Prompt.ID,
			Version:      candidate.Prompt.Version,
			SystemPrompt: candidate.Prompt.SystemPrompt,
		},
		Budget: profileBudgetSnapshotV1{
			MaxSteps:               candidate.Budget.MaxSteps,
			MaxInputTokens:         candidate.Budget.MaxInputTokens,
			MaxOutputTokens:        candidate.Budget.MaxOutputTokens,
			MaxTotalTokens:         candidate.Budget.MaxTotalTokens,
			MaxEstimatedCostMicros: candidate.Budget.MaxEstimatedCostMicros,
			Timeout:                timeout,
		},
		AllowedTools: append([]string(nil), candidate.AllowedTools...),
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return "", fmt.Errorf("encode profile snapshot: %w", err)
	}
	return string(encoded), nil
}

func decodeProfileVersion(record *repository.ProfileVersionRecord) (profile.AgentProfile, error) {
	if record == nil {
		return profile.AgentProfile{}, errors.New("profile version record is required")
	}
	if record.SnapshotSchema != repository.ProfileSnapshotSchemaV1 {
		return profile.AgentProfile{}, fmt.Errorf("unsupported profile snapshot schema %q", record.SnapshotSchema)
	}
	if err := record.VerifySnapshot(); err != nil {
		return profile.AgentProfile{}, err
	}

	decoder := json.NewDecoder(bytes.NewBufferString(record.SnapshotJSON))
	decoder.DisallowUnknownFields()
	var snapshot profileSnapshotV1
	if err := decoder.Decode(&snapshot); err != nil {
		return profile.AgentProfile{}, fmt.Errorf("decode profile snapshot: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return profile.AgentProfile{}, errors.New("decode profile snapshot: multiple JSON values are not allowed")
		}
		return profile.AgentProfile{}, fmt.Errorf("decode profile snapshot: %w", err)
	}

	timeout := time.Duration(0)
	if raw := strings.TrimSpace(snapshot.Budget.Timeout); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed < 0 {
			return profile.AgentProfile{}, fmt.Errorf("invalid profile timeout %q", raw)
		}
		timeout = parsed
	}
	candidate := canonicalProfile(profile.AgentProfile{
		ID:      snapshot.ID,
		Version: snapshot.Version,
		Prompt: profile.PromptProfile{
			ID:           snapshot.Prompt.ID,
			Version:      snapshot.Prompt.Version,
			SystemPrompt: snapshot.Prompt.SystemPrompt,
		},
		Budget: agentRuntime.Budget{
			MaxSteps:               snapshot.Budget.MaxSteps,
			MaxInputTokens:         snapshot.Budget.MaxInputTokens,
			MaxOutputTokens:        snapshot.Budget.MaxOutputTokens,
			MaxTotalTokens:         snapshot.Budget.MaxTotalTokens,
			MaxEstimatedCostMicros: snapshot.Budget.MaxEstimatedCostMicros,
			Timeout:                timeout,
		},
		AllowedTools: snapshot.AllowedTools,
	})
	if candidate.ID != strings.TrimSpace(record.ProfileID) || candidate.Version != strings.TrimSpace(record.Version) {
		return profile.AgentProfile{}, fmt.Errorf(
			"profile snapshot identity %q@%q does not match record %q@%q",
			candidate.ID,
			candidate.Version,
			record.ProfileID,
			record.Version,
		)
	}
	if _, err := agentRuntime.NewBudgetTracker(candidate.Budget); err != nil {
		return profile.AgentProfile{}, fmt.Errorf("invalid profile budget: %w", err)
	}
	if _, err := profile.NewCatalog([]profile.AgentProfile{candidate}, nil); err != nil {
		return profile.AgentProfile{}, err
	}
	return candidate, nil
}

func decodeProfileCatalogSnapshot(
	snapshot *repository.ProfileCatalogSnapshot,
) ([]profile.AgentProfile, []profile.Release, error) {
	if snapshot == nil {
		return nil, nil, errors.New("profile catalog snapshot is required")
	}
	profiles := make([]profile.AgentProfile, 0, len(snapshot.Versions))
	for _, record := range snapshot.Versions {
		if record == nil || record.Status != repository.ProfileVersionStatusPublished {
			return nil, nil, errors.New("profile catalog contains a non-published version")
		}
		candidate, err := decodeProfileVersion(record)
		if err != nil {
			return nil, nil, fmt.Errorf("decode persisted profile: %w", err)
		}
		profiles = append(profiles, candidate)
	}
	releases := make([]profile.Release, 0, len(snapshot.Releases))
	for _, record := range snapshot.Releases {
		if record == nil {
			return nil, nil, errors.New("profile catalog contains an empty release")
		}
		releases = append(releases, canonicalRelease(profile.Release{
			ProfileID:            record.ProfileID,
			StableVersion:        record.StableVersion,
			CandidateVersion:     record.CandidateVersion,
			CandidateBasisPoints: record.CandidateBasisPoints,
			Salt:                 record.Salt,
		}))
	}
	return profiles, releases, nil
}

func ensureCatalogResolvable(catalog *profile.Catalog, persisted []profile.AgentProfile) error {
	versions := make(map[string]map[string]struct{}, len(persisted)+len(builtInAgentProfiles()))
	for _, candidate := range append(builtInAgentProfiles(), persisted...) {
		profileID := strings.TrimSpace(candidate.ID)
		profileVersions := versions[profileID]
		if profileVersions == nil {
			profileVersions = make(map[string]struct{})
			versions[profileID] = profileVersions
		}
		profileVersions[strings.TrimSpace(candidate.Version)] = struct{}{}
	}
	for profileID, profileVersions := range versions {
		if isCoordinatedProfileSetMember(profileID) {
			for version := range profileVersions {
				if _, err := catalog.ResolveVersion(context.Background(), profileID, version); err != nil {
					return fmt.Errorf("profile catalog cannot resolve set member %q@%q: %w", profileID, version, err)
				}
			}
			continue
		}
		if _, err := catalog.Resolve(context.Background(), profileID, profile.SelectionSubject{UserID: 1}); err != nil {
			return fmt.Errorf("profile catalog cannot resolve %q: %w", profileID, err)
		}
	}
	return nil
}

func buildValidatedProfileCatalog(
	profiles []profile.AgentProfile,
	releases []profile.Release,
	overrides []profile.Release,
) (*profile.Catalog, error) {
	catalog, err := NewBuiltInProfileCatalog(profiles, releases, overrides)
	if err != nil {
		return nil, err
	}
	if err := ensureCatalogResolvable(catalog, profiles); err != nil {
		return nil, err
	}
	return catalog, nil
}

func replaceRelease(releases []profile.Release, replacement profile.Release) []profile.Release {
	next := make([]profile.Release, 0, len(releases)+1)
	replaced := false
	for _, current := range releases {
		if strings.TrimSpace(current.ProfileID) == replacement.ProfileID {
			if !replaced {
				next = append(next, replacement)
				replaced = true
			}
			continue
		}
		next = append(next, current)
	}
	if !replaced {
		next = append(next, replacement)
	}
	return next
}

func canonicalProfile(candidate profile.AgentProfile) profile.AgentProfile {
	candidate.ID = strings.TrimSpace(candidate.ID)
	candidate.Version = strings.TrimSpace(candidate.Version)
	candidate.Prompt.ID = strings.TrimSpace(candidate.Prompt.ID)
	candidate.Prompt.Version = strings.TrimSpace(candidate.Prompt.Version)
	candidate.AllowedTools = append([]string(nil), candidate.AllowedTools...)
	for index := range candidate.AllowedTools {
		candidate.AllowedTools[index] = strings.TrimSpace(candidate.AllowedTools[index])
	}
	return candidate
}

func canonicalRelease(release profile.Release) profile.Release {
	release.ProfileID = strings.TrimSpace(release.ProfileID)
	release.StableVersion = strings.TrimSpace(release.StableVersion)
	release.CandidateVersion = strings.TrimSpace(release.CandidateVersion)
	release.Salt = strings.TrimSpace(release.Salt)
	return release
}
