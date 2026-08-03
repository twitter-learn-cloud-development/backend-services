package repository

import (
	"testing"
	"time"
)

func TestNormalizeNewAgentExecutionRunAppliesVersionedRunningDefaults(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	run := &AgentExecutionRun{
		ID: "run-1", UserID: 42, ExecutionProfile: "runtime.chat",
		CapabilityIDs: []string{"conversation.reply"}, InputDigest: "digest",
	}
	if err := normalizeNewAgentExecutionRun(run, now); err != nil {
		t.Fatalf("normalizeNewAgentExecutionRun() error = %v", err)
	}
	if run.Status != AgentExecutionRunRunning || run.Revision != 1 ||
		run.StateVersion != AgentExecutionStateVersion || !run.StartedAt.Equal(now) || !run.UpdatedAt.Equal(now) {
		t.Fatalf("normalized run = %+v", run)
	}
}

func TestValidateAgentExecutionRunCommitRejectsRunningTarget(t *testing.T) {
	err := validateAgentExecutionRunCommit(AgentExecutionRunCommit{
		RunID: "run-1", UserID: 42, ExpectedRevision: 1, Status: AgentExecutionRunRunning,
	})
	if err == nil {
		t.Fatal("validateAgentExecutionRunCommit() error = nil, want invalid target")
	}
}

func TestValidateAgentExecutionRunCommitRequiresCompletedPublishableDraft(t *testing.T) {
	err := validateAgentExecutionRunCommit(AgentExecutionRunCommit{
		RunID: "run-1", UserID: 42, ExpectedRevision: 1,
		Status: AgentExecutionRunFailed, PublishableDraft: true,
	})
	if err == nil {
		t.Fatal("validateAgentExecutionRunCommit() accepted a publishable failed run")
	}
}

func TestValidateAgentExecutionRunCommitValidatesAccounting(t *testing.T) {
	valid := AgentExecutionRunCommit{
		RunID:             "run-1",
		UserID:            42,
		ExpectedRevision:  1,
		Status:            AgentExecutionRunCompleted,
		AccountingVersion: ExecutionAccountingVersion,
	}
	if err := validateAgentExecutionRunCommit(valid); err != nil {
		t.Fatalf("validateAgentExecutionRunCommit() valid accounting error = %v", err)
	}

	negative := valid
	negative.TotalTokens = -1
	if err := validateAgentExecutionRunCommit(negative); err == nil {
		t.Fatal("validateAgentExecutionRunCommit() negative accounting error = nil")
	}

	unsupported := valid
	unsupported.AccountingVersion = "execution.accounting.v999"
	if err := validateAgentExecutionRunCommit(unsupported); err == nil {
		t.Fatal("validateAgentExecutionRunCommit() unsupported accounting version error = nil")
	}
}

func TestSuspendedAgentExecutionStatusesAreExplicit(t *testing.T) {
	if !isSuspendedAgentExecutionStatus(AgentExecutionRunAwaitingHuman) ||
		!isSuspendedAgentExecutionStatus(AgentExecutionRunApprovalRequired) ||
		isSuspendedAgentExecutionStatus(AgentExecutionRunCompleted) {
		t.Fatal("suspended Agent execution status classification changed")
	}
}

func TestValidateAgentExecutionRunCommitRequiresCompleteResumableCheckpoint(t *testing.T) {
	commit := AgentExecutionRunCommit{
		RunID: "run-1", UserID: 42, ExpectedRevision: 1,
		Status: AgentExecutionRunAwaitingHuman, PendingActionType: "ask_human",
		ResumeSupported: true,
	}
	if err := validateAgentExecutionRunCommit(commit); err == nil {
		t.Fatal("validateAgentExecutionRunCommit() error = nil")
	}
	commit.CheckpointVersion = "react.v1"
	commit.CheckpointKeyID = "v1"
	commit.CheckpointNonce = "nonce"
	commit.CheckpointCiphertext = "ciphertext"
	commit.CheckpointDigest = "digest"
	commit.CheckpointSizeBytes = 128
	if err := validateAgentExecutionRunCommit(commit); err != nil {
		t.Fatalf("validateAgentExecutionRunCommit() error = %v", err)
	}
}

func TestValidateAgentExecutionRunCommitAcceptsHumanToolContinuation(t *testing.T) {
	commit := AgentExecutionRunCommit{
		RunID: "run-1", UserID: 42, ExpectedRevision: 1,
		Status:            AgentExecutionRunAwaitingHuman,
		PendingActionType: "tool_call", PendingActionName: "workflow_507f1f77bcf86cd799439011",
		PendingActionID: "action-1", PendingResumeKind: AgentExecutionResumeHuman,
		ResumeSupported: true, CheckpointVersion: "react.v1", CheckpointKeyID: "v1",
		CheckpointNonce: "nonce", CheckpointCiphertext: "ciphertext",
		CheckpointDigest: "digest", CheckpointSizeBytes: 128,
	}
	if err := validateAgentExecutionRunCommit(commit); err != nil {
		t.Fatalf("validateAgentExecutionRunCommit() error = %v", err)
	}
	commit.PendingResumeKind = "unknown"
	if err := validateAgentExecutionRunCommit(commit); err == nil {
		t.Fatal("validateAgentExecutionRunCommit() unsupported resume kind error = nil")
	}
}

func TestValidateAgentExecutionRunClaimRequiresLeaseAndAttempt(t *testing.T) {
	if err := validateAgentExecutionRunClaim(AgentExecutionRunClaim{
		RunID: "run-1", UserID: 42, ExpectedRevision: 2,
	}); err == nil {
		t.Fatal("validateAgentExecutionRunClaim() error = nil")
	}
	if err := validateAgentExecutionRunClaim(AgentExecutionRunClaim{
		RunID: "run-1", UserID: 42, ExpectedRevision: 2,
		AttemptID: "attempt-1", LeaseDuration: time.Minute,
	}); err != nil {
		t.Fatalf("validateAgentExecutionRunClaim() error = %v", err)
	}
}
