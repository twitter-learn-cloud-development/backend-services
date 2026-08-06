package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"twitter-clone/internal/module/agent/profile"
	"twitter-clone/internal/module/agent/repository"
)

func TestProfileCatalogManagerDraftPublishAndReleaseLifecycle(t *testing.T) {
	ctx := context.Background()
	repo := newFakeProfileCatalogRepository()
	resolver := newTestAtomicProfileResolver(t, nil)
	manager, err := NewProfileCatalogManager(repo, resolver, nil)
	if err != nil {
		t.Fatalf("NewProfileCatalogManager() error = %v", err)
	}

	v1 := testManagedProfile("custom.writer", "v1")
	draft, err := manager.CreateDraft(ctx, v1, 42)
	if err != nil {
		t.Fatalf("CreateDraft(v1) error = %v", err)
	}
	if draft.Status != repository.ProfileVersionStatusDraft || draft.Revision != 1 {
		t.Fatalf("draft = %+v", draft)
	}
	if _, err := resolver.Resolve(ctx, v1.ID, profile.SelectionSubject{UserID: 7}); !errors.Is(err, profile.ErrProfileNotFound) {
		t.Fatalf("draft became visible, Resolve() error = %v", err)
	}

	if err := manager.PublishVersion(ctx, v1.ID, v1.Version, draft.Revision, 43); err != nil {
		t.Fatalf("PublishVersion(v1) error = %v", err)
	}
	selected, err := resolver.Resolve(ctx, v1.ID, profile.SelectionSubject{UserID: 7})
	if err != nil || selected.Version != "v1" {
		t.Fatalf("Resolve(v1) = %+v, %v", selected, err)
	}

	v2 := testManagedProfile("custom.writer", "v2")
	draftV2, err := manager.CreateDraft(ctx, v2, 42)
	if err != nil {
		t.Fatalf("CreateDraft(v2) error = %v", err)
	}
	err = manager.PublishVersion(ctx, v2.ID, v2.Version, draftV2.Revision, 43)
	if err == nil || !strings.Contains(err.Error(), "was published but catalog activation failed") {
		t.Fatalf("PublishVersion(v2) error = %v", err)
	}
	selected, err = resolver.Resolve(ctx, v1.ID, profile.SelectionSubject{UserID: 7})
	if err != nil || selected.Version != "v1" {
		t.Fatalf("failed reload replaced previous catalog: %+v, %v", selected, err)
	}

	storedRelease, err := manager.UpsertRelease(ctx, profile.Release{
		ProfileID:            v1.ID,
		StableVersion:        "v1",
		CandidateVersion:     "v2",
		CandidateBasisPoints: profile.MaxReleaseBasisPoints,
	}, 0, 44)
	if err != nil {
		t.Fatalf("UpsertRelease() error = %v", err)
	}
	if storedRelease.Revision != 1 {
		t.Fatalf("release revision = %d", storedRelease.Revision)
	}
	selected, err = resolver.Resolve(ctx, v1.ID, profile.SelectionSubject{UserID: 7})
	if err != nil || selected.Version != "v2" {
		t.Fatalf("Resolve(v2) = %+v, %v", selected, err)
	}
}

func TestProfileCatalogManagerRejectsCorruptReloadAndKeepsPreviousCatalog(t *testing.T) {
	ctx := context.Background()
	repo := newFakeProfileCatalogRepository()
	resolver := newTestAtomicProfileResolver(t, nil)
	manager, err := NewProfileCatalogManager(repo, resolver, nil)
	if err != nil {
		t.Fatalf("NewProfileCatalogManager() error = %v", err)
	}

	record := fakePublishedProfileRecord(t, testManagedProfile("custom.corrupt", "v1"))
	record.SnapshotJSON = strings.Replace(record.SnapshotJSON, "custom.corrupt", "tampered", 1)
	repo.versions[profileVersionKey(record.ProfileID, record.Version)] = record

	if err := manager.Reload(ctx); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("Reload() error = %v", err)
	}
	if _, err := resolver.Resolve(ctx, "assist.draft", profile.SelectionSubject{UserID: 7}); err != nil {
		t.Fatalf("previous built-in catalog was lost: %v", err)
	}
	if _, err := resolver.Resolve(ctx, "custom.corrupt", profile.SelectionSubject{UserID: 7}); !errors.Is(err, profile.ErrProfileNotFound) {
		t.Fatalf("corrupt profile became visible, error = %v", err)
	}
}

