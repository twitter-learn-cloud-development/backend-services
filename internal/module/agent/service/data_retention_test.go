package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"twitter-clone/internal/module/agent/repository"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"
)

type dataRetentionRepositoryFake struct {
	lease               bool
	candidates          []repository.DataRetentionCandidate
	claimed             bool
	claimCalls          int
	subjectState        repository.DataRetentionSubjectLeaseState
	subjectLeaseCalls   int
	subjectReleaseCalls int
	completed           []repository.DataRetentionCandidate
	released            int
	cutoffs             repository.DataRetentionCutoffs
}

func (f *dataRetentionRepositoryFake) AcquireDataRetentionLease(context.Context, string, string, time.Time, time.Time) (bool, error) {
	return f.lease, nil
}

func (f *dataRetentionRepositoryFake) AcquireDataRetentionSubjectLease(context.Context, uint64, string, time.Time, time.Time) (repository.DataRetentionSubjectLeaseState, error) {
	f.subjectLeaseCalls++
	if f.subjectState == "" {
		return repository.DataRetentionSubjectLeaseAcquired, nil
	}
	return f.subjectState, nil
}

func (f *dataRetentionRepositoryFake) ReleaseDataRetentionSubjectLease(context.Context, uint64, string) error {
	f.subjectReleaseCalls++
	return nil
}

func (f *dataRetentionRepositoryFake) ListDataRetentionCandidates(_ context.Context, cutoffs repository.DataRetentionCutoffs, _ int) ([]repository.DataRetentionCandidate, error) {
	f.cutoffs = cutoffs
	return f.candidates, nil
}

func (f *dataRetentionRepositoryFake) ClaimDataRetentionCandidate(context.Context, repository.DataRetentionCandidate, string, time.Time, time.Time) (bool, error) {
	f.claimCalls++
	return f.claimed, nil
}

func (f *dataRetentionRepositoryFake) CompleteDataRetentionCandidate(_ context.Context, candidate repository.DataRetentionCandidate, _ string, _ time.Time) (repository.DataRetentionPurgeResult, error) {
	f.completed = append(f.completed, candidate)
	return repository.DataRetentionPurgeResult{PrimaryDocuments: 1, RelatedDocuments: 2}, nil
}

func (f *dataRetentionRepositoryFake) ReleaseDataRetentionCandidate(context.Context, repository.DataRetentionCandidate, string) error {
	f.released++
	return nil
}

type dataRetentionObjectStoreFake struct {
	deleted []workflowTool.ResultReference
	err     error
}

func (f *dataRetentionObjectStoreFake) Put(context.Context, workflowTool.ResultArchiveRequest) (*workflowTool.ResultReference, error) {
	return nil, errors.New("unexpected put")
}

func (f *dataRetentionObjectStoreFake) Get(context.Context, workflowTool.ResultReference, int) ([]byte, error) {
	return nil, errors.New("unexpected get")
}

func (f *dataRetentionObjectStoreFake) Delete(_ context.Context, reference workflowTool.ResultReference) error {
	f.deleted = append(f.deleted, reference)
	return f.err
}

