package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"twitter-clone/internal/module/agent/repository"
)

const (
	DefaultDataRetentionInterval         = 6 * time.Hour
	DefaultDataRetentionLeaseDuration    = 15 * time.Minute
	DefaultDataRetentionOperationTimeout = 2 * time.Minute
	DefaultDialogueRetention             = 180 * 24 * time.Hour
	DefaultOrphanMessageGracePeriod      = 24 * time.Hour
	DefaultTraceRetention                = 30 * 24 * time.Hour
	DefaultTerminalCheckpointRetention   = 7 * 24 * time.Hour
	DefaultSuspendedCheckpointRetention  = 30 * 24 * time.Hour
	DefaultToolResultRetention           = 30 * 24 * time.Hour
	DefaultDataRetentionBatchSize        = 100
	dataRetentionLeaseName               = "agent-application-data"
)

type DataRetentionConfig struct {
	Enabled                      bool
	DryRun                       bool
	Interval                     time.Duration
	LeaseDuration                time.Duration
	OperationTimeout             time.Duration
	DialogueRetention            time.Duration
	OrphanMessageGracePeriod     time.Duration
	TraceRetention               time.Duration
	TerminalCheckpointRetention  time.Duration
	SuspendedCheckpointRetention time.Duration
	ToolResultRetention          time.Duration
	BatchSize                    int
}

func DefaultDataRetentionConfig() DataRetentionConfig {
	return DataRetentionConfig{
		DryRun: true, Interval: DefaultDataRetentionInterval,
		LeaseDuration:                DefaultDataRetentionLeaseDuration,
		OperationTimeout:             DefaultDataRetentionOperationTimeout,
		DialogueRetention:            DefaultDialogueRetention,
		OrphanMessageGracePeriod:     DefaultOrphanMessageGracePeriod,
		TraceRetention:               DefaultTraceRetention,
		TerminalCheckpointRetention:  DefaultTerminalCheckpointRetention,
		SuspendedCheckpointRetention: DefaultSuspendedCheckpointRetention,
		ToolResultRetention:          DefaultToolResultRetention,
		BatchSize:                    DefaultDataRetentionBatchSize,
	}
}

func (c DataRetentionConfig) Validate() error {
	if c.Interval <= 0 || c.LeaseDuration <= 0 || c.OperationTimeout <= 0 {
		return errors.New("data retention worker intervals must be positive")
	}
	if c.LeaseDuration < c.OperationTimeout {
		return errors.New("data retention lease duration must cover the operation timeout")
	}
	if c.DialogueRetention <= 0 || c.OrphanMessageGracePeriod <= 0 || c.TraceRetention <= 0 || c.TerminalCheckpointRetention <= 0 ||
		c.SuspendedCheckpointRetention <= 0 || c.ToolResultRetention <= 0 {
		return errors.New("data retention periods must be positive")
	}
	if c.SuspendedCheckpointRetention < c.TerminalCheckpointRetention {
		return errors.New("suspended checkpoint retention must not be shorter than terminal checkpoint retention")
	}
	if c.BatchSize <= 0 || c.BatchSize > 1000 {
		return errors.New("data retention batch size must be between 1 and 1000")
	}
	return nil
}

type DataRetentionObserver interface {
	ObserveDataRetention(resource repository.DataRetentionResource, outcome string)
	ObserveDataRetentionCycle(outcome string, duration time.Duration)
}

type noopDataRetentionObserver struct{}

func (noopDataRetentionObserver) ObserveDataRetention(repository.DataRetentionResource, string) {}
func (noopDataRetentionObserver) ObserveDataRetentionCycle(string, time.Duration)               {}

type DataRetentionSweepReport struct {
	LeaseAcquired    bool
	Candidates       int
	Purged           int
	Skipped          int
	Failed           int
	PrimaryDocuments int64
	RelatedDocuments int64
}

type DataRetentionWorker struct {
	repository repository.DataRetentionRepository
	objects    ToolResultObjectStore
	observer   DataRetentionObserver
	config     DataRetentionConfig
	owner      string
	now        func() time.Time
}

func NewDataRetentionWorker(
	repo repository.DataRetentionRepository,
	objects ToolResultObjectStore,
	observer DataRetentionObserver,
	config DataRetentionConfig,
	owner string,
) (*DataRetentionWorker, error) {
	if repo == nil {
		return nil, errors.New("data retention repository is required")
	}
	if owner == "" {
		return nil, errors.New("data retention worker owner is required")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if observer == nil {
		observer = noopDataRetentionObserver{}
	}
	return &DataRetentionWorker{
		repository: repo, objects: objects, observer: observer, config: config,
		owner: owner, now: func() time.Time { return time.Now().UTC() },
	}, nil
}

func (w *DataRetentionWorker) Run(ctx context.Context) {
	if w == nil || !w.config.Enabled {
		return
	}
	w.runCycle(ctx)
	ticker := time.NewTicker(w.config.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.runCycle(ctx)
		}
	}
}

