package repository

import (
	"context"
	"reflect"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	agentdeletion "twitter-clone/internal/module/agent/deletion"
)

func TestValidateDataDeletionRequestInput(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name, reason, actor string
		userID              uint64
		wantErr             bool
	}{
		{name: "valid", userID: 42, reason: "user_request", actor: "privacy@example.com"},
		{name: "missing subject", reason: "user_request", actor: "privacy", wantErr: true},
		{name: "invalid reason", userID: 42, reason: "User Request", actor: "privacy", wantErr: true},
		{name: "missing actor", userID: 42, reason: "user_request", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateDataDeletionRequestInput(test.userID, test.reason, test.actor, now)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateDataDeletionRequestInput() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestDataDeletionActorDigestNormalizesIdentity(t *testing.T) {
	if DataDeletionActorDigest(" Privacy@Example.com ") != DataDeletionActorDigest("privacy@example.com") {
		t.Fatal("actor digest should normalize case and surrounding whitespace")
	}
}

func TestDataDeletionInventoryQueriesClassifyOwnedRelatedAndRetainedResources(t *testing.T) {
	queries := dataDeletionInventoryQueries(42)
	want := map[string]dataDeletionInventoryClass{
		"workflow_definitions":             dataDeletionOwned,
		"workflow_state_events":            dataDeletionOwned,
		"provider_configs":                 dataDeletionOwned,
		"mcp_tool_snapshots":               dataDeletionRelated,
		"mcp_connections_personal":         dataDeletionRelated,
		"agent_task_templates":             dataDeletionOwned,
		"workflow_tool_publications":       dataDeletionOwned,
		"project_memberships":              dataDeletionRelated,
		"marketplace_publisher_ownerships": dataDeletionRelated,
		"product_events":                   dataDeletionRetained,
		"published_content_attributions":   dataDeletionRetained,
		"profile_audit_events":             dataDeletionRetained,
		"marketplace_audit_events":         dataDeletionRetained,
	}
	seen := make(map[string]dataDeletionInventoryClass, len(queries))
	for _, query := range queries {
		if query.name == "" || query.collection == "" || len(query.filter) == 0 {
			t.Fatalf("invalid inventory query: %+v", query)
		}
		if _, exists := seen[query.name]; exists {
			t.Fatalf("duplicate inventory query name %q", query.name)
		}
		seen[query.name] = query.class
	}
	for name, class := range want {
		if seen[name] != class {
			t.Fatalf("query %q class = %q, want %q", name, seen[name], class)
		}
	}
}

func TestDataDeletionInventoryQueriesUseSubjectFilters(t *testing.T) {
	queries := dataDeletionInventoryQueries(42)
	filters := make(map[string]bson.M, len(queries))
	for _, query := range queries {
		filters[query.name] = query.filter
	}
	if !reflect.DeepEqual(filters["dialogues"], bson.M{"user_id": uint64(42)}) {
		t.Fatalf("dialogue filter is not user scoped: %#v", filters["dialogues"])
	}
	if !reflect.DeepEqual(filters["project_memberships"], bson.M{"members.user_id": uint64(42)}) {
		t.Fatalf("project membership filter is not user scoped: %#v", filters["project_memberships"])
	}
	if !reflect.DeepEqual(filters["mcp_connections_personal"], personalMCPConnectionFilter(42)) {
		t.Fatalf("personal MCP filter is not scope-safe: %#v", filters["mcp_connections_personal"])
	}
	if !reflect.DeepEqual(filters["published_content_attributions"], bson.M{"author_user_id": uint64(42)}) {
		t.Fatalf("attribution filter is not author scoped: %#v", filters["published_content_attributions"])
	}
}

func TestCompareAndSwapDataDeletionCheckpointRejectsInvalidTransitionBeforeMongo(t *testing.T) {
	repository := &MongoAgentRepository{}
	now := time.Date(2026, 9, 25, 15, 0, 0, 0, time.UTC)
	protocol, err := agentdeletion.BuildProtocol(agentdeletion.Inventory{}, agentdeletion.EmptyCapabilityRegistry().Snapshot(), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.CompareAndSwapDataDeletionCheckpoint(
		context.Background(), primitive.NewObjectID(), 1, 1, 0, "", *protocol, agentdeletion.AuditEvent{}, now,
	); err == nil {
		t.Fatal("checkpoint without a revision transition was accepted")
	}
}

func TestDataDeletionNoActiveStageFilterGuardsRunningLease(t *testing.T) {
	now := time.Date(2026, 9, 25, 15, 0, 0, 0, time.UTC)
	filter := dataDeletionNoActiveStageFilter(now)
	nor, ok := filter["$nor"].(bson.A)
	if !ok || len(nor) != 1 {
		t.Fatalf("unexpected no-active-stage filter: %#v", filter)
	}
	stageFilter, ok := nor[0].(bson.M)
	if !ok {
		t.Fatalf("unexpected stage filter: %#v", nor[0])
	}
	arrayFilter, ok := stageFilter["execution_protocol.checkpoint.stages"].(bson.M)
	if !ok {
		t.Fatalf("missing checkpoint stage guard: %#v", stageFilter)
	}
	elemMatch, ok := arrayFilter["$elemMatch"].(bson.M)
	if !ok || elemMatch["status"] != agentdeletion.StageRunning {
		t.Fatalf("missing running stage guard: %#v", arrayFilter)
	}
	lease, ok := elemMatch["lease_until"].(bson.M)
	if !ok || !lease["$gt"].(time.Time).Equal(now) {
		t.Fatalf("missing active lease guard: %#v", elemMatch)
	}
}

func TestDataDeletionNoAuditEventsFilterAllowsOnlyUnstartedExecution(t *testing.T) {
	filter := dataDeletionNoAuditEventsFilter()
	alternatives, ok := filter["$or"].(bson.A)
	if !ok || len(alternatives) != 2 {
		t.Fatalf("unexpected no-audit filter: %#v", filter)
	}
	zero, ok := alternatives[0].(bson.M)
	if !ok || zero["execution_audit_count"] != int64(0) {
		t.Fatalf("missing zero audit-count guard: %#v", alternatives[0])
	}
	missing, ok := alternatives[1].(bson.M)
	if !ok {
		t.Fatalf("missing legacy audit-count guard: %#v", alternatives[1])
	}
	exists, ok := missing["execution_audit_count"].(bson.M)
	if !ok || exists["$exists"] != false {
		t.Fatalf("legacy audit-count guard is invalid: %#v", missing)
	}
}