func TestProfileCatalogManagerEnvironmentReleaseOverridesPersistentRelease(t *testing.T) {
	ctx := context.Background()
	repo := newFakeProfileCatalogRepository()
	repo.releases[profileAssistDraft] = &repository.ProfileReleaseRecord{
		ProfileID:            profileAssistDraft,
		StableVersion:        "v1",
		CandidateVersion:     "v2",
		CandidateBasisPoints: profile.MaxReleaseBasisPoints,
		Revision:             1,
	}
	override := profile.Release{
		ProfileID:            profileAssistDraft,
		StableVersion:        "v1",
		CandidateVersion:     "v2",
		CandidateBasisPoints: 0,
	}
	resolver := newTestAtomicProfileResolver(t, []profile.Release{override})
	manager, err := NewProfileCatalogManager(repo, resolver, []profile.Release{override})
	if err != nil {
		t.Fatalf("NewProfileCatalogManager() error = %v", err)
	}
	if err := manager.Reload(ctx); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	selected, err := resolver.Resolve(ctx, profileAssistDraft, profile.SelectionSubject{UserID: 7})
	if err != nil || selected.Version != "v1" {
		t.Fatalf("Resolve() = %+v, %v", selected, err)
	}
}

func TestProfileCatalogManagerEnvironmentOverrideCannotHideInvalidPersistentRelease(t *testing.T) {
	ctx := context.Background()
	repo := newFakeProfileCatalogRepository()
	repo.releases[profileAssistDraft] = &repository.ProfileReleaseRecord{
		ProfileID:            profileAssistDraft,
		StableVersion:        "v1",
		CandidateVersion:     "missing",
		CandidateBasisPoints: profile.MaxReleaseBasisPoints,
		Revision:             1,
	}
	override := profile.Release{
		ProfileID:            profileAssistDraft,
		StableVersion:        "v1",
		CandidateVersion:     "v2",
		CandidateBasisPoints: 0,
	}
	resolver := newTestAtomicProfileResolver(t, []profile.Release{override})
	manager, err := NewProfileCatalogManager(repo, resolver, []profile.Release{override})
	if err != nil {
		t.Fatalf("NewProfileCatalogManager() error = %v", err)
	}
	if err := manager.Reload(ctx); err == nil || !errors.Is(err, profile.ErrProfileVersionAbsent) {
		t.Fatalf("Reload() error = %v", err)
	}
	selected, err := resolver.Resolve(ctx, profileAssistDraft, profile.SelectionSubject{UserID: 7})
	if err != nil || selected.Version != "v1" {
		t.Fatalf("failed reload replaced previous catalog: %+v, %v", selected, err)
	}
}

func TestProfileSnapshotV1RoundTrip(t *testing.T) {
	candidate := testManagedProfile("custom.snapshot", "v1")
	candidate.Budget.MaxSteps = 9
	snapshot, err := encodeProfileSnapshotV1(candidate)
	if err != nil {
		t.Fatalf("encodeProfileSnapshotV1() error = %v", err)
	}
	record := &repository.ProfileVersionRecord{
		ProfileID:      candidate.ID,
		Version:        candidate.Version,
		Status:         repository.ProfileVersionStatusPublished,
		SnapshotSchema: repository.ProfileSnapshotSchemaV1,
		SnapshotJSON:   snapshot,
		SnapshotHash:   repository.ProfileSnapshotHash(snapshot),
	}
	decoded, err := decodeProfileVersion(record)
	if err != nil {
		t.Fatalf("decodeProfileVersion() error = %v", err)
	}
	if decoded.ID != candidate.ID || decoded.Version != candidate.Version ||
		decoded.Prompt != candidate.Prompt || decoded.Budget.MaxSteps != 9 ||
		len(decoded.AllowedTools) != len(candidate.AllowedTools) {
		t.Fatalf("decoded profile = %+v", decoded)
	}
}

