package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	agentObservability "twitter-clone/internal/module/agent/observability"
	"twitter-clone/internal/module/agent/profile"
	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

const DefaultProfileExperimentReconcileInterval = 30 * time.Second

var (
	ErrProfileExperimentDisabled              = errors.New("Agent Profile experiments are disabled")
	ErrProfileExperimentReleaseOverridden     = errors.New("Agent Profile release is controlled by an environment override")
	ErrProfileExperimentReleaseChanged        = errors.New("Agent Profile release changed after the experiment started")
	ErrProfileExperimentObservationQueueFull  = errors.New("Agent Profile experiment observation queue is full")
	ErrProfileExperimentOutcomeNotConfigured  = errors.New("Agent Profile experiment has no business outcome signal configured")
	ErrProfileExperimentOutcomeSignalMismatch = errors.New("Agent Profile experiment outcome signal does not match its policy")
)

type ProfileExperimentManager struct {
	repository      repository.ProfileExperimentRepository
	auditRepository repository.ProfileCatalogAuditRepository
	catalogManager  *ProfileCatalogManager
	observer        ProfileExperimentObserver
}

type ProfileExperimentProductOutcomeRecorder struct {
	manager     *ProfileExperimentManager
	actorUserID uint64
}

func NewProfileExperimentProductOutcomeRecorder(
	manager *ProfileExperimentManager,
	actorUserID uint64,
) (*ProfileExperimentProductOutcomeRecorder, error) {
	if manager == nil || actorUserID == 0 {
		return nil, errors.New("profile experiment manager and product outcome actor are required")
	}
	return &ProfileExperimentProductOutcomeRecorder{manager: manager, actorUserID: actorUserID}, nil
}

func (r *ProfileExperimentProductOutcomeRecorder) RecordProductOutcome(
	ctx context.Context,
	run agentObservability.RunRecord,
	signal string,
	positive bool,
) error {
	if r == nil || r.manager == nil || r.actorUserID == 0 {
		return ErrProfileExperimentDisabled
	}
	_, _, err := r.manager.RecordProductOutcome(ctx, run, signal, positive, r.actorUserID)
	return err
}

type ProfileExperimentManagerOption func(*ProfileExperimentManager)

func WithProfileExperimentObserver(observer ProfileExperimentObserver) ProfileExperimentManagerOption {
	return func(manager *ProfileExperimentManager) {
		if observer != nil {
			manager.observer = observer
		}
	}
}

func NewProfileExperimentManager(repo repository.ProfileExperimentRepository, auditRepo repository.ProfileCatalogAuditRepository, catalogManager *ProfileCatalogManager, options ...ProfileExperimentManagerOption) (*ProfileExperimentManager, error) {
	if repo == nil || auditRepo == nil || catalogManager == nil {
		return nil, errors.New("profile experiment repository, audit repository and catalog manager are required")
	}
	manager := &ProfileExperimentManager{repository: repo, auditRepository: auditRepo, catalogManager: catalogManager, observer: noopProfileExperimentObserver{}}
	for _, option := range options {
		if option != nil {
			option(manager)
		}
	}
	return manager, nil
}

