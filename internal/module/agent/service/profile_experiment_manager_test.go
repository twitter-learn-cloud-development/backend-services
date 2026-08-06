package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	agentObservability "twitter-clone/internal/module/agent/observability"
	"twitter-clone/internal/module/agent/profile"
	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func TestProfileExperimentRecordsRunsAndAutomaticallyRollsBack(t *testing.T) {
	manager, experimentRepo, catalogRepo := newTestProfileExperimentManager(t, nil)
	ctx := context.Background()
	experiment, err := manager.Start(ctx, "custom.experiment", 1, profile.ExperimentPolicy{
		MinSamplesPerArm: 1, TargetSamplesPerArm: 2,
		MaxErrorRateIncreaseBasisPoints:   100,
		MaxP95LatencyIncreaseBasisPoints:  1_000,
		MaxAverageCostIncreaseBasisPoints: 1_000,
	}, 7)
	require.NoError(t, err)

	recorder := NewProfileExperimentRunRecorder(experimentRepo)
	require.NoError(t, recorder.RecordRun(ctx, agentObservability.RunRecord{
		RunID: "stable-run", AgentProfileID: experiment.ProfileID, AgentProfileVersion: experiment.StableVersion,
		Status: string(agentRuntime.RunStatusCompleted), DurationMS: 100,
		Usage: agentObservability.TokenUsage{EstimatedCostMicros: 100}, FinishedAt: time.Now(),
	}))
	require.NoError(t, recorder.RecordRun(ctx, agentObservability.RunRecord{
		RunID: "candidate-run", AgentProfileID: experiment.ProfileID, AgentProfileVersion: experiment.CandidateVersion,
		Status: string(agentRuntime.RunStatusFailed), DurationMS: 100,
		Usage: agentObservability.TokenUsage{EstimatedCostMicros: 100}, FinishedAt: time.Now(),
	}))
	// The same final Run trace may be replayed by the fanout; observation writes are idempotent.
	require.NoError(t, recorder.RecordRun(ctx, agentObservability.RunRecord{
		RunID: "candidate-run", AgentProfileID: experiment.ProfileID, AgentProfileVersion: experiment.CandidateVersion,
		Status: string(agentRuntime.RunStatusFailed), DurationMS: 100,
	}))

	result, err := manager.Evaluate(ctx, experiment.ID, experiment.Revision, 7)
	require.NoError(t, err)
	require.Equal(t, profile.ExperimentStatusRolledBack, result.Status)
	require.Equal(t, profile.ExperimentDecisionRollback, result.Decision)
	require.Len(t, experimentRepo.observations, 2)
	release, err := catalogRepo.GetProfileRelease(ctx, experiment.ProfileID)
	require.NoError(t, err)
	require.Equal(t, 0, release.CandidateBasisPoints)
	require.Equal(t, int64(2), release.Revision)
}

func TestProfileExperimentPassDoesNotPromoteCandidate(t *testing.T) {
	manager, experimentRepo, catalogRepo := newTestProfileExperimentManager(t, nil)
	ctx := context.Background()
	experiment, err := manager.Start(ctx, "custom.experiment", 1, profile.ExperimentPolicy{
		MinSamplesPerArm: 1, TargetSamplesPerArm: 1,
		MaxErrorRateIncreaseBasisPoints:   100,
		MaxP95LatencyIncreaseBasisPoints:  1_000,
		MaxAverageCostIncreaseBasisPoints: 1_000,
	}, 7)
	require.NoError(t, err)
	experimentRepo.observations = append(experimentRepo.observations,
		&repository.ProfileExperimentObservationRecord{ExperimentID: experiment.ID, EventID: "stable", Arm: profile.ExperimentArmStable, ProfileVersion: experiment.StableVersion, Success: true, DurationMS: 100, CostMicros: 100},
		&repository.ProfileExperimentObservationRecord{ExperimentID: experiment.ID, EventID: "candidate", Arm: profile.ExperimentArmCandidate, ProfileVersion: experiment.CandidateVersion, Success: true, DurationMS: 100, CostMicros: 100},
	)
	result, err := manager.Evaluate(ctx, experiment.ID, experiment.Revision, 7)
	require.NoError(t, err)
	require.Equal(t, profile.ExperimentStatusPassed, result.Status)
	release, err := catalogRepo.GetProfileRelease(ctx, experiment.ProfileID)
	require.NoError(t, err)
	require.Equal(t, 1_000, release.CandidateBasisPoints)
	require.Equal(t, int64(1), release.Revision)
}