func TestProfileCatalogManagerPublishApprovalRequiresDifferentActor(t *testing.T) {
	ctx := context.Background()
	repo := newFakeProfileCatalogRepository()
	resolver := newTestAtomicProfileResolver(t, nil)
	manager, err := NewProfileCatalogManager(repo, resolver, nil)
	if err != nil {
		t.Fatalf("NewProfileCatalogManager() error = %v", err)
	}

	candidate := testManagedProfile("custom.approved", "v1")
	draft, err := manager.CreateDraft(ctx, candidate, 42)
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	approval, err := manager.RequestPublishApproval(ctx, candidate.ID, candidate.Version, draft.Revision, 42)
	if err != nil {
		t.Fatalf("RequestPublishApproval() error = %v", err)
	}
	if approval.Status != repository.ProfilePublishApprovalStatusPending || approval.SnapshotHash != draft.SnapshotHash {
		t.Fatalf("approval = %+v", approval)
	}

	_, err = manager.DecidePublishApproval(ctx, approval.ID.Hex(), approval.Revision, 42, repository.ProfilePublishDecisionApproved, "self")
	if !errors.Is(err, repository.ErrProfilePublishSelfApproval) {
		t.Fatalf("self DecidePublishApproval() error = %v", err)
	}

	applied, err := manager.DecidePublishApproval(ctx, approval.ID.Hex(), approval.Revision, 43, repository.ProfilePublishDecisionApproved, "reviewed")
	if err != nil {
		t.Fatalf("DecidePublishApproval() error = %v", err)
	}
	if applied.Status != repository.ProfilePublishApprovalStatusApplied || applied.DecidedBy != 43 {
		t.Fatalf("applied approval = %+v", applied)
	}
	selected, err := resolver.Resolve(ctx, candidate.ID, profile.SelectionSubject{UserID: 7})
	if err != nil || selected.Version != candidate.Version {
		t.Fatalf("Resolve() = %+v, %v", selected, err)
	}
}

func TestProfileCatalogManagerPublishApprovalRecoversPropagationFailure(t *testing.T) {
	ctx := context.Background()
	repo := newFakeProfileCatalogRepository()
	publisher := &fakeProfileChangePublisher{err: errors.New("redis unavailable")}
	manager, err := NewProfileCatalogManager(
		repo,
		newTestAtomicProfileResolver(t, nil),
		nil,
		WithProfileCatalogChangePublisher(publisher),
	)
	if err != nil {
		t.Fatalf("NewProfileCatalogManager() error = %v", err)
	}
	candidate := testManagedProfile("custom.recovery", "v1")
	draft, err := manager.CreateDraft(ctx, candidate, 42)
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	approval, err := manager.RequestPublishApproval(ctx, candidate.ID, candidate.Version, draft.Revision, 42)
	if err != nil {
		t.Fatalf("RequestPublishApproval() error = %v", err)
	}
	failed, err := manager.DecidePublishApproval(ctx, approval.ID.Hex(), approval.Revision, 43, repository.ProfilePublishDecisionApproved, "reviewed")
	if err == nil || failed == nil || failed.Status != repository.ProfilePublishApprovalStatusApplyFailed {
		t.Fatalf("DecidePublishApproval() = %+v, %v", failed, err)
	}

	publisher.err = nil
	applied, err := manager.RetryPublishApproval(ctx, failed.ID.Hex(), failed.Revision, 43)
	if err != nil {
		t.Fatalf("RetryPublishApproval() error = %v", err)
	}
	if applied.Status != repository.ProfilePublishApprovalStatusApplied || len(publisher.events) != 1 {
		t.Fatalf("recovered approval = %+v, changes = %+v", applied, publisher.events)
	}
}