func (m *ProfileExperimentManager) Start(ctx context.Context, profileID string, expectedReleaseRevision int64, policy profile.ExperimentPolicy, actorUserID uint64) (*repository.ProfileExperimentRecord, error) {
	if m == nil || m.repository == nil || m.catalogManager == nil {
		return nil, ErrProfileExperimentDisabled
	}
	profileID = strings.TrimSpace(profileID)
	if profileID == "" || expectedReleaseRevision < 1 || actorUserID == 0 {
		return nil, errors.New("profile id, expected release revision and actor are required")
	}
	if m.catalogManager.IsReleaseOverridden(profileID) {
		return nil, ErrProfileExperimentReleaseOverridden
	}
	normalizedPolicy, err := profile.NormalizeExperimentPolicy(policy)
	if err != nil {
		return nil, err
	}
	release, err := m.catalogManager.GetRelease(ctx, profileID)
	if err != nil {
		return nil, err
	}
	if release.Revision != expectedReleaseRevision {
		return nil, repository.ErrProfileReleaseConflict
	}
	if _, err := profile.ExperimentObservationLimit(normalizedPolicy, release.CandidateBasisPoints); err != nil {
		return nil, err
	}
	experiment := &repository.ProfileExperimentRecord{
		ID: primitive.NewObjectID(), ProfileID: release.ProfileID,
		StableVersion: release.StableVersion, CandidateVersion: release.CandidateVersion,
		CandidateBasisPoints: release.CandidateBasisPoints, ReleaseRevision: release.Revision,
		ReleaseSalt: release.Salt, Policy: normalizedPolicy, CreatedBy: actorUserID,
	}
	operationID, err := newProfileOperationID()
	if err != nil {
		return nil, err
	}
	audit := repository.ProfileAuditEvent{
		OperationID: operationID, Action: repository.ProfileAuditActionStartExperiment,
		ProfileID: profileID, ExperimentID: experiment.ID.Hex(), ActorUserID: actorUserID,
		ReleaseRevision: release.Revision,
	}
	if err := m.appendExperimentAudit(ctx, audit, repository.ProfileAuditOutcomeRequested, ""); err != nil {
		return nil, fmt.Errorf("profile experiment audit failed before mutation: %w", err)
	}
	if err := m.repository.CreateProfileExperiment(ctx, experiment); err != nil {
		return nil, m.finishFailedExperimentMutation(ctx, audit, err)
	}
	if err := m.appendExperimentAudit(ctx, audit, repository.ProfileAuditOutcomeSucceeded, ""); err != nil {
		return experiment, fmt.Errorf("profile experiment was stored but final audit failed: %w", err)
	}
	return experiment, nil
}

func (m *ProfileExperimentManager) Get(ctx context.Context, experimentID primitive.ObjectID) (*repository.ProfileExperimentRecord, error) {
	if m == nil || m.repository == nil {
		return nil, ErrProfileExperimentDisabled
	}
	return m.repository.GetProfileExperiment(ctx, experimentID)
}

func (m *ProfileExperimentManager) List(ctx context.Context, profileID, status string, page, pageSize int) ([]*repository.ProfileExperimentRecord, int64, error) {
	if m == nil || m.repository == nil {
		return nil, 0, ErrProfileExperimentDisabled
	}
	return m.repository.ListProfileExperiments(ctx, profileID, status, page, pageSize)
}

func (m *ProfileExperimentManager) RecordOutcome(
	ctx context.Context,
	experimentID primitive.ObjectID,
	eventID, signal string,
	positive bool,
	actorUserID uint64,
) (bool, error) {
	if m == nil || m.repository == nil {
		return false, ErrProfileExperimentDisabled
	}
	if experimentID.IsZero() || strings.TrimSpace(eventID) == "" || actorUserID == 0 {
		return false, errors.New("experiment id, event id and actor are required")
	}
	experiment, err := m.repository.GetProfileExperiment(ctx, experimentID)
	if err != nil {
		return false, err
	}
	if experiment.Status != profile.ExperimentStatusRunning {
		return false, repository.ErrProfileExperimentConflict
	}
	normalizedPolicy, err := profile.NormalizeExperimentPolicy(experiment.Policy)
	if err != nil {
		return false, err
	}
	if normalizedPolicy.OutcomeSignal == "" {
		return false, ErrProfileExperimentOutcomeNotConfigured
	}
	if strings.TrimSpace(signal) != normalizedPolicy.OutcomeSignal {
		return false, ErrProfileExperimentOutcomeSignalMismatch
	}
	observation, replay, err := m.repository.RecordProfileExperimentOutcome(
		ctx, experiment.ID, eventID, normalizedPolicy.OutcomeSignal, positive, actorUserID,
	)
	if err != nil {
		return false, err
	}
	if observation != nil {
		m.observer.ObserveOutcome(observation.Arm, positive, replay)
	}
	return replay, nil
}