func TestProfileExperimentOutcomeIsIdempotentAndGatesDecision(t *testing.T) {
	manager, experimentRepo, _ := newTestProfileExperimentManager(t, nil)
	ctx := context.Background()
	experiment, err := manager.Start(ctx, "custom.experiment", 1, profile.ExperimentPolicy{
		MinSamplesPerArm: 1, TargetSamplesPerArm: 1,
		OutcomeSignal:           profile.ExperimentOutcomeSignalDraftPublished,
		MinOutcomeSamplesPerArm: 1, MaxOutcomeRateDecreaseBasisPoints: 500,
	}, 7)
	require.NoError(t, err)
	experimentRepo.observations = append(experimentRepo.observations,
		&repository.ProfileExperimentObservationRecord{ExperimentID: experiment.ID, EventID: "stable", Arm: profile.ExperimentArmStable, ProfileVersion: experiment.StableVersion, Success: true},
		&repository.ProfileExperimentObservationRecord{ExperimentID: experiment.ID, EventID: "candidate", Arm: profile.ExperimentArmCandidate, ProfileVersion: experiment.CandidateVersion, Success: true},
	)
	replay, err := manager.RecordOutcome(ctx, experiment.ID, "stable", profile.ExperimentOutcomeSignalDraftPublished, true, 7)
	require.NoError(t, err)
	require.False(t, replay)
	replay, err = manager.RecordOutcome(ctx, experiment.ID, "stable", profile.ExperimentOutcomeSignalDraftPublished, true, 7)
	require.NoError(t, err)
	require.True(t, replay)
	_, err = manager.RecordOutcome(ctx, experiment.ID, "stable", profile.ExperimentOutcomeSignalDraftPublished, false, 7)
	require.ErrorIs(t, err, repository.ErrProfileExperimentOutcomeConflict)
	_, err = manager.RecordOutcome(ctx, experiment.ID, "candidate", profile.ExperimentOutcomeSignalDraftPublished, false, 7)
	require.NoError(t, err)

	result, err := manager.Evaluate(ctx, experiment.ID, experiment.Revision, 7)
	require.NoError(t, err)
	require.Equal(t, profile.ExperimentStatusRolledBack, result.Status)
	require.Equal(t, "candidate_outcome_rate_regressed", result.DecisionReason)
}

func TestProfileExperimentProductOutcomeCreatesRunObservationAndReplays(t *testing.T) {
	manager, experimentRepo, _ := newTestProfileExperimentManager(t, nil)
	ctx := context.Background()
	experiment, err := manager.Start(ctx, "custom.experiment", 1, profile.ExperimentPolicy{
		MinSamplesPerArm: 1, TargetSamplesPerArm: 1,
		OutcomeSignal:           profile.ExperimentOutcomeSignalDraftPublished,
		MinOutcomeSamplesPerArm: 1,
	}, 7)
	require.NoError(t, err)
	run := agentObservability.RunRecord{
		RunID: "assist-run", UserID: 42, AgentProfileID: experiment.ProfileID,
		AgentProfileVersion: experiment.CandidateVersion, Mode: string(agentRuntime.ModeAssist),
		Status: string(agentRuntime.RunStatusCompleted), DurationMS: 125,
		Usage: agentObservability.TokenUsage{EstimatedCostMicros: 300}, FinishedAt: time.Now(),
	}

	applicable, replay, err := manager.RecordProductOutcome(
		ctx, run, profile.ExperimentOutcomeSignalDraftPublished, true, 7,
	)
	require.NoError(t, err)
	require.True(t, applicable)
	require.False(t, replay)
	require.Len(t, experimentRepo.observations, 1)
	require.True(t, experimentRepo.observations[0].OutcomeObserved)
	require.True(t, experimentRepo.observations[0].OutcomePositive)

	applicable, replay, err = manager.RecordProductOutcome(
		ctx, run, profile.ExperimentOutcomeSignalDraftPublished, true, 7,
	)
	require.NoError(t, err)
	require.True(t, applicable)
	require.True(t, replay)
	require.Len(t, experimentRepo.observations, 1)
}