func TestProfileCatalogManagerPublishApprovalRequiresAndRevalidatesQualityEvidence(t *testing.T) {
	ctx := context.Background()
	repo := newFakeProfileCatalogRepository()
	verifier := &fakeProfileQualityEvidenceVerifier{}
	manager, err := NewProfileCatalogManager(
		repo,
		newTestAtomicProfileResolver(t, nil),
		nil,
		WithProfileQualityEvidenceVerifier(verifier, true),
	)
	if err != nil {
		t.Fatalf("NewProfileCatalogManager() error = %v", err)
	}
	candidate := testManagedProfile("custom.evidence", "v2")
	draft, err := manager.CreateDraft(ctx, candidate, 42)
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if _, err := manager.RequestPublishApproval(ctx, candidate.ID, candidate.Version, draft.Revision, 42); !errors.Is(err, profile.ErrQualityEvidenceRequired) {
		t.Fatalf("RequestPublishApproval() error = %v, want ErrQualityEvidenceRequired", err)
	}

	reference := testProfileQualityEvidenceReference()
	approval, err := manager.RequestPublishApprovalWithEvidence(ctx, candidate.ID, candidate.Version, draft.Revision, 42, &reference)
	if err != nil {
		t.Fatalf("RequestPublishApprovalWithEvidence() error = %v", err)
	}
	if approval.QualityEvidence == nil || verifier.calls != 1 {
		t.Fatalf("approval evidence = %+v, verifier calls = %d", approval.QualityEvidence, verifier.calls)
	}
	if _, err := manager.DecidePublishApproval(ctx, approval.ID.Hex(), approval.Revision, 43, repository.ProfilePublishDecisionApproved, "reviewed"); err != nil {
		t.Fatalf("DecidePublishApproval() error = %v", err)
	}
	if verifier.calls != 2 {
		t.Fatalf("verifier calls = %d, want request and approve verification", verifier.calls)
	}
}

type fakeProfileCatalogRepository struct {
	mu               sync.Mutex
	versions         map[string]*repository.ProfileVersionRecord
	releases         map[string]*repository.ProfileReleaseRecord
	approvals        map[string]*repository.ProfilePublishApprovalRecord
	audits           []*repository.ProfileAuditEvent
	failAuditOutcome map[string]error
}

func newFakeProfileCatalogRepository() *fakeProfileCatalogRepository {
	return &fakeProfileCatalogRepository{
		versions:         make(map[string]*repository.ProfileVersionRecord),
		releases:         make(map[string]*repository.ProfileReleaseRecord),
		approvals:        make(map[string]*repository.ProfilePublishApprovalRecord),
		failAuditOutcome: make(map[string]error),
	}
}

func (r *fakeProfileCatalogRepository) CreateProfileVersion(_ context.Context, version *repository.ProfileVersionRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := profileVersionKey(version.ProfileID, version.Version)
	if _, exists := r.versions[key]; exists {
		return repository.ErrProfileVersionConflict
	}
	copy := *version
	copy.ID = primitive.NewObjectID()
	copy.Status = repository.ProfileVersionStatusDraft
	copy.SnapshotSchema = repository.ProfileSnapshotSchemaV1
	copy.SnapshotHash = repository.ProfileSnapshotHash(copy.SnapshotJSON)
	copy.Revision = 1
	r.versions[key] = &copy
	*version = copy
	return nil
}

func (r *fakeProfileCatalogRepository) PublishProfileVersion(
	_ context.Context,
	profileID, version string,
	expectedRevision int64,
	publishedBy uint64,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, exists := r.versions[profileVersionKey(profileID, version)]
	if !exists {
		return repository.ErrProfileVersionNotFound
	}
	if record.Status != repository.ProfileVersionStatusDraft || record.Revision != expectedRevision {
		return repository.ErrProfileVersionConflict
	}
	record.Status = repository.ProfileVersionStatusPublished
	record.Revision++
	record.PublishedBy = publishedBy
	return nil
}

func (r *fakeProfileCatalogRepository) GetProfileVersion(
	_ context.Context,
	profileID, version string,
) (*repository.ProfileVersionRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, exists := r.versions[profileVersionKey(profileID, version)]
	if !exists {
		return nil, repository.ErrProfileVersionNotFound
	}
	copy := *record
	return &copy, nil
}

func (r *fakeProfileCatalogRepository) ListProfileVersions(
	context.Context,
	string,
	int,
	int,
) ([]*repository.ProfileVersionRecord, int64, error) {
	return nil, 0, nil
}

func (r *fakeProfileCatalogRepository) UpsertProfileRelease(
	_ context.Context,
	release *repository.ProfileReleaseRecord,
	expectedRevision int64,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, exists := r.releases[release.ProfileID]
	if !exists && expectedRevision != 0 || exists && current.Revision != expectedRevision {
		return repository.ErrProfileReleaseConflict
	}
	copy := *release
	copy.Revision = expectedRevision + 1
	r.releases[release.ProfileID] = &copy
	*release = copy
	return nil
}