// RecordProductOutcome attributes a trusted product action to a completed Run.
// Runs outside a matching active experiment are intentionally ignored.
func (m *ProfileExperimentManager) RecordProductOutcome(
	ctx context.Context,
	run agentObservability.RunRecord,
	signal string,
	positive bool,
	actorUserID uint64,
) (bool, bool, error) {
	if m == nil || m.repository == nil {
		return false, false, ErrProfileExperimentDisabled
	}
	signal = strings.TrimSpace(signal)
	if run.RunID == "" || run.UserID == 0 || actorUserID == 0 || !profile.ValidExperimentOutcomeSignal(signal) {
		m.observer.ObserveProductOutcome("failed")
		return false, false, errors.New("completed run, valid product signal and actor are required")
	}
	if run.Status != string(agentRuntime.RunStatusCompleted) {
		m.observer.ObserveProductOutcome("failed")
		return false, false, errors.New("product outcomes require a completed run")
	}

	experiment, err := m.repository.GetRunningProfileExperiment(ctx, run.AgentProfileID)
	if errors.Is(err, repository.ErrProfileExperimentNotFound) {
		m.observer.ObserveProductOutcome("not_applicable")
		return false, false, nil
	}
	if err != nil {
		m.observer.ObserveProductOutcome("failed")
		return false, false, err
	}
	policy, err := profile.NormalizeExperimentPolicy(experiment.Policy)
	if err != nil {
		m.observer.ObserveProductOutcome("failed")
		return false, false, err
	}
	runOccurredAt := run.StartedAt
	if runOccurredAt.IsZero() {
		runOccurredAt = run.FinishedAt
	}
	if policy.OutcomeSignal != signal || (!runOccurredAt.IsZero() && runOccurredAt.Before(experiment.StartedAt)) {
		m.observer.ObserveProductOutcome("not_applicable")
		return false, false, nil
	}
	arm := experimentArmForVersion(experiment, run.AgentProfileVersion)
	if arm == "" {
		m.observer.ObserveProductOutcome("not_applicable")
		return false, false, nil
	}
	occurredAt := run.FinishedAt
	if occurredAt.IsZero() {
		occurredAt = time.Now()
	}
	inserted, err := m.repository.AppendProfileExperimentObservation(ctx, &repository.ProfileExperimentObservationRecord{
		ExperimentID: experiment.ID, EventID: run.RunID, ProfileVersion: run.AgentProfileVersion,
		Arm: arm, Success: true, DurationMS: run.DurationMS,
		CostMicros: run.Usage.EstimatedCostMicros, OccurredAt: occurredAt,
	})
	if err != nil {
		m.observer.ObserveProductOutcome("failed")
		return false, false, err
	}
	if inserted {
		m.observer.ObserveObservation(arm, true)
	}
	observation, replay, err := m.repository.RecordProfileExperimentOutcome(
		ctx, experiment.ID, run.RunID, signal, positive, actorUserID,
	)
	if err != nil {
		m.observer.ObserveProductOutcome("failed")
		return true, false, err
	}
	if observation != nil {
		m.observer.ObserveOutcome(observation.Arm, positive, replay)
	}
	if replay {
		m.observer.ObserveProductOutcome("replayed")
	} else {
		m.observer.ObserveProductOutcome("recorded")
	}
	return true, replay, nil
}

func (m *ProfileExperimentManager) Evaluate(ctx context.Context, experimentID primitive.ObjectID, expectedRevision int64, actorUserID uint64) (*repository.ProfileExperimentRecord, error) {
	if m == nil || m.repository == nil || m.catalogManager == nil {
		return nil, ErrProfileExperimentDisabled
	}
	if experimentID.IsZero() || expectedRevision < 1 || actorUserID == 0 {
		return nil, errors.New("experiment id, expected revision and actor are required")
	}
	experiment, err := m.repository.GetProfileExperiment(ctx, experimentID)
	if err != nil {
		return nil, err
	}
	if experiment.Status != profile.ExperimentStatusRunning || experiment.Revision != expectedRevision {
		return nil, repository.ErrProfileExperimentConflict
	}
	limit, err := profile.ExperimentObservationLimit(experiment.Policy, experiment.CandidateBasisPoints)
	if err != nil {
		return nil, err
	}
	records, err := m.repository.ListProfileExperimentObservations(ctx, experiment.ID, limit)
	if err != nil {
		return nil, err
	}
	observations := make([]profile.ExperimentObservation, 0, len(records))
	for _, record := range records {
		if record == nil {
			continue
		}
		observations = append(observations, profile.ExperimentObservation{
			Arm: record.Arm, Success: record.Success, DurationMS: record.DurationMS, CostMicros: record.CostMicros,
			OutcomeObserved: record.OutcomeObserved, OutcomePositive: record.OutcomePositive,
		})
	}
	decision, err := profile.EvaluateExperiment(experiment.Policy, observations)
	if err != nil {
		return nil, err
	}
	currentRelease, err := m.catalogManager.GetRelease(ctx, experiment.ProfileID)
	if err != nil {
		return nil, err
	}
	if !experimentMatchesRelease(experiment, currentRelease) {
		if recoveredExperimentRollback(experiment, currentRelease, decision) {
			return m.finishExperimentDecision(ctx, experiment, profile.ExperimentStatusRolledBack, decision, actorUserID)
		}
		superseded := decision
		superseded.Outcome = profile.ExperimentDecisionSuperseded
		superseded.Reason = "release_changed_during_experiment"
		return m.finishExperimentDecision(ctx, experiment, profile.ExperimentStatusSuperseded, superseded, actorUserID)
	}

	switch decision.Outcome {
	case profile.ExperimentDecisionRollback:
		if _, err := m.catalogManager.UpsertRelease(ctx, profile.Release{
			ProfileID: experiment.ProfileID, StableVersion: experiment.StableVersion,
			CandidateVersion: experiment.CandidateVersion, CandidateBasisPoints: 0, Salt: experiment.ReleaseSalt,
		}, currentRelease.Revision, actorUserID); err != nil {
			return nil, err
		}
		return m.finishExperimentDecision(ctx, experiment, profile.ExperimentStatusRolledBack, decision, actorUserID)
	case profile.ExperimentDecisionPass:
		return m.finishExperimentDecision(ctx, experiment, profile.ExperimentStatusPassed, decision, actorUserID)
	default:
		if err := m.repository.UpdateProfileExperimentDecision(ctx, experiment.ID, experiment.Revision, profile.ExperimentStatusRunning, decision.Outcome, decision.Reason, decision.Stats, actorUserID); err != nil {
			return nil, err
		}
		return m.repository.GetProfileExperiment(ctx, experiment.ID)
	}
}

