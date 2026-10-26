package deletion

import (
	"errors"
	"testing"
	"time"
)

func testInventory() Inventory {
	return Inventory{
		Owned:       map[string]int64{"dialogues": 2, "tool_executions": 3},
		Related:     map[string]int64{"project_memberships": 1},
		Retained:    map[string]int64{"product_events": 4},
		Unsupported: []string{"qdrant_episodic_memory", "minio_tool_result_objects", "tweet_service_published_content"},
	}
}

func TestBuildProtocolFailsClosedWithoutExecutionCapabilities(t *testing.T) {
	now := time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)
	protocol, err := BuildProtocol(testInventory(), EmptyCapabilityRegistry().Snapshot(), now)
	if err != nil {
		t.Fatal(err)
	}
	if protocol.Plan.Ready || len(protocol.Plan.Blockers) != len(defaultStages()) {
		t.Fatalf("plan should expose every missing capability: %+v", protocol.Plan)
	}
	if protocol.Checkpoint.Revision != 1 || len(protocol.Checkpoint.Stages) != len(protocol.Plan.Stages) {
		t.Fatalf("unexpected initial checkpoint: %+v", protocol.Checkpoint)
	}
	if _, err := ClaimNextStage(&protocol.Checkpoint, protocol.Plan, 1, "worker", now, now.Add(time.Minute)); !errors.Is(err, ErrPlanNotReady) {
		t.Fatalf("ClaimNextStage() error = %v, want plan not ready", err)
	}
}

func TestBuildPlanIsDeterministicForMapOrder(t *testing.T) {
	now := time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)
	left := testInventory()
	right := Inventory{
		Owned:       map[string]int64{"tool_executions": 3, "dialogues": 2},
		Related:     map[string]int64{"project_memberships": 1},
		Retained:    map[string]int64{"product_events": 4},
		Unsupported: []string{"tweet_service_published_content", "minio_tool_result_objects", "qdrant_episodic_memory"},
	}
	leftPlan, err := BuildPlan(left, allCapabilitySnapshot(t), now)
	if err != nil {
		t.Fatal(err)
	}
	rightPlan, err := BuildPlan(right, allCapabilitySnapshot(t), now)
	if err != nil {
		t.Fatal(err)
	}
	if !leftPlan.Ready || leftPlan.Digest != rightPlan.Digest || leftPlan.InventoryDigest != rightPlan.InventoryDigest {
		t.Fatalf("plan digest is not deterministic: left=%+v right=%+v", leftPlan, rightPlan)
	}
}