func (r *fakeProfileCatalogRepository) GetProfileRelease(
	_ context.Context,
	profileID string,
) (*repository.ProfileReleaseRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, exists := r.releases[strings.TrimSpace(profileID)]
	if !exists {
		return nil, repository.ErrProfileReleaseNotFound
	}
	copy := *record
	return &copy, nil
}

func (r *fakeProfileCatalogRepository) AppendProfileAuditEvent(
	_ context.Context,
	event *repository.ProfileAuditEvent,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.failAuditOutcome[event.Outcome]; err != nil {
		return err
	}
	copy := *event
	copy.ID = primitive.NewObjectID()
	copy.CreatedAt = time.Now()
	r.audits = append(r.audits, &copy)
	return nil
}

func (r *fakeProfileCatalogRepository) ListProfileAuditEvents(
	_ context.Context,
	profileID string,
	page, pageSize int,
) ([]*repository.ProfileAuditEvent, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	filtered := make([]*repository.ProfileAuditEvent, 0, len(r.audits))
	for _, event := range r.audits {
		if strings.TrimSpace(profileID) != "" && event.ProfileID != strings.TrimSpace(profileID) {
			continue
		}
		copy := *event
		filtered = append(filtered, &copy)
	}
	return filtered, int64(len(filtered)), nil
}

func (r *fakeProfileCatalogRepository) CreateProfilePublishApproval(
	_ context.Context,
	approval *repository.ProfilePublishApprovalRecord,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, current := range r.approvals {
		if current.ProfileID == approval.ProfileID && current.Version == approval.Version && current.SnapshotHash == approval.SnapshotHash {
			return repository.ErrProfilePublishApprovalConflict
		}
	}
	copy := *approval
	copy.ID = primitive.NewObjectID()
	copy.Status = repository.ProfilePublishApprovalStatusPending
	copy.Revision = 1
	copy.RequestedAt = time.Now()
	copy.UpdatedAt = copy.RequestedAt
	r.approvals[copy.ID.Hex()] = &copy
	*approval = copy
	return nil
}

func (r *fakeProfileCatalogRepository) GetProfilePublishApproval(
	_ context.Context,
	approvalID string,
) (*repository.ProfilePublishApprovalRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, exists := r.approvals[approvalID]
	if !exists {
		return nil, repository.ErrProfilePublishApprovalNotFound
	}
	copy := *record
	return &copy, nil
}

func (r *fakeProfileCatalogRepository) ListProfilePublishApprovals(
	_ context.Context,
	profileID, status string,
	_, _ int,
) ([]*repository.ProfilePublishApprovalRecord, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]*repository.ProfilePublishApprovalRecord, 0, len(r.approvals))
	for _, record := range r.approvals {
		if profileID != "" && record.ProfileID != profileID || status != "" && record.Status != status {
			continue
		}
		copy := *record
		items = append(items, &copy)
	}
	return items, int64(len(items)), nil
}

func (r *fakeProfileCatalogRepository) DecideProfilePublishApproval(
	_ context.Context,
	approvalID string,
	expectedRevision int64,
	actorUserID uint64,
	decision, reason string,
	lease time.Duration,
) (*repository.ProfilePublishApprovalRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, exists := r.approvals[approvalID]
	if !exists {
		return nil, repository.ErrProfilePublishApprovalNotFound
	}
	if record.RequestedBy == actorUserID {
		return nil, repository.ErrProfilePublishSelfApproval
	}
	if record.Status != repository.ProfilePublishApprovalStatusPending || record.Revision != expectedRevision {
		return nil, repository.ErrProfilePublishApprovalConflict
	}
	record.Decision = decision
	record.Reason = reason
	record.DecidedBy = actorUserID
	record.DecidedAt = time.Now()
	record.UpdatedAt = record.DecidedAt
	record.Revision++
	if decision == repository.ProfilePublishDecisionRejected {
		record.Status = repository.ProfilePublishApprovalStatusRejected
	} else {
		record.Status = repository.ProfilePublishApprovalStatusApplying
		record.ApplyingBy = actorUserID
		record.ApplyLeaseUntil = time.Now().Add(lease)
	}
	copy := *record
	return &copy, nil
}