func TestProfileExperimentProductOutcomeIgnoresMismatchedPolicy(t *testing.T) {
	manager, experimentRepo, _ := newTestProfileExperimentManager(t, nil)
	experiment, err := manager.Start(context.Background(), "custom.experiment", 1, profile.ExperimentPolicy{
		MinSamplesPerArm: 1, TargetSamplesPerArm: 1,
		OutcomeSignal: profile.ExperimentOutcomeSignalResponseAccepted,
	}, 7)
	require.NoError(t, err)

	applicable, replay, err := manager.RecordProductOutcome(context.Background(), agentObservability.RunRecord{
		RunID: "assist-run", UserID: 42, AgentProfileID: experiment.ProfileID,
		AgentProfileVersion: experiment.StableVersion, Status: string(agentRuntime.RunStatusCompleted),
		FinishedAt: time.Now(),
	}, profile.ExperimentOutcomeSignalDraftPublished, true, 7)
	require.NoError(t, err)
	require.False(t, applicable)
	require.False(t, replay)
	require.Empty(t, experimentRepo.observations)
}

func TestProfileExperimentBecomesSupersededWhenReleaseChanges(t *testing.T) {
	manager, _, catalogRepo := newTestProfileExperimentManager(t, nil)
	ctx := context.Background()
	experiment, err := manager.Start(ctx, "custom.experiment", 1, profile.ExperimentPolicy{MinSamplesPerArm: 1, TargetSamplesPerArm: 1}, 7)
	require.NoError(t, err)
	require.NoError(t, catalogRepo.UpsertProfileRelease(ctx, &repository.ProfileReleaseRecord{
		ProfileID: experiment.ProfileID, StableVersion: experiment.StableVersion, CandidateVersion: experiment.CandidateVersion,
		CandidateBasisPoints: 2_000, Salt: experiment.ReleaseSalt, UpdatedBy: 9,
	}, 1))
	result, err := manager.Evaluate(ctx, experiment.ID, experiment.Revision, 7)
	require.NoError(t, err)
	require.Equal(t, profile.ExperimentStatusSuperseded, result.Status)
}

func TestProfileExperimentRejectsEnvironmentOverride(t *testing.T) {
	override := profile.Release{ProfileID: "custom.experiment", StableVersion: "v1", CandidateVersion: "v2", CandidateBasisPoints: 1_000, Salt: "override"}
	manager, _, _ := newTestProfileExperimentManager(t, []profile.Release{override})
	_, err := manager.Start(context.Background(), "custom.experiment", 1, profile.ExperimentPolicy{}, 7)
	require.ErrorIs(t, err, ErrProfileExperimentReleaseOverridden)
}

func TestAsyncProfileExperimentRunRecorderUsesBoundedNonBlockingQueue(t *testing.T) {
	asyncRecorder, err := NewAsyncProfileExperimentRunRecorder(NewProfileExperimentRunRecorder(newFakeProfileExperimentRepository()), 1)
	require.NoError(t, err)
	record := agentObservability.RunRecord{
		RunID: "run-1", AgentProfileID: "custom.experiment", AgentProfileVersion: "v1",
		Status: string(agentRuntime.RunStatusCompleted),
	}
	require.NoError(t, asyncRecorder.RecordRun(context.Background(), record))
	record.RunID = "run-2"
	require.ErrorIs(t, asyncRecorder.RecordRun(context.Background(), record), ErrProfileExperimentObservationQueueFull)
	require.NoError(t, asyncRecorder.RecordRun(context.Background(), agentObservability.RunRecord{Status: string(agentRuntime.RunStatusRunning)}))
}

