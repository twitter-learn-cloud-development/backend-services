package repository

import (
	"reflect"
	"strings"
	"testing"
)

func TestAgentMongoCollectionCompatibilityContract(t *testing.T) {
	want := map[string]string{
		"dialogues":              CollectionDialogues,
		"messages":               CollectionMessages,
		"workflows":              CollectionWorkflows,
		"workflow_revisions":     CollectionWorkflowRevisions,
		"workflow_runs":          CollectionRuns,
		"workflow_state_events":  CollectionWorkflowStateEvents,
		"workflow_snapshots":     CollectionWorkflowSnapshots,
		"workflow_compensations": CollectionWorkflowCompensations,
		"agent_execution_runs":   CollectionAgentExecutionRuns,
		"agent_product_events":   CollectionAgentProductEvents,
		"tool_approvals":         CollectionToolApprovals,
		"tool_executions":        CollectionToolExecutions,
		"profile_versions":       CollectionProfileVersions,
		"profile_releases":       CollectionProfileReleases,
		"profile_audits":         CollectionProfileAudits,
	}
	for contract, got := range want {
		if got == "" {
			t.Fatalf("%s collection name must not be empty", contract)
		}
	}

	if CollectionDialogues != "dialogues" {
		t.Fatalf("CollectionDialogues = %q, want dialogues", CollectionDialogues)
	}
	if CollectionMessages != "dialogue_messages" {
		t.Fatalf("CollectionMessages = %q, want dialogue_messages", CollectionMessages)
	}
	if CollectionWorkflows != "agent_workflows" {
		t.Fatalf("CollectionWorkflows = %q, want agent_workflows", CollectionWorkflows)
	}
	if CollectionRuns != "agent_workflow_runs" {
		t.Fatalf("CollectionRuns = %q, want agent_workflow_runs", CollectionRuns)
	}
	if CollectionAgentExecutionRuns != "agent_execution_runs" {
		t.Fatalf("CollectionAgentExecutionRuns = %q, want agent_execution_runs", CollectionAgentExecutionRuns)
	}
	if CollectionAgentProductEvents != "agent_product_events" {
		t.Fatalf("CollectionAgentProductEvents = %q, want agent_product_events", CollectionAgentProductEvents)
	}
	if AgentExecutionStateVersion != 2 {
		t.Fatalf("AgentExecutionStateVersion = %d, want 2", AgentExecutionStateVersion)
	}
	if CollectionWorkflowRevisions != "agent_workflow_revisions" {
		t.Fatalf("CollectionWorkflowRevisions = %q, want agent_workflow_revisions", CollectionWorkflowRevisions)
	}
	if CollectionWorkflowStateEvents != "agent_workflow_state_events" {
		t.Fatalf("CollectionWorkflowStateEvents = %q, want agent_workflow_state_events", CollectionWorkflowStateEvents)
	}
	if CollectionWorkflowSnapshots != "agent_workflow_state_snapshots" {
		t.Fatalf("CollectionWorkflowSnapshots = %q, want agent_workflow_state_snapshots", CollectionWorkflowSnapshots)
	}
	if CollectionWorkflowCompensations != "agent_workflow_compensations" {
		t.Fatalf("CollectionWorkflowCompensations = %q, want agent_workflow_compensations", CollectionWorkflowCompensations)
	}
	if CollectionProfileVersions != "agent_profile_versions" {
		t.Fatalf("CollectionProfileVersions = %q, want agent_profile_versions", CollectionProfileVersions)
	}
	if CollectionProfileReleases != "agent_profile_releases" {
		t.Fatalf("CollectionProfileReleases = %q, want agent_profile_releases", CollectionProfileReleases)
	}
	if CollectionProfileAudits != "agent_profile_audit_events" {
		t.Fatalf("CollectionProfileAudits = %q, want agent_profile_audit_events", CollectionProfileAudits)
	}
}

func TestAgentDialogueModeCompatibilityContract(t *testing.T) {
	want := map[DialogueMode]int32{
		ModeChat:     1,
		ModeConsult:  2,
		ModeAssist:   3,
		ModeMulti:    4,
		ModeWorkflow: 5,
	}
	for mode, number := range want {
		if int32(mode) != number {
			t.Fatalf("dialogue mode %d changed, want %d", mode, number)
		}
	}
}

