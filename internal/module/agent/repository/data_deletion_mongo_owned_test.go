package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"testing"
	"time"

	agentdeletion "twitter-clone/internal/module/agent/deletion"
)

func TestMongoOwnedDeletionManifestIsFixedAndExcludesSharedControlData(t *testing.T) {
	resources := mongoOwnedDeletionResources(42)
	wantNames := []string{
		"dialogues", "dialogue_messages", "workflow_definitions", "workflow_revisions",
		"workflow_runs", "workflow_state_events", "workflow_state_snapshots", "workflow_compensations",
		"provider_configs", "agent_task_templates", "workflow_tool_publications", "agent_execution_runs",
		"run_traces", "step_traces", "llm_call_traces", "tool_call_traces",
		"tool_approvals", "tool_executions",
	}
	gotNames := make([]string, 0, len(resources))
	collections := make(map[string]struct{}, len(resources))
	for _, resource := range resources {
		gotNames = append(gotNames, resource.Name)
		if _, exists := collections[resource.Collection]; exists {
			t.Fatalf("owned manifest repeats collection %q", resource.Collection)
		}
		collections[resource.Collection] = struct{}{}
		if got := resource.Filter["user_id"]; got != uint64(42) {
			t.Fatalf("resource %s filter user_id = %#v", resource.Name, got)
		}
	}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("manifest names = %#v, want %#v", gotNames, wantNames)
	}
	for _, forbidden := range []string{
		CollectionMCPConnections, CollectionMCPToolSnapshots,
		CollectionAgentDataDeletionRequests, CollectionAgentRetentionSubjects,
		CollectionAgentDataDeletionOperations, CollectionAgentProjects,
	} {
		if _, exists := collections[forbidden]; exists {
			t.Fatalf("shared or governance collection %q entered owned manifest", forbidden)
		}
	}
	if first, second := MongoOwnedDeletionScopeDigest(), MongoOwnedDeletionScopeDigest(); first == "" || first != second {
		t.Fatalf("scope digest is not stable: %q %q", first, second)
	}
}

func TestDataDeletionInventoryUsesMongoOwnedManifestWithoutMCPOverlap(t *testing.T) {
	queries := dataDeletionInventoryQueries(77)
	owned := make(map[string]int)
	related := make(map[string]int)
	for _, query := range queries {
		switch query.class {
		case dataDeletionOwned:
			owned[query.name]++
		case dataDeletionRelated:
			related[query.name]++
		}
	}
	for _, resource := range mongoOwnedDeletionResources(77) {
		if owned[resource.Name] != 1 {
			t.Fatalf("owned resource %q occurs %d times", resource.Name, owned[resource.Name])
		}
	}
	for _, name := range []string{"mcp_connections_personal", "project_scoped_mcp_connections_created", "mcp_tool_snapshots"} {
		if related[name] != 1 {
			t.Fatalf("MCP relationship %q occurs %d times", name, related[name])
		}
	}
	if owned["mcp_connections_created"] != 0 || owned["mcp_tool_snapshots"] != 0 || owned["tool_result_references"] != 0 {
		t.Fatalf("overlapping owned inventory remains: %#v", owned)
	}
}

func TestMongoOwnedDeletionAdapterDeletesOnceAndReplaysReceipt(t *testing.T) {
	now := time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)
	store := &fakeMongoOwnedDeletionStore{
		counts: map[string]int64{"dialogues": 2, "tool_executions": 1},
	}
	adapter := &MongoOwnedDeletionAdapter{store: store, now: func() time.Time { return now }}
	request := mongoOwnedAdapterRequest(t, adapter, now, MongoOwnedDeletionScopeDigest())

	first, err := adapter.Delete(context.Background(), request)
	if err != nil {
		t.Fatalf("Delete() first error = %v", err)
	}
	if first.Replayed || store.mutations != 1 || first.Receipt.AffectedCount != 3 {
		t.Fatalf("first result = %#v, mutations = %d", first, store.mutations)
	}
	second, err := adapter.Delete(context.Background(), request)
	if err != nil {
		t.Fatalf("Delete() replay error = %v", err)
	}
	if !second.Replayed || store.mutations != 1 || second.Receipt.ReceiptDigest != first.Receipt.ReceiptDigest {
		t.Fatalf("replay result = %#v, mutations = %d", second, store.mutations)
	}

	postState, err := adapter.Observe(context.Background(), request.ObservationRequest())
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if err := agentdeletion.VerifyAdapterResult(adapter, request, second, postState, now); err != nil {
		t.Fatalf("VerifyAdapterResult() error = %v", err)
	}
}