func TestAsyncProfileExperimentRunRecorderPersistsInBackground(t *testing.T) {
	manager, experimentRepo, _ := newTestProfileExperimentManager(t, nil)
	experiment, err := manager.Start(context.Background(), "custom.experiment", 1, profile.ExperimentPolicy{MinSamplesPerArm: 1, TargetSamplesPerArm: 2}, 7)
	require.NoError(t, err)
	asyncRecorder, err := NewAsyncProfileExperimentRunRecorder(NewProfileExperimentRunRecorder(experimentRepo), 4)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go asyncRecorder.Run(ctx)

	require.NoError(t, asyncRecorder.RecordRun(context.Background(), agentObservability.RunRecord{
		RunID: "async-run", AgentProfileID: experiment.ProfileID, AgentProfileVersion: experiment.StableVersion,
		Status: string(agentRuntime.RunStatusCompleted), FinishedAt: time.Now(),
	}))
	require.Eventually(t, func() bool {
		experimentRepo.mu.Lock()
		defer experimentRepo.mu.Unlock()
		return len(experimentRepo.observations) == 1
	}, time.Second, 10*time.Millisecond)
}

func newTestProfileExperimentManager(t *testing.T, overrides []profile.Release) (*ProfileExperimentManager, *fakeProfileExperimentRepository, *fakeProfileCatalogRepository) {
	t.Helper()
	catalogRepo := newFakeProfileCatalogRepository()
	for _, version := range []string{"v1", "v2"} {
		record := fakePublishedProfileRecord(t, testManagedProfile("custom.experiment", version))
		catalogRepo.versions[profileVersionKey(record.ProfileID, record.Version)] = record
	}
	catalogRepo.releases["custom.experiment"] = &repository.ProfileReleaseRecord{
		ID: primitive.NewObjectID(), ProfileID: "custom.experiment", StableVersion: "v1", CandidateVersion: "v2",
		CandidateBasisPoints: 1_000, Salt: "experiment-a", Revision: 1, CreatedBy: 7, UpdatedBy: 7,
	}
	catalogManager, err := NewProfileCatalogManager(catalogRepo, newTestAtomicProfileResolver(t, nil), overrides)
	require.NoError(t, err)
	require.NoError(t, catalogManager.Reload(context.Background()))
	experimentRepo := newFakeProfileExperimentRepository()
	manager, err := NewProfileExperimentManager(experimentRepo, catalogRepo, catalogManager)
	require.NoError(t, err)
	return manager, experimentRepo, catalogRepo
}

type fakeProfileExperimentRepository struct {
	mu           sync.Mutex
	experiments  map[primitive.ObjectID]*repository.ProfileExperimentRecord
	observations []*repository.ProfileExperimentObservationRecord
}

func newFakeProfileExperimentRepository() *fakeProfileExperimentRepository {
	return &fakeProfileExperimentRepository{experiments: make(map[primitive.ObjectID]*repository.ProfileExperimentRecord)}
}

func (r *fakeProfileExperimentRepository) CreateProfileExperiment(_ context.Context, experiment *repository.ProfileExperimentRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, current := range r.experiments {
		if current.ProfileID == experiment.ProfileID && current.Status == profile.ExperimentStatusRunning {
			return repository.ErrProfileExperimentAlreadyRunning
		}
	}
	copy := *experiment
	copy.Status = profile.ExperimentStatusRunning
	copy.Decision = profile.ExperimentDecisionCollecting
	copy.DecisionReason = "minimum_samples_not_reached"
	copy.Revision = 1
	copy.StartedAt = time.Now()
	copy.UpdatedAt = copy.StartedAt
	r.experiments[copy.ID] = &copy
	*experiment = copy
	return nil
}

