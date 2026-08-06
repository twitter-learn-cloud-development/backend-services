package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"twitter-clone/internal/module/agent/repository"
)

// WorkflowCompensationJournal is a tenant-scoped, redacted operations view.
// Raw inputs, outputs, idempotency keys and approval parameters never cross
// this service boundary.
type WorkflowCompensationJournal struct {
	Run            *repository.WorkflowRunRecord
	Entries        []WorkflowCompensationJournalEntry
	NextSequence   int
	RetryAvailable bool
}

type WorkflowCompensationJournalEntry struct {
	Sequence          int
	SourceNodeID      string
	StepID            string
	ToolName          string
	InputHash         string
	PlanHash          string
	Status            string
	Attempt           int
	ErrorMessage      string
	ApprovalRequestID string
	LeaseUntil        int64
	CreatedAt         int64
	UpdatedAt         int64
	FinishedAt        int64
	IsNext            bool
}

func (s *AgentService) GetWorkflowCompensationJournal(
	ctx context.Context,
	userID uint64,
	runID string,
) (*WorkflowCompensationJournal, error) {
	if s.repo == nil {
		return nil, errors.New("agent repository is not initialized")
	}
	runOID, err := primitive.ObjectIDFromHex(runID)
	if err != nil {
		return nil, fmt.Errorf("invalid run_id: %w", err)
	}
	run, err := s.repo.GetWorkflowRun(ctx, runOID, userID)
	if err != nil {
		return nil, err
	}
	compensationRepo, ok := s.repo.(repository.WorkflowCompensationRepository)
	if !ok {
		return nil, errors.New("workflow compensation repository is not available")
	}
	records, err := compensationRepo.ListWorkflowCompensations(ctx, run.ID, run.UserID)
	if err != nil {
		return nil, err
	}

	journal := &WorkflowCompensationJournal{Run: run}
	next := nextWorkflowCompensation(records)
	if next != nil {
		journal.NextSequence = next.Sequence
		journal.RetryAvailable = isManualCompensationRetryAvailable(run.Status, next, time.Now())
	}
	journal.Entries = make([]WorkflowCompensationJournalEntry, 0, len(records))
	for _, record := range records {
		if record == nil {
			continue
		}
		journal.Entries = append(journal.Entries, workflowCompensationJournalEntry(record, next))
	}
	return journal, nil
}

// RetryWorkflowCompensation is an explicit control-plane operation. It only
// resumes a persisted compensation plan and never reruns the main workflow.
func (s *AgentService) RetryWorkflowCompensation(
	ctx context.Context,
	userID uint64,
	runID string,
) (*WorkflowExecutionResult, error) {
	journal, err := s.GetWorkflowCompensationJournal(ctx, userID, runID)
	if err != nil {
		return nil, err
	}
	if journal.NextSequence == 0 {
		return nil, errors.New("workflow compensation journal has no unfinished entry")
	}
	if !journal.RetryAvailable {
		return nil, fmt.Errorf("workflow compensation cannot be retried while run status is %s", journal.Run.Status)
	}
	return s.driveWorkflowCompensations(
		ctx,
		&WorkflowExecutionResult{Run: journal.Run},
		primitive.NilObjectID,
		true,
	)
}

func workflowCompensationJournalEntry(
	record *repository.WorkflowCompensationRecord,
	next *repository.WorkflowCompensationRecord,
) WorkflowCompensationJournalEntry {
	approvalID := ""
	if !record.ApprovalRequestID.IsZero() {
		approvalID = record.ApprovalRequestID.Hex()
	}
	return WorkflowCompensationJournalEntry{
		Sequence: record.Sequence, SourceNodeID: record.SourceNodeID, StepID: record.StepID,
		ToolName: record.ToolName, InputHash: record.InputHash, PlanHash: record.PlanHash,
		Status: record.Status, Attempt: record.Attempt, ErrorMessage: record.ErrorMessage,
		ApprovalRequestID: approvalID, LeaseUntil: unixTime(record.LeaseUntil),
		CreatedAt: unixTime(record.CreatedAt), UpdatedAt: unixTime(record.UpdatedAt),
		FinishedAt: unixTime(record.FinishedAt),
		IsNext:     next != nil && record.ID == next.ID,
	}
}

func isManualCompensationRetryAvailable(
	runStatus string,
	record *repository.WorkflowCompensationRecord,
	now time.Time,
) bool {
	if !isRecoverableCompensationRunStatus(runStatus) || record == nil {
		return false
	}
	switch record.Status {
	case repository.WorkflowCompensationStatusPlanned, repository.WorkflowCompensationStatusFailed:
		return true
	case repository.WorkflowCompensationStatusExecuting:
		return record.LeaseUntil.IsZero() || !record.LeaseUntil.After(now)
	default:
		return false
	}
}