func TestAgentMongoBSONFieldCompatibilityContract(t *testing.T) {
	assertBSONField(t, reflect.TypeOf(Dialogue{}), "UserID", "user_id")
	assertBSONField(t, reflect.TypeOf(Dialogue{}), "Mode", "mode")
	assertBSONField(t, reflect.TypeOf(DialogueMessage{}), "DialogueID", "dialogue_id")
	assertBSONField(t, reflect.TypeOf(DialogueMessage{}), "ToolCallID", "tool_call_id")
	assertBSONField(t, reflect.TypeOf(WorkflowDefinition{}), "DSLJSON", "dsl_json")
	assertBSONField(t, reflect.TypeOf(WorkflowDefinition{}), "CurrentRevisionID", "current_revision_id")
	assertBSONField(t, reflect.TypeOf(WorkflowRevision{}), "RevisionNumber", "revision_number")
	assertBSONField(t, reflect.TypeOf(WorkflowRunRecord{}), "InputJSON", "input_json")
	assertBSONField(t, reflect.TypeOf(WorkflowRunRecord{}), "OutputJSON", "output_json")
	assertBSONField(t, reflect.TypeOf(WorkflowRunRecord{}), "CheckpointJSON", "checkpoint_json")
	assertBSONField(t, reflect.TypeOf(WorkflowRunRecord{}), "ApprovalRequestID", "approval_request_id")
	assertBSONField(t, reflect.TypeOf(WorkflowRunRecord{}), "ResumeTokenHash", "resume_token_hash")
	assertBSONField(t, reflect.TypeOf(WorkflowRunRecord{}), "ResumeGrantIssuedAt", "resume_grant_issued_at")
	assertBSONField(t, reflect.TypeOf(WorkflowRunRecord{}), "ResumeGrantExpiresAt", "resume_grant_expires_at")
	assertBSONField(t, reflect.TypeOf(WorkflowRunRecord{}), "WorkflowRevisionID", "workflow_revision_id")
	assertBSONField(t, reflect.TypeOf(WorkflowRunRecord{}), "StateVersion", "state_version")
	assertBSONField(t, reflect.TypeOf(WorkflowStateEvent{}), "EventHash", "event_hash")
	assertBSONField(t, reflect.TypeOf(WorkflowStateSnapshot{}), "SnapshotHash", "snapshot_hash")
	assertBSONField(t, reflect.TypeOf(WorkflowCompensationRecord{}), "PlanHash", "plan_hash")
	assertBSONField(t, reflect.TypeOf(WorkflowCompensationRecord{}), "IdempotencyKey", "idempotency_key")
	assertBSONField(t, reflect.TypeOf(WorkflowCompensationRecord{}), "AttemptID", "attempt_id")
	assertBSONField(t, reflect.TypeOf(WorkflowCompensationRecord{}), "OutputJSON", "output_json")
	assertBSONField(t, reflect.TypeOf(AgentExecutionRun{}), "ExecutionProfile", "execution_profile")
	assertBSONField(t, reflect.TypeOf(AgentExecutionRun{}), "SkillID", "skill_id")
	assertBSONField(t, reflect.TypeOf(AgentExecutionRun{}), "SkillVersion", "skill_version")
	assertBSONField(t, reflect.TypeOf(AgentExecutionRun{}), "ExecutionStrategyPlan", "execution_strategy_plan")
	assertBSONField(t, reflect.TypeOf(AgentExecutionRun{}), "InputDigest", "input_digest")
	assertBSONField(t, reflect.TypeOf(AgentExecutionRun{}), "ResumeSupported", "resume_supported")
	assertBSONField(t, reflect.TypeOf(AgentExecutionRun{}), "PendingActionID", "pending_action_id")
	assertBSONField(t, reflect.TypeOf(AgentExecutionRun{}), "ApprovalRequestID", "approval_request_id")
	assertBSONField(t, reflect.TypeOf(AgentExecutionRun{}), "ApprovalInputDigest", "approval_input_digest")
	assertBSONField(t, reflect.TypeOf(AgentExecutionRun{}), "ApprovalIdempotencyKey", "approval_idempotency_key")
	assertBSONField(t, reflect.TypeOf(AgentExecutionRun{}), "ResumeTokenHash", "resume_token_hash")
	assertBSONField(t, reflect.TypeOf(ToolApprovalRequest{}), "RedactedInputs", "redacted_inputs")
	assertBSONField(t, reflect.TypeOf(ToolExecutionRecord{}), "IdempotencyKey", "idempotency_key")
	assertBSONField(t, reflect.TypeOf(ProfileVersionRecord{}), "SnapshotSchema", "snapshot_schema")
	assertBSONField(t, reflect.TypeOf(ProfileVersionRecord{}), "SnapshotJSON", "snapshot_json")
	assertBSONField(t, reflect.TypeOf(ProfileVersionRecord{}), "SnapshotHash", "snapshot_hash")
	assertBSONField(t, reflect.TypeOf(ProfileReleaseRecord{}), "CandidateBasisPoints", "candidate_basis_points")
	assertBSONField(t, reflect.TypeOf(ProfileReleaseRecord{}), "Revision", "revision")
	assertBSONField(t, reflect.TypeOf(ProfileAuditEvent{}), "OperationID", "operation_id")
	assertBSONField(t, reflect.TypeOf(ProfileAuditEvent{}), "ActorUserID", "actor_user_id")
	assertBSONField(t, reflect.TypeOf(ProfileAuditEvent{}), "SnapshotHash", "snapshot_hash")
}

func assertBSONField(t *testing.T, typ reflect.Type, fieldName, want string) {
	t.Helper()
	field, ok := typ.FieldByName(fieldName)
	if !ok {
		t.Fatalf("%s.%s is missing", typ.Name(), fieldName)
	}
	got := strings.Split(field.Tag.Get("bson"), ",")[0]
	if got != want {
		t.Fatalf("%s.%s bson field = %q, want %q", typ.Name(), fieldName, got, want)
	}
}