func (r *fakeProfileExperimentRepository) GetProfileExperiment(_ context.Context, experimentID primitive.ObjectID) (*repository.ProfileExperimentRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.experiments[experimentID]
	if !ok {
		return nil, repository.ErrProfileExperimentNotFound
	}
	copy := *record
	return &copy, nil
}

func (r *fakeProfileExperimentRepository) GetRunningProfileExperiment(_ context.Context, profileID string) (*repository.ProfileExperimentRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, record := range r.experiments {
		if record.ProfileID == profileID && record.Status == profile.ExperimentStatusRunning {
			copy := *record
			return &copy, nil
		}
	}
	return nil, repository.ErrProfileExperimentNotFound
}

func (r *fakeProfileExperimentRepository) ListProfileExperiments(_ context.Context, profileID, status string, _, _ int) ([]*repository.ProfileExperimentRecord, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]*repository.ProfileExperimentRecord, 0)
	for _, record := range r.experiments {
		if (profileID == "" || record.ProfileID == profileID) && (status == "" || record.Status == status) {
			copy := *record
			result = append(result, &copy)
		}
	}
	return result, int64(len(result)), nil
}

func (r *fakeProfileExperimentRepository) AppendProfileExperimentObservation(_ context.Context, observation *repository.ProfileExperimentObservationRecord) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, current := range r.observations {
		if current.ExperimentID == observation.ExperimentID && current.EventID == observation.EventID {
			return false, nil
		}
	}
	copy := *observation
	r.observations = append(r.observations, &copy)
	return true, nil
}

func (r *fakeProfileExperimentRepository) RecordProfileExperimentOutcome(
	_ context.Context,
	experimentID primitive.ObjectID,
	eventID, signal string,
	positive bool,
	recordedBy uint64,
) (*repository.ProfileExperimentObservationRecord, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, observation := range r.observations {
		if observation.ExperimentID != experimentID || observation.EventID != eventID {
			continue
		}
		if observation.OutcomeObserved {
			if observation.OutcomeSignal == signal && observation.OutcomePositive == positive {
				copy := *observation
				return &copy, true, nil
			}
			return nil, false, repository.ErrProfileExperimentOutcomeConflict
		}
		observation.OutcomeObserved = true
		observation.OutcomeSignal = signal
		observation.OutcomePositive = positive
		observation.OutcomeRecordedBy = recordedBy
		observation.OutcomeRecordedAt = time.Now()
		copy := *observation
		return &copy, false, nil
	}
	return nil, false, repository.ErrProfileExperimentObservationNotFound
}

func (r *fakeProfileExperimentRepository) ListProfileExperimentObservations(_ context.Context, experimentID primitive.ObjectID, limit int) ([]*repository.ProfileExperimentObservationRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]*repository.ProfileExperimentObservationRecord, 0)
	for _, record := range r.observations {
		if record.ExperimentID == experimentID {
			copy := *record
			result = append(result, &copy)
		}
	}
	if len(result) > limit {
		return nil, repository.ErrProfileExperimentObservationLimit
	}
	return result, nil
}

func (r *fakeProfileExperimentRepository) UpdateProfileExperimentDecision(_ context.Context, experimentID primitive.ObjectID, expectedRevision int64, status, decision, reason string, stats profile.ExperimentStats, updatedBy uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.experiments[experimentID]
	if !ok {
		return repository.ErrProfileExperimentNotFound
	}
	if record.Revision != expectedRevision || record.Status != profile.ExperimentStatusRunning {
		return repository.ErrProfileExperimentConflict
	}
	record.Status = status
	record.Decision = decision
	record.DecisionReason = reason
	record.Stats = stats
	record.UpdatedBy = updatedBy
	record.Revision++
	if status != profile.ExperimentStatusRunning {
		record.CompletedAt = time.Now()
	}
	return nil
}
