package repository

import (
	"context"
	"reflect"
	"testing"
	"time"

	agentdeletion "twitter-clone/internal/module/agent/deletion"
)

func TestRelationshipDeletionManifestIsFixedAndOwnershipSafe(t *testing.T) {
	wantResources := []string{
		"project_memberships",
		"mcp_connections_personal",
		"mcp_tool_snapshots",
		"profile_role_bindings",
	}
	resources := relationshipDeletionResources()
	gotResources := make([]string, 0, len(resources))
	for _, resource := range resources {
		gotResources = append(gotResources, resource.Name)
	}
	if !reflect.DeepEqual(gotResources, wantResources) {
		t.Fatalf("relationship resources = %#v, want %#v", gotResources, wantResources)
	}

	wantBlockers := []string{
		"project_ownerships",
		"project_scoped_mcp_connections_created",
		"marketplace_publisher_ownerships",
	}
	blockers := relationshipDeletionBlockers(42)
	gotBlockers := make([]string, 0, len(blockers))
	for _, blocker := range blockers {
		gotBlockers = append(gotBlockers, blocker.Name)
	}
	if !reflect.DeepEqual(gotBlockers, wantBlockers) {
		t.Fatalf("relationship blockers = %#v, want %#v", gotBlockers, wantBlockers)
	}
	if first, second := RelationshipDeletionScopeDigest(), RelationshipDeletionScopeDigest(); first == "" || first != second {
		t.Fatalf("relationship scope digest is unstable: %q %q", first, second)
	}
}