func TestMongoOwnedDeletionAdapterRejectsScopeDriftBeforeStoreMutation(t *testing.T) {
	now := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	store := &fakeMongoOwnedDeletionStore{counts: map[string]int64{"dialogues": 1}}
	adapter := &MongoOwnedDeletionAdapter{store: store, now: func() time.Time { return now }}
	request := mongoOwnedAdapterRequest(t, adapter, now, deletionTestDigest("drifted-scope"))

	if _, err := adapter.Delete(context.Background(), request); err == nil {
		t.Fatal("Delete() allowed a scope digest that did not match the fixed manifest")
	}
	if store.deleteCalls != 0 || store.mutations != 0 {
		t.Fatalf("store was called before scope rejection: calls=%d mutations=%d", store.deleteCalls, store.mutations)
	}
}

func TestMongoOwnedGovernanceDecisionBindsHoldRevisionAndExpiry(t *testing.T) {
	now := time.Date(2026, 9, 25, 11, 0, 0, 0, time.UTC)
	subject := dataRetentionSubjectDocument{
		UserID: 91, Held: true, HoldUntil: now.Add(time.Hour), Revision: 7,
	}
	held, err := mongoOwnedGovernanceDecision(subject, 91, now, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("held decision error = %v", err)
	}
	if held.Status != agentdeletion.GovernanceHeld || held.Revision != 7 {
		t.Fatalf("held decision = %#v", held)
	}

	clear, err := mongoOwnedGovernanceDecision(subject, 91, now.Add(2*time.Hour), now.Add(2*time.Hour+time.Minute))
	if err != nil {
		t.Fatalf("clear decision error = %v", err)
	}
	if clear.Status != agentdeletion.GovernanceClear || clear.EvidenceDigest == held.EvidenceDigest {
		t.Fatalf("expired hold decision = %#v; held evidence = %q", clear, held.EvidenceDigest)
	}

	changed := subject
	changed.Revision++
	changedDecision, err := mongoOwnedGovernanceDecision(changed, 91, now.Add(2*time.Hour), now.Add(2*time.Hour+time.Minute))
	if err != nil {
		t.Fatalf("changed decision error = %v", err)
	}
	if changedDecision.EvidenceDigest == clear.EvidenceDigest || changedDecision.Digest == clear.Digest {
		t.Fatal("governance revision change did not change decision evidence")
	}
}