func (m *ProfileExperimentManager) Stop(ctx context.Context, experimentID primitive.ObjectID, expectedRevision int64, actorUserID uint64) (*repository.ProfileExperimentRecord, error) {
	if m == nil || m.repository == nil {
		return nil, ErrProfileExperimentDisabled
	}
	experiment, err := m.repository.GetProfileExperiment(ctx, experimentID)
	if err != nil {
		return nil, err
	}
	if experiment.Status != profile.ExperimentStatusRunning || experiment.Revision != expectedRevision || actorUserID == 0 {
		return nil, repository.ErrProfileExperimentConflict
	}
	decision := profile.ExperimentGateDecision{
		Outcome: profile.ExperimentDecisionStop, Reason: "stopped_by_operator", Stats: experiment.Stats,
	}
	return m.finishExperimentDecision(ctx, experiment, profile.ExperimentStatusStopped, decision, actorUserID)
}

func (m *ProfileExperimentManager) EvaluateRunning(ctx context.Context, actorUserID uint64, limit int) error {
	if actorUserID == 0 {
		return errors.New("profile experiment automation actor is required")
	}
	if limit < 1 || limit > 100 {
		limit = 100
	}
	experiments, _, err := m.List(ctx, "", profile.ExperimentStatusRunning, 1, limit)
	if err != nil {
		return err
	}
	var joined error
	for _, experiment := range experiments {
		if experiment == nil {
			continue
		}
		_, evaluateErr := m.Evaluate(ctx, experiment.ID, experiment.Revision, actorUserID)
		if evaluateErr != nil && !errors.Is(evaluateErr, repository.ErrProfileExperimentConflict) {
			joined = errors.Join(joined, fmt.Errorf("evaluate experiment %s: %w", experiment.ID.Hex(), evaluateErr))
		}
	}
	return joined
}

func (m *ProfileExperimentManager) finishExperimentDecision(ctx context.Context, experiment *repository.ProfileExperimentRecord, status string, decision profile.ExperimentGateDecision, actorUserID uint64) (*repository.ProfileExperimentRecord, error) {
	operationID, err := newProfileOperationID()
	if err != nil {
		return nil, err
	}
	action := repository.ProfileAuditActionEvaluateExperiment
	if status == profile.ExperimentStatusStopped {
		action = repository.ProfileAuditActionStopExperiment
	}
	audit := repository.ProfileAuditEvent{
		OperationID: operationID, Action: action, ProfileID: experiment.ProfileID,
		ExperimentID: experiment.ID.Hex(), ActorUserID: actorUserID, ReleaseRevision: experiment.ReleaseRevision,
	}
	if err := m.appendExperimentAudit(ctx, audit, repository.ProfileAuditOutcomeRequested, ""); err != nil {
		return nil, fmt.Errorf("profile experiment decision audit failed before mutation: %w", err)
	}
	if err := m.repository.UpdateProfileExperimentDecision(ctx, experiment.ID, experiment.Revision, status, decision.Outcome, decision.Reason, decision.Stats, actorUserID); err != nil {
		return nil, m.finishFailedExperimentMutation(ctx, audit, err)
	}
	m.observer.ObserveDecision(status, decision.Outcome)
	if err := m.appendExperimentAudit(ctx, audit, repository.ProfileAuditOutcomeSucceeded, ""); err != nil {
		return nil, fmt.Errorf("profile experiment decision was stored but final audit failed: %w", err)
	}
	return m.repository.GetProfileExperiment(ctx, experiment.ID)
}