func TestCheckpointRunsStrictlyInOrderWithReceipts(t *testing.T) {
	now := time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)
	protocol, err := BuildProtocol(testInventory(), allCapabilitySnapshot(t), now)
	if err != nil {
		t.Fatal(err)
	}
	stage, err := ClaimNextStage(&protocol.Checkpoint, protocol.Plan, 1, "worker-1", now, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if stage.StageID != StageEnforceWriteFence || stage.Attempts != 1 || protocol.Checkpoint.Revision != 2 {
		t.Fatalf("unexpected first claim: stage=%+v checkpoint=%+v", stage, protocol.Checkpoint)
	}
	if err := CompleteStage(&protocol.Checkpoint, protocol.Plan, 2, "worker-1", StageSucceeded, sha256Hex("fence-receipt"), "", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	next, err := ClaimNextStage(&protocol.Checkpoint, protocol.Plan, 3, "worker-2", now.Add(2*time.Second), now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if next.StageID != StageDetachRelationships {
		t.Fatalf("next stage = %q, want %q", next.StageID, StageDetachRelationships)
	}
}

func TestCheckpointReclaimsExpiredLeaseAndRetriesFailure(t *testing.T) {
	now := time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)
	protocol, err := BuildProtocol(testInventory(), allCapabilitySnapshot(t), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ClaimNextStage(&protocol.Checkpoint, protocol.Plan, 1, "worker-1", now, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := ClaimNextStage(&protocol.Checkpoint, protocol.Plan, 2, "worker-2", now.Add(500*time.Millisecond), now.Add(time.Minute)); !errors.Is(err, ErrStageBusy) {
		t.Fatalf("active lease claim error = %v, want busy", err)
	}
	reclaimed, err := ClaimNextStage(&protocol.Checkpoint, protocol.Plan, 2, "worker-2", now.Add(2*time.Second), now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if reclaimed.Attempts != 2 {
		t.Fatalf("reclaimed attempts = %d, want 2", reclaimed.Attempts)
	}
	if err := CompleteStage(&protocol.Checkpoint, protocol.Plan, 3, "worker-2", StageFailed, "", "dependency_unavailable", now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	retried, err := ClaimNextStage(&protocol.Checkpoint, protocol.Plan, 4, "worker-3", now.Add(4*time.Second), now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if retried.StageID != StageEnforceWriteFence || retried.Attempts != 3 {
		t.Fatalf("unexpected retry: %+v", retried)
	}
}

func TestCheckpointRejectsCompletionAfterLeaseExpiry(t *testing.T) {
	now := time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)
	protocol, err := BuildProtocol(testInventory(), allCapabilitySnapshot(t), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ClaimNextStage(&protocol.Checkpoint, protocol.Plan, 1, "worker-1", now, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	err = CompleteStage(&protocol.Checkpoint, protocol.Plan, 2, "worker-1", StageSucceeded, sha256Hex("late-receipt"), "", now.Add(2*time.Second))
	if !errors.Is(err, ErrStageLeaseExpired) {
		t.Fatalf("CompleteStage() error = %v, want expired lease", err)
	}
}

func TestCheckpointRejectsTamperedStageOrder(t *testing.T) {
	now := time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)
	protocol, err := BuildProtocol(testInventory(), allCapabilitySnapshot(t), now)
	if err != nil {
		t.Fatal(err)
	}
	protocol.Checkpoint.Stages[1].Status = StageSucceeded
	protocol.Checkpoint.Stages[1].ReceiptDigest = sha256Hex("forged")
	protocol.Checkpoint.Stages[1].CompletedAt = now
	if _, err := ClaimNextStage(&protocol.Checkpoint, protocol.Plan, 1, "worker-1", now, now.Add(time.Minute)); err == nil {
		t.Fatal("tampered checkpoint was accepted")
	}
}

func TestCheckpointRejectsUnboundedLeaseAndUnsafeFailureCode(t *testing.T) {
	now := time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)
	protocol, err := BuildProtocol(testInventory(), allCapabilitySnapshot(t), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ClaimNextStage(&protocol.Checkpoint, protocol.Plan, 1, "worker-1", now, now.Add(maxStageLease+time.Second)); err == nil {
		t.Fatal("unbounded lease was accepted")
	}
	if _, err := ClaimNextStage(&protocol.Checkpoint, protocol.Plan, 1, "worker-1", now, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := CompleteStage(&protocol.Checkpoint, protocol.Plan, 2, "worker-1", StageFailed, "", "dependency unavailable!", now.Add(time.Second)); err == nil {
		t.Fatal("unsafe failure code was accepted")
	}
}

func TestBuildPlanRejectsUnknownExternalResource(t *testing.T) {
	inventory := testInventory()
	inventory.Unsupported = append(inventory.Unsupported, "unknown_external_store")
	plan, err := BuildPlan(inventory, allCapabilitySnapshot(t), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Ready || len(plan.Blockers) != 1 || plan.Blockers[0] != "unsupported_resource:unknown_external_store" {
		t.Fatalf("unknown resource did not fail closed: %+v", plan)
	}
}

func TestBuildPlanRequiresExplicitSharedOwnershipResolution(t *testing.T) {
	now := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	plan, err := BuildPlan(Inventory{Related: map[string]int64{
		"project_ownerships": 1, "project_memberships": 2,
		"project_scoped_mcp_connections_created": 1,
	}}, allCapabilitySnapshot(t), now)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Ready || len(plan.Blockers) != 2 ||
		plan.Blockers[0] != "relationship_resolution_required:project_ownerships" ||
		plan.Blockers[1] != "relationship_resolution_required:project_scoped_mcp_connections_created" {
		t.Fatalf("unexpected shared ownership plan: %+v", plan)
	}
}

func TestValidatePlanRejectsTampering(t *testing.T) {
	plan, err := BuildPlan(testInventory(), allCapabilitySnapshot(t), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	plan.Stages[2].DependsOn = []string{StageEnforceWriteFence}
	if err := ValidatePlan(plan); err == nil {
		t.Fatal("tampered dependency chain was accepted")
	}
}

func TestValidatePlanRejectsReadinessTampering(t *testing.T) {
	plan, err := BuildPlan(testInventory(), EmptyCapabilityRegistry().Snapshot(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	plan.Ready = true
	if err := ValidatePlan(plan); err == nil {
		t.Fatal("tampered readiness was accepted")
	}
}

func TestValidateProtocolRejectsCheckpointTampering(t *testing.T) {
	protocol, err := BuildProtocol(testInventory(), allCapabilitySnapshot(t), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	protocol.Checkpoint.Stages[0].StageID = StageDeleteMongo
	if err := ValidateProtocol(*protocol); err == nil {
		t.Fatal("tampered checkpoint was accepted")
	}
}