func TestMongoOwnedDeletionOperationRejectsCountAndReceiptTampering(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	adapter := &MongoOwnedDeletionAdapter{store: &fakeMongoOwnedDeletionStore{}, now: func() time.Time { return now }}
	request := mongoOwnedAdapterRequest(t, adapter, now, MongoOwnedDeletionScopeDigest())
	before, err := agentdeletion.NewPostState(request.Capability, request.SubjectDigest, request.ScopeDigest, 2, now)
	if err != nil {
		t.Fatal(err)
	}
	after, err := agentdeletion.NewPostState(request.Capability, request.SubjectDigest, request.ScopeDigest, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := agentdeletion.NewExecutionReceipt(request, before, after, now)
	if err != nil {
		t.Fatal(err)
	}
	beforeCounts := emptyMongoOwnedCounts()
	beforeCounts["dialogues"] = 2
	afterCounts := emptyMongoOwnedCounts()
	operation := newMongoOwnedDeletionOperation(request, beforeCounts, afterCounts, receipt, now)
	if err := validateMongoOwnedDeletionOperation(operation, request); err != nil {
		t.Fatalf("valid operation rejected: %v", err)
	}

	operation.BeforeCounts["dialogues"] = 3
	if err := validateMongoOwnedDeletionOperation(operation, request); err == nil {
		t.Fatal("tampered collection counts were accepted")
	}
}

func TestMongoOwnedDeletionAdapterRejectsForgedPersistentReplay(t *testing.T) {
	now := time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)
	store := &fakeMongoOwnedDeletionStore{counts: map[string]int64{"dialogues": 1}}
	adapter := &MongoOwnedDeletionAdapter{store: store, now: func() time.Time { return now }}
	request := mongoOwnedAdapterRequest(t, adapter, now, MongoOwnedDeletionScopeDigest())
	if _, err := adapter.Delete(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	store.operation.BeforeCounts["dialogues"]++
	if _, err := adapter.Delete(context.Background(), request); err == nil {
		t.Fatal("Delete() accepted a tampered persisted operation")
	}
}

type fakeMongoOwnedDeletionStore struct {
	counts      map[string]int64
	operation   *mongoOwnedDeletionOperation
	deleteCalls int
	mutations   int
	deleteErr   error
}

func (s *fakeMongoOwnedDeletionStore) Observe(_ context.Context, _ uint64, _ time.Time) (map[string]int64, error) {
	return cloneDeletionCounts(s.counts), nil
}

func (s *fakeMongoOwnedDeletionStore) IssueGovernanceDecision(
	_ context.Context,
	userID uint64,
	now time.Time,
	validUntil time.Time,
) (agentdeletion.GovernanceDecision, error) {
	return agentdeletion.NewGovernanceDecision(userID, agentdeletion.GovernanceClear, 1, deletionTestDigest("clear"), now, validUntil)
}

func (s *fakeMongoOwnedDeletionStore) Delete(
	_ context.Context,
	request agentdeletion.AdapterRequest,
	now time.Time,
) (mongoOwnedDeletionOperation, bool, error) {
	s.deleteCalls++
	if s.deleteErr != nil {
		return mongoOwnedDeletionOperation{}, false, s.deleteErr
	}
	if s.operation != nil {
		return *s.operation, true, nil
	}
	beforeCounts := cloneDeletionCounts(s.counts)
	beforeCounts = completeMongoOwnedCounts(beforeCounts)
	before, err := agentdeletion.NewPostState(
		request.Capability, request.SubjectDigest, request.ScopeDigest, sumDeletionCounts(beforeCounts), now,
	)
	if err != nil {
		return mongoOwnedDeletionOperation{}, false, err
	}
	afterCounts := make(map[string]int64, len(beforeCounts))
	for name := range beforeCounts {
		afterCounts[name] = 0
	}
	after, err := agentdeletion.NewPostState(request.Capability, request.SubjectDigest, request.ScopeDigest, 0, now)
	if err != nil {
		return mongoOwnedDeletionOperation{}, false, err
	}
	receipt, err := agentdeletion.NewExecutionReceipt(request, before, after, now)
	if err != nil {
		return mongoOwnedDeletionOperation{}, false, err
	}
	operation := newMongoOwnedDeletionOperation(request, beforeCounts, afterCounts, receipt, now)
	s.operation = &operation
	s.counts = afterCounts
	s.mutations++
	return operation, false, nil
}

func emptyMongoOwnedCounts() map[string]int64 {
	return completeMongoOwnedCounts(nil)
}

func completeMongoOwnedCounts(counts map[string]int64) map[string]int64 {
	complete := make(map[string]int64, len(mongoOwnedDeletionResources(1)))
	for _, resource := range mongoOwnedDeletionResources(1) {
		complete[resource.Name] = counts[resource.Name]
	}
	return complete
}

func mongoOwnedAdapterRequest(
	t *testing.T,
	adapter *MongoOwnedDeletionAdapter,
	now time.Time,
	scopeDigest string,
) agentdeletion.AdapterRequest {
	t.Helper()
	registry, err := agentdeletion.NewCapabilityRegistry(adapter)
	if err != nil {
		t.Fatal(err)
	}
	governance, err := agentdeletion.NewGovernanceDecision(
		1234, agentdeletion.GovernanceClear, 1, deletionTestDigest("governance"), now.Add(-time.Second), now.Add(time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	request, err := agentdeletion.NewAdapterRequest(agentdeletion.AdapterRequestInput{
		RequestID: "request-1", StageID: agentdeletion.StageDeleteMongo, UserID: 1234,
		PlanDigest: deletionTestDigest("plan"), CapabilityDigest: registry.Snapshot().Digest,
		Capability: adapter.DeletionCapability(), ScopeDigest: scopeDigest, Governance: governance,
		Now: now, Deadline: now.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func deletionTestDigest(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

var _ mongoOwnedDeletionPersistence = (*fakeMongoOwnedDeletionStore)(nil)