func (m *ProfileExperimentManager) appendExperimentAudit(ctx context.Context, base repository.ProfileAuditEvent, outcome, errorCode string) error {
	base.ID = primitive.NilObjectID
	base.Outcome = outcome
	base.ErrorCode = errorCode
	return m.auditRepository.AppendProfileAuditEvent(ctx, &base)
}

func (m *ProfileExperimentManager) finishFailedExperimentMutation(ctx context.Context, audit repository.ProfileAuditEvent, mutationErr error) error {
	if auditErr := m.appendExperimentAudit(ctx, audit, repository.ProfileAuditOutcomeFailed, profileExperimentErrorCode(mutationErr)); auditErr != nil {
		return errors.Join(mutationErr, fmt.Errorf("final profile experiment audit failed: %w", auditErr))
	}
	return mutationErr
}

func experimentMatchesRelease(experiment *repository.ProfileExperimentRecord, release *repository.ProfileReleaseRecord) bool {
	return experiment != nil && release != nil && release.Revision == experiment.ReleaseRevision &&
		release.ProfileID == experiment.ProfileID && release.StableVersion == experiment.StableVersion &&
		release.CandidateVersion == experiment.CandidateVersion && release.CandidateBasisPoints == experiment.CandidateBasisPoints &&
		release.Salt == experiment.ReleaseSalt
}

func recoveredExperimentRollback(experiment *repository.ProfileExperimentRecord, release *repository.ProfileReleaseRecord, decision profile.ExperimentGateDecision) bool {
	return experiment != nil && release != nil && decision.Outcome == profile.ExperimentDecisionRollback &&
		release.Revision == experiment.ReleaseRevision+1 && release.ProfileID == experiment.ProfileID &&
		release.StableVersion == experiment.StableVersion && release.CandidateVersion == experiment.CandidateVersion &&
		release.CandidateBasisPoints == 0 && release.Salt == experiment.ReleaseSalt
}

func profileExperimentErrorCode(err error) string {
	switch {
	case errors.Is(err, repository.ErrProfileExperimentNotFound):
		return "not_found"
	case errors.Is(err, repository.ErrProfileExperimentConflict), errors.Is(err, repository.ErrProfileExperimentAlreadyRunning):
		return "revision_conflict"
	default:
		return "persistence_failed"
	}
}

type ProfileExperimentRunRecorder struct {
	repository repository.ProfileExperimentRepository
	observer   ProfileExperimentObserver
}

func NewProfileExperimentRunRecorder(repo repository.ProfileExperimentRepository, observers ...ProfileExperimentObserver) *ProfileExperimentRunRecorder {
	var observer ProfileExperimentObserver = noopProfileExperimentObserver{}
	if len(observers) > 0 && observers[0] != nil {
		observer = observers[0]
	}
	return &ProfileExperimentRunRecorder{repository: repo, observer: observer}
}