func (r *fakeProfileCatalogRepository) ClaimProfilePublishApprovalRetry(
	_ context.Context,
	approvalID string,
	expectedRevision int64,
	actorUserID uint64,
	lease time.Duration,
) (*repository.ProfilePublishApprovalRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, exists := r.approvals[approvalID]
	if !exists {
		return nil, repository.ErrProfilePublishApprovalNotFound
	}
	if record.RequestedBy == actorUserID {
		return nil, repository.ErrProfilePublishSelfApproval
	}
	if record.Revision != expectedRevision || record.Status != repository.ProfilePublishApprovalStatusApplyFailed && !(record.Status == repository.ProfilePublishApprovalStatusApplying && !record.ApplyLeaseUntil.After(time.Now())) {
		return nil, repository.ErrProfilePublishApprovalConflict
	}
	record.Status = repository.ProfilePublishApprovalStatusApplying
	record.ApplyingBy = actorUserID
	record.ApplyLeaseUntil = time.Now().Add(lease)
	record.ErrorCode = ""
	record.Revision++
	copy := *record
	return &copy, nil
}

func (r *fakeProfileCatalogRepository) CompleteProfilePublishApproval(
	_ context.Context,
	approvalID string,
	expectedRevision int64,
	applied bool,
	errorCode string,
) (*repository.ProfilePublishApprovalRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, exists := r.approvals[approvalID]
	if !exists {
		return nil, repository.ErrProfilePublishApprovalNotFound
	}
	if record.Revision != expectedRevision || record.Status != repository.ProfilePublishApprovalStatusApplying {
		return nil, repository.ErrProfilePublishApprovalConflict
	}
	record.Revision++
	record.ApplyLeaseUntil = time.Time{}
	record.UpdatedAt = time.Now()
	if applied {
		record.Status = repository.ProfilePublishApprovalStatusApplied
		record.AppliedAt = record.UpdatedAt
		record.ErrorCode = ""
	} else {
		record.Status = repository.ProfilePublishApprovalStatusApplyFailed
		record.ErrorCode = errorCode
	}
	copy := *record
	return &copy, nil
}

func (r *fakeProfileCatalogRepository) LoadPublishedProfileCatalog(
	context.Context,
) (*repository.ProfileCatalogSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot := &repository.ProfileCatalogSnapshot{}
	for _, record := range r.versions {
		if record.Status == repository.ProfileVersionStatusPublished {
			copy := *record
			snapshot.Versions = append(snapshot.Versions, &copy)
		}
	}
	for _, record := range r.releases {
		copy := *record
		snapshot.Releases = append(snapshot.Releases, &copy)
	}
	return snapshot, nil
}

func newTestAtomicProfileResolver(t *testing.T, overrides []profile.Release) *profile.AtomicResolver {
	t.Helper()
	catalog, err := NewBuiltInProfileCatalog(nil, nil, overrides)
	if err != nil {
		t.Fatalf("NewBuiltInProfileCatalog() error = %v", err)
	}
	resolver, err := profile.NewAtomicResolver(catalog)
	if err != nil {
		t.Fatalf("NewAtomicResolver() error = %v", err)
	}
	return resolver
}

func testManagedProfile(id, version string) profile.AgentProfile {
	candidate := assistDraftAgentProfile(0)
	candidate.ID = id
	candidate.Version = version
	candidate.Prompt.ID = id + ".system"
	candidate.Prompt.Version = version
	return candidate
}

func fakePublishedProfileRecord(t *testing.T, candidate profile.AgentProfile) *repository.ProfileVersionRecord {
	t.Helper()
	snapshot, err := encodeProfileSnapshotV1(candidate)
	if err != nil {
		t.Fatalf("encodeProfileSnapshotV1() error = %v", err)
	}
	return &repository.ProfileVersionRecord{
		ProfileID:      candidate.ID,
		Version:        candidate.Version,
		Status:         repository.ProfileVersionStatusPublished,
		SnapshotSchema: repository.ProfileSnapshotSchemaV1,
		SnapshotJSON:   snapshot,
		SnapshotHash:   repository.ProfileSnapshotHash(snapshot),
		Revision:       2,
	}
}

func profileVersionKey(profileID, version string) string {
	return strings.TrimSpace(profileID) + "@" + strings.TrimSpace(version)
}