func (w *DataRetentionWorker) runCycle(parent context.Context) {
	startedAt := w.now()
	ctx, cancel := context.WithTimeout(parent, w.config.OperationTimeout)
	defer cancel()
	report, err := w.Sweep(ctx, startedAt)
	outcome := "success"
	if err != nil || report.Failed > 0 {
		outcome = "failed"
	} else if !report.LeaseAcquired {
		outcome = "lease_skipped"
	} else if w.config.DryRun {
		outcome = "dry_run"
	}
	w.observer.ObserveDataRetentionCycle(outcome, w.now().Sub(startedAt))
	if err != nil {
		slog.ErrorContext(ctx, "Agent application data retention cycle failed", "error", err, "report", report)
		return
	}
	if report.Candidates > 0 {
		slog.InfoContext(ctx, "Agent application data retention cycle completed", "dry_run", w.config.DryRun, "report", report)
	}
}

func (w *DataRetentionWorker) Sweep(ctx context.Context, now time.Time) (DataRetentionSweepReport, error) {
	if w == nil {
		return DataRetentionSweepReport{}, errors.New("data retention worker is nil")
	}
	if now.IsZero() {
		now = w.now()
	}
	report := DataRetentionSweepReport{}
	acquired, err := w.repository.AcquireDataRetentionLease(
		ctx, dataRetentionLeaseName, w.owner, now, now.Add(w.config.LeaseDuration),
	)
	if err != nil {
		return report, err
	}
	report.LeaseAcquired = acquired
	if !acquired {
		return report, nil
	}
	candidates, err := w.repository.ListDataRetentionCandidates(ctx, repository.DataRetentionCutoffs{
		Now:                 now,
		Dialogue:            now.Add(-w.config.DialogueRetention),
		OrphanMessage:       now.Add(-w.config.OrphanMessageGracePeriod),
		Trace:               now.Add(-w.config.TraceRetention),
		TerminalCheckpoint:  now.Add(-w.config.TerminalCheckpointRetention),
		SuspendedCheckpoint: now.Add(-w.config.SuspendedCheckpointRetention),
		ToolResult:          now.Add(-w.config.ToolResultRetention),
	}, w.config.BatchSize)
	if err != nil {
		return report, err
	}
	report.Candidates = len(candidates)
	var sweepErr error
	for _, candidate := range candidates {
		w.observer.ObserveDataRetention(candidate.Resource, "candidate")
		if w.config.DryRun {
			report.Skipped++
			w.observer.ObserveDataRetention(candidate.Resource, "dry_run")
			continue
		}
		if err := w.purgeCandidate(ctx, now, candidate, &report); err != nil {
			report.Failed++
			w.observer.ObserveDataRetention(candidate.Resource, "failed")
			sweepErr = errors.Join(sweepErr, fmt.Errorf("purge %s: %w", candidate.Resource, err))
			continue
		}
	}
	return report, sweepErr
}

func (w *DataRetentionWorker) purgeCandidate(
	ctx context.Context,
	now time.Time,
	candidate repository.DataRetentionCandidate,
	report *DataRetentionSweepReport,
) error {
	if candidate.UserID == 0 {
		return errors.New("data retention candidate has no subject owner")
	}
	token, err := newDataRetentionClaimToken()
	if err != nil {
		return err
	}
	subjectState, err := w.repository.AcquireDataRetentionSubjectLease(
		ctx, candidate.UserID, token, now, now.Add(w.config.OperationTimeout),
	)
	if err != nil {
		return err
	}
	if subjectState != repository.DataRetentionSubjectLeaseAcquired {
		report.Skipped++
		outcome := "subject_busy"
		if subjectState == repository.DataRetentionSubjectHeld {
			outcome = "legal_hold"
		}
		w.observer.ObserveDataRetention(candidate.Resource, outcome)
		return nil
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = w.repository.ReleaseDataRetentionSubjectLease(releaseCtx, candidate.UserID, token)
	}()
	claimed, err := w.repository.ClaimDataRetentionCandidate(
		ctx, candidate, token, now, now.Add(w.config.OperationTimeout),
	)
	if err != nil {
		return err
	}
	if !claimed {
		report.Skipped++
		w.observer.ObserveDataRetention(candidate.Resource, "claim_skipped")
		return nil
	}
	release := true
	defer func() {
		if release {
			releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_ = w.repository.ReleaseDataRetentionCandidate(releaseCtx, candidate, token)
		}
	}()
	if candidate.OutputReference != nil {
		if w.objects == nil {
			return errors.New("tool result object store is required to purge an archived result")
		}
		if err := w.objects.Delete(ctx, toolResultReferenceFromRepository(candidate.OutputReference)); err != nil {
			return fmt.Errorf("delete archived tool result: %w", err)
		}
	}
	purged, err := w.repository.CompleteDataRetentionCandidate(ctx, candidate, token, now)
	if err != nil {
		return err
	}
	release = false
	if purged.PrimaryDocuments == 0 {
		report.Skipped++
		w.observer.ObserveDataRetention(candidate.Resource, "completion_skipped")
		return nil
	}
	report.Purged++
	report.PrimaryDocuments += purged.PrimaryDocuments
	report.RelatedDocuments += purged.RelatedDocuments
	w.observer.ObserveDataRetention(candidate.Resource, "purged")
	return nil
}

func newDataRetentionClaimToken() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate data retention claim token: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}