func (r *ProfileExperimentRunRecorder) RecordRun(ctx context.Context, record agentObservability.RunRecord) error {
	if r == nil || r.repository == nil || record.AgentProfileID == "" || record.AgentProfileVersion == "" || record.RunID == "" {
		return nil
	}
	success := false
	switch record.Status {
	case string(agentRuntime.RunStatusCompleted):
		success = true
	case string(agentRuntime.RunStatusFailed):
	default:
		return nil
	}
	experiment, err := r.repository.GetRunningProfileExperiment(ctx, record.AgentProfileID)
	if errors.Is(err, repository.ErrProfileExperimentNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	arm := experimentArmForVersion(experiment, record.AgentProfileVersion)
	if arm == "" {
		return nil
	}
	occurredAt := record.FinishedAt
	if occurredAt.IsZero() {
		occurredAt = time.Now()
	}
	inserted, err := r.repository.AppendProfileExperimentObservation(ctx, &repository.ProfileExperimentObservationRecord{
		ExperimentID: experiment.ID, EventID: record.RunID, ProfileVersion: record.AgentProfileVersion,
		Arm: arm, Success: success, DurationMS: record.DurationMS,
		CostMicros: record.Usage.EstimatedCostMicros, OccurredAt: occurredAt,
	})
	if err != nil {
		return err
	}
	if inserted {
		r.observer.ObserveObservation(arm, success)
	}
	return nil
}

func experimentArmForVersion(experiment *repository.ProfileExperimentRecord, version string) string {
	if experiment == nil {
		return ""
	}
	switch strings.TrimSpace(version) {
	case experiment.StableVersion:
		return profile.ExperimentArmStable
	case experiment.CandidateVersion:
		return profile.ExperimentArmCandidate
	default:
		return ""
	}
}

func (*ProfileExperimentRunRecorder) RecordStep(context.Context, agentObservability.StepRecord) error {
	return nil
}
func (*ProfileExperimentRunRecorder) RecordLLMCall(context.Context, agentObservability.LLMCallRecord) error {
	return nil
}
func (*ProfileExperimentRunRecorder) RecordToolCall(context.Context, agentObservability.ToolCallRecord) error {
	return nil
}

type AsyncProfileExperimentRunRecorder struct {
	delegate *ProfileExperimentRunRecorder
	queue    chan agentObservability.RunRecord
}

func NewAsyncProfileExperimentRunRecorder(delegate *ProfileExperimentRunRecorder, queueSize int) (*AsyncProfileExperimentRunRecorder, error) {
	if delegate == nil {
		return nil, errors.New("profile experiment run recorder is required")
	}
	if queueSize < 1 || queueSize > profile.MaxExperimentObservationScan {
		return nil, fmt.Errorf("profile experiment observation queue size must be within 1..%d", profile.MaxExperimentObservationScan)
	}
	return &AsyncProfileExperimentRunRecorder{delegate: delegate, queue: make(chan agentObservability.RunRecord, queueSize)}, nil
}

func (r *AsyncProfileExperimentRunRecorder) RecordRun(_ context.Context, record agentObservability.RunRecord) error {
	if r == nil || r.delegate == nil || record.AgentProfileID == "" || record.AgentProfileVersion == "" || record.RunID == "" {
		return nil
	}
	if record.Status != string(agentRuntime.RunStatusCompleted) && record.Status != string(agentRuntime.RunStatusFailed) {
		return nil
	}
	select {
	case r.queue <- record:
		return nil
	default:
		return ErrProfileExperimentObservationQueueFull
	}
}

func (*AsyncProfileExperimentRunRecorder) RecordStep(context.Context, agentObservability.StepRecord) error {
	return nil
}
func (*AsyncProfileExperimentRunRecorder) RecordLLMCall(context.Context, agentObservability.LLMCallRecord) error {
	return nil
}
func (*AsyncProfileExperimentRunRecorder) RecordToolCall(context.Context, agentObservability.ToolCallRecord) error {
	return nil
}

func (r *AsyncProfileExperimentRunRecorder) Run(ctx context.Context) {
	if r == nil || r.delegate == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case record := <-r.queue:
			persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			err := r.delegate.RecordRun(persistCtx, record)
			cancel()
			if err != nil {
				slog.WarnContext(ctx, "persist Agent Profile experiment observation failed", "run_id", record.RunID, "error", err)
			}
		}
	}
}

type ProfileExperimentReconciler struct {
	manager     *ProfileExperimentManager
	actorUserID uint64
	interval    time.Duration
}

func NewProfileExperimentReconciler(manager *ProfileExperimentManager, actorUserID uint64, interval time.Duration) (*ProfileExperimentReconciler, error) {
	if manager == nil || actorUserID == 0 {
		return nil, errors.New("profile experiment manager and automation actor are required")
	}
	if interval <= 0 {
		interval = DefaultProfileExperimentReconcileInterval
	}
	return &ProfileExperimentReconciler{manager: manager, actorUserID: actorUserID, interval: interval}, nil
}

func (r *ProfileExperimentReconciler) Run(ctx context.Context) {
	if r == nil || r.manager == nil {
		return
	}
	r.evaluate(ctx)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.evaluate(ctx)
		}
	}
}

func (r *ProfileExperimentReconciler) evaluate(ctx context.Context) {
	if err := r.manager.EvaluateRunning(ctx, r.actorUserID, 100); err != nil {
		slog.WarnContext(ctx, "reconcile Agent Profile experiments failed", "error", err)
	}
}