func TestProfileCatalogManagerAuditIsFailClosedBeforeMutation(t *testing.T) {
	repo := newFakeProfileCatalogRepository()
	repo.failAuditOutcome[repository.ProfileAuditOutcomeRequested] = errors.New("audit unavailable")
	manager, err := NewProfileCatalogManager(repo, newTestAtomicProfileResolver(t, nil), nil)
	if err != nil {
		t.Fatalf("NewProfileCatalogManager() error = %v", err)
	}
	candidate := testManagedProfile("custom.audit", "v1")
	if _, err := manager.CreateDraft(context.Background(), candidate, 42); err == nil || !strings.Contains(err.Error(), "before mutation") {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if len(repo.versions) != 0 {
		t.Fatalf("profile mutation ran without requested audit: %+v", repo.versions)
	}
}

func TestProfileCatalogManagerPublishesChangeAfterActivation(t *testing.T) {
	repo := newFakeProfileCatalogRepository()
	publisher := &fakeProfileChangePublisher{}
	manager, err := NewProfileCatalogManager(
		repo,
		newTestAtomicProfileResolver(t, nil),
		nil,
		WithProfileCatalogChangePublisher(publisher),
	)
	if err != nil {
		t.Fatalf("NewProfileCatalogManager() error = %v", err)
	}
	candidate := testManagedProfile("custom.change", "v1")
	draft, err := manager.CreateDraft(context.Background(), candidate, 42)
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if err := manager.PublishVersion(context.Background(), candidate.ID, candidate.Version, draft.Revision, 43); err != nil {
		t.Fatalf("PublishVersion() error = %v", err)
	}
	if len(publisher.events) != 1 || publisher.events[0].VersionRevision != 2 {
		t.Fatalf("published changes = %+v", publisher.events)
	}
	if got := repo.audits[len(repo.audits)-1].Outcome; got != repository.ProfileAuditOutcomeSucceeded {
		t.Fatalf("final audit outcome = %q", got)
	}
}

type fakeProfileChangePublisher struct {
	events []profile.CatalogChangeEvent
	err    error
}

type fakeProfileQualityEvidenceVerifier struct {
	calls int
	err   error
}

func (v *fakeProfileQualityEvidenceVerifier) Verify(
	_ context.Context,
	reference profile.QualityEvidenceReference,
	profileID, version string,
) (profile.QualityEvidence, error) {
	v.calls++
	if v.err != nil {
		return profile.QualityEvidence{}, v.err
	}
	now := time.Date(2026, 7, 22, 8, 0, 0, 0, time.UTC)
	reference = profile.NormalizeQualityEvidenceReference(reference)
	return profile.QualityEvidence{
		Reference: reference, ProfileID: profileID, ProfileVersion: version,
		GateStatus: profile.QualityEvidenceGatePassed, Cases: 50, Passed: 50,
		TaskCompletionRateBPS: 10000, ReadToolSelectionAccuracyBPS: 10000,
		SemanticPassRateBPS: 10000, ApprovalPassRateBPS: 10000,
		ReportSignedAt: now.Add(-time.Minute), VerifiedAt: now,
	}, nil
}

func testProfileQualityEvidenceReference() profile.QualityEvidenceReference {
	now := time.Date(2026, 7, 22, 8, 0, 0, 0, time.UTC)
	return profile.QualityEvidenceReference{
		Storage: profile.QualityEvidenceStorageMinIO, Bucket: "agent-eval", Key: "agent-task-eval/a/report.json",
		VersionID: "version-1", ReportSHA256: strings.Repeat("a", 64), Length: 1024,
		ContentType: profile.QualityEvidenceContentTypeJSON, RetentionMode: profile.QualityEvidenceRetentionCompliance,
		RetainUntil: now.Add(30 * 24 * time.Hour), ArchivedAt: now.Add(-30 * time.Second),
		DatasetVersion: "dataset-v1", DatasetSHA256: strings.Repeat("b", 64),
		ExecutionConfigHash: strings.Repeat("c", 64), IntegrityKeyID: "eval-key-v1",
	}
}

func (p *fakeProfileChangePublisher) PublishCatalogChange(_ context.Context, event profile.CatalogChangeEvent) error {
	if p.err != nil {
		return p.err
	}
	p.events = append(p.events, event)
	return nil
}