func TestRelationshipDeletionAdapterDeletesOnceAndReplays(t *testing.T) {
	now := time.Date(2026, 9, 25, 16, 0, 0, 0, time.UTC)
	store := &fakeRelationshipDeletionStore{
		counts: map[string]int64{"project_memberships": 2, "mcp_connections_personal": 1},
	}
	adapter := &RelationshipDeletionAdapter{store: store, now: func() time.Time { return now }}
	request := relationshipAdapterRequest(t, adapter, now, RelationshipDeletionScopeDigest())

	first, err := adapter.Delete(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := adapter.Delete(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Replayed || !second.Replayed || store.mutations != 1 ||
		first.Receipt.AffectedCount != 3 ||
		second.Receipt.ReceiptDigest != first.Receipt.ReceiptDigest {
		t.Fatalf("unexpected replay results: first=%#v second=%#v mutations=%d", first, second, store.mutations)
	}

	postState, err := adapter.Observe(context.Background(), request.ObservationRequest())
	if err != nil {
		t.Fatal(err)
	}
	if err := agentdeletion.VerifyAdapterResult(adapter, request, second, postState, now); err != nil {
		t.Fatal(err)
	}
}

func TestRelationshipDeletionAdapterRejectsScopeDriftBeforeMutation(t *testing.T) {
	now := time.Date(2026, 9, 25, 17, 0, 0, 0, time.UTC)
	store := &fakeRelationshipDeletionStore{counts: map[string]int64{"project_memberships": 1}}
	adapter := &RelationshipDeletionAdapter{store: store, now: func() time.Time { return now }}
	request := relationshipAdapterRequest(t, adapter, now, deletionTestDigest("drifted-relationship-scope"))

	if _, err := adapter.Delete(context.Background(), request); err == nil {
		t.Fatal("Delete() accepted a drifted relationship scope")
	}
	if store.deleteCalls != 0 || store.mutations != 0 {
		t.Fatalf("store called before scope rejection: calls=%d mutations=%d", store.deleteCalls, store.mutations)
	}
}

func TestRelationshipDeletionOperationRejectsTampering(t *testing.T) {
	now := time.Date(2026, 9, 25, 18, 0, 0, 0, time.UTC)
	adapter := &RelationshipDeletionAdapter{
		store: &fakeRelationshipDeletionStore{},
		now:   func() time.Time { return now },
	}
	request := relationshipAdapterRequest(t, adapter, now, RelationshipDeletionScopeDigest())
	beforeCounts := completeRelationshipDeletionCounts(map[string]int64{"project_memberships": 1})
	before, err := agentdeletion.NewPostState(
		request.Capability,
		request.SubjectDigest,
		request.ScopeDigest,
		1,
		now,
	)
	if err != nil {
		t.Fatal(err)
	}
	after, err := agentdeletion.NewPostState(
		request.Capability,
		request.SubjectDigest,
		request.ScopeDigest,
		0,
		now,
	)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := agentdeletion.NewExecutionReceipt(request, before, after, now)
	if err != nil {
		t.Fatal(err)
	}
	operation := newRelationshipDeletionOperation(
		request,
		beforeCounts,
		completeRelationshipDeletionCounts(nil),
		receipt,
		now,
	)
	if err := validateRelationshipDeletionOperation(operation, request); err != nil {
		t.Fatalf("valid operation rejected: %v", err)
	}
	operation.BeforeCounts["project_memberships"]++
	if err := validateRelationshipDeletionOperation(operation, request); err == nil {
		t.Fatal("tampered relationship operation accepted")
	}
}

type fakeRelationshipDeletionStore struct {
	counts      map[string]int64
	operation   *relationshipDeletionOperation
	deleteCalls int
	mutations   int
	err         error
}

func (s *fakeRelationshipDeletionStore) Observe(
	context.Context,
	uint64,
	time.Time,
) (map[string]int64, error) {
	return completeRelationshipDeletionCounts(s.counts), s.err
}

func (s *fakeRelationshipDeletionStore) IssueGovernanceDecision(
	_ context.Context,
	userID uint64,
	now time.Time,
	validUntil time.Time,
) (agentdeletion.GovernanceDecision, error) {
	return agentdeletion.NewGovernanceDecision(
		userID,
		agentdeletion.GovernanceClear,
		1,
		deletionTestDigest("relationship-clear"),
		now,
		validUntil,
	)
}

func (s *fakeRelationshipDeletionStore) Delete(
	_ context.Context,
	request agentdeletion.AdapterRequest,
	now time.Time,
) (relationshipDeletionOperation, bool, error) {
	s.deleteCalls++
	if s.err != nil {
		return relationshipDeletionOperation{}, false, s.err
	}
	if s.operation != nil {
		return *s.operation, true, nil
	}
	beforeCounts := completeRelationshipDeletionCounts(s.counts)
	before, err := agentdeletion.NewPostState(
		request.Capability,
		request.SubjectDigest,
		request.ScopeDigest,
		sumDeletionCounts(beforeCounts),
		now,
	)
	if err != nil {
		return relationshipDeletionOperation{}, false, err
	}
	afterCounts := completeRelationshipDeletionCounts(nil)
	after, err := agentdeletion.NewPostState(
		request.Capability,
		request.SubjectDigest,
		request.ScopeDigest,
		0,
		now,
	)
	if err != nil {
		return relationshipDeletionOperation{}, false, err
	}
	receipt, err := agentdeletion.NewExecutionReceipt(request, before, after, now)
	if err != nil {
		return relationshipDeletionOperation{}, false, err
	}
	operation := newRelationshipDeletionOperation(request, beforeCounts, afterCounts, receipt, now)
	s.operation = &operation
	s.counts = afterCounts
	s.mutations++
	return operation, false, nil
}

func completeRelationshipDeletionCounts(counts map[string]int64) map[string]int64 {
	complete := make(map[string]int64, len(relationshipDeletionResources()))
	for _, resource := range relationshipDeletionResources() {
		complete[resource.Name] = counts[resource.Name]
	}
	return complete
}

func relationshipAdapterRequest(
	t *testing.T,
	adapter *RelationshipDeletionAdapter,
	now time.Time,
	scopeDigest string,
) agentdeletion.AdapterRequest {
	t.Helper()
	registry, err := agentdeletion.NewCapabilityRegistry(adapter)
	if err != nil {
		t.Fatal(err)
	}
	governance, err := agentdeletion.NewGovernanceDecision(
		1234,
		agentdeletion.GovernanceClear,
		1,
		deletionTestDigest("governance"),
		now.Add(-time.Second),
		now.Add(time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	request, err := agentdeletion.NewAdapterRequest(agentdeletion.AdapterRequestInput{
		RequestID:        "request-relationships",
		StageID:          agentdeletion.StageDetachRelationships,
		UserID:           1234,
		PlanDigest:       deletionTestDigest("plan"),
		CapabilityDigest: registry.Snapshot().Digest,
		Capability:       adapter.DeletionCapability(),
		ScopeDigest:      scopeDigest,
		Governance:       governance,
		Now:              now,
		Deadline:         now.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

var _ relationshipDeletionPersistence = (*fakeRelationshipDeletionStore)(nil)