func TestDataRetentionSweepDryRunDoesNotClaimOrDelete(t *testing.T) {
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	repo := &dataRetentionRepositoryFake{
		lease: true,
		candidates: []repository.DataRetentionCandidate{{
			UserID: 42, Resource: repository.DataRetentionDialogue, ObjectID: primitive.NewObjectID(),
			VersionField: "updated_at", ObservedAt: now.Add(-200 * 24 * time.Hour),
		}},
	}
	config := DefaultDataRetentionConfig()
	config.Enabled = true
	worker, err := NewDataRetentionWorker(repo, nil, nil, config, "worker-1")
	if err != nil {
		t.Fatal(err)
	}
	report, err := worker.Sweep(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if report.Candidates != 1 || report.Skipped != 1 || report.Purged != 0 || repo.claimCalls != 0 || repo.subjectLeaseCalls != 0 {
		t.Fatalf("unexpected dry-run report: %+v claim_calls=%d", report, repo.claimCalls)
	}
	if !repo.cutoffs.Dialogue.Equal(now.Add(-DefaultDialogueRetention)) ||
		!repo.cutoffs.SuspendedCheckpoint.Equal(now.Add(-DefaultSuspendedCheckpointRetention)) {
		t.Fatalf("unexpected retention cutoffs: %+v", repo.cutoffs)
	}
}

func TestDataRetentionSweepDeletesObjectBeforeClearingReference(t *testing.T) {
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	reference := &repository.ToolResultReference{
		Storage: "minio", Bucket: "agent-tool-results", Key: "tool-results/tenant/result/digest.json",
		Digest: "digest", Length: 42, ContentType: "application/json",
	}
	repo := &dataRetentionRepositoryFake{
		lease: true, claimed: true,
		candidates: []repository.DataRetentionCandidate{{
			UserID: 42, Resource: repository.DataRetentionToolResult, ObjectID: primitive.NewObjectID(),
			VersionField: "updated_at", ObservedAt: now.Add(-31 * 24 * time.Hour), OutputReference: reference,
		}},
	}
	objects := &dataRetentionObjectStoreFake{}
	config := DefaultDataRetentionConfig()
	config.Enabled = true
	config.DryRun = false
	worker, err := NewDataRetentionWorker(repo, objects, nil, config, "worker-1")
	if err != nil {
		t.Fatal(err)
	}
	report, err := worker.Sweep(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if report.Purged != 1 || len(objects.deleted) != 1 || len(repo.completed) != 1 || repo.released != 0 || repo.subjectReleaseCalls != 1 {
		t.Fatalf("unexpected purge state: report=%+v deleted=%d completed=%d released=%d", report, len(objects.deleted), len(repo.completed), repo.released)
	}
}

func TestDataRetentionSweepPreservesReferenceWhenObjectDeleteFails(t *testing.T) {
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	repo := &dataRetentionRepositoryFake{
		lease: true, claimed: true,
		candidates: []repository.DataRetentionCandidate{{
			UserID: 42, Resource: repository.DataRetentionToolResult, ObjectID: primitive.NewObjectID(),
			VersionField: "updated_at", ObservedAt: now.Add(-31 * 24 * time.Hour),
			OutputReference: &repository.ToolResultReference{Storage: "minio"},
		}},
	}
	objects := &dataRetentionObjectStoreFake{err: errors.New("minio unavailable")}
	config := DefaultDataRetentionConfig()
	config.Enabled = true
	config.DryRun = false
	worker, err := NewDataRetentionWorker(repo, objects, nil, config, "worker-1")
	if err != nil {
		t.Fatal(err)
	}
	report, err := worker.Sweep(context.Background(), now)
	if err == nil {
		t.Fatal("expected object deletion failure")
	}
	if report.Failed != 1 || len(repo.completed) != 0 || repo.released != 1 || repo.subjectReleaseCalls != 1 {
		t.Fatalf("unexpected failed purge state: report=%+v completed=%d released=%d", report, len(repo.completed), repo.released)
	}
}

func TestDataRetentionSweepSkipsHeldSubject(t *testing.T) {
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	repo := &dataRetentionRepositoryFake{
		lease: true, claimed: true, subjectState: repository.DataRetentionSubjectHeld,
		candidates: []repository.DataRetentionCandidate{{
			UserID: 42, Resource: repository.DataRetentionOrphanMessage, ObjectID: primitive.NewObjectID(),
			VersionField: "created_at", ObservedAt: now.Add(-48 * time.Hour),
		}},
	}
	config := DefaultDataRetentionConfig()
	config.Enabled = true
	config.DryRun = false
	worker, err := NewDataRetentionWorker(repo, nil, nil, config, "worker-1")
	if err != nil {
		t.Fatal(err)
	}
	report, err := worker.Sweep(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if report.Skipped != 1 || report.Purged != 0 || repo.claimCalls != 0 || repo.subjectReleaseCalls != 0 {
		t.Fatalf("unexpected held-subject state: report=%+v claim_calls=%d subject_releases=%d", report, repo.claimCalls, repo.subjectReleaseCalls)
	}
}

func TestDataRetentionConfigRejectsShortSuspendedWindow(t *testing.T) {
	config := DefaultDataRetentionConfig()
	config.SuspendedCheckpointRetention = config.TerminalCheckpointRetention - time.Hour
	if err := config.Validate(); err == nil {
		t.Fatal("expected invalid suspended checkpoint retention")
	}
}

func TestDataRetentionConfigRejectsMissingOrphanMessageGrace(t *testing.T) {
	config := DefaultDataRetentionConfig()
	config.OrphanMessageGracePeriod = 0
	if err := config.Validate(); err == nil {
		t.Fatal("expected invalid orphan message grace period")
	}
}
