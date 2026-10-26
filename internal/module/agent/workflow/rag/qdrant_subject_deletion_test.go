package rag

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	agentdeletion "twitter-clone/internal/module/agent/deletion"
	"twitter-clone/pkg/qdrant"
)

type qdrantDeletionJournalStub struct{}

func (qdrantDeletionJournalStub) Claim(context.Context, agentdeletion.AdapterRequest, agentdeletion.PostState, time.Time) (agentdeletion.ExternalDeletionClaim, error) {
	return agentdeletion.ExternalDeletionClaim{}, nil
}

func (qdrantDeletionJournalStub) Complete(context.Context, agentdeletion.AdapterRequest, string, agentdeletion.ExecutionReceipt, time.Time) error {
	return nil
}

func TestQdrantSubjectDeletionResourceUsesExactTenantFilter(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		var body map[string]interface{}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		filter, _ := body["filter"].(map[string]interface{})
		must, _ := filter["must"].([]interface{})
		clause, _ := must[0].(map[string]interface{})
		match, _ := clause["match"].(map[string]interface{})
		if clause["key"] != "user_id" || match["value"] != "42" {
			t.Fatalf("unexpected Qdrant subject filter: %#v", filter)
		}
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/collections/agent_episodic_memory/points/count" {
			_, _ = writer.Write([]byte(`{"result":{"count":2}}`))
			return
		}
		if request.URL.Path != "/collections/agent_episodic_memory/points/delete" || request.URL.Query().Get("wait") != "true" {
			t.Fatalf("unexpected Qdrant deletion request: %s?%s", request.URL.Path, request.URL.RawQuery)
		}
		_, _ = writer.Write([]byte(`{"result":{"status":"completed"}}`))
	}))
	defer server.Close()

	resource := &qdrantSubjectDeletionResource{
		client: qdrant.NewClient(server.URL), collection: DefaultEpisodicCollectionName,
	}
	count, err := resource.CountSubject(t.Context(), 42)
	if err != nil || count != 2 {
		t.Fatalf("CountSubject() = %d, %v", count, err)
	}
	if err := resource.DeleteSubject(t.Context(), 42); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}

func TestQdrantSubjectDeletionAdapterRequiresLegacyCollectionsRetired(t *testing.T) {
	client := qdrant.NewClient("http://unused")
	if _, err := NewQdrantSubjectDeletionAdapter(client, QdrantSubjectDeletionConfig{}, qdrantDeletionJournalStub{}); err == nil {
		t.Fatal("Qdrant deletion capability accepted an unresolved legacy collection boundary")
	}
	adapter, err := NewQdrantSubjectDeletionAdapter(client, QdrantSubjectDeletionConfig{
		CollectionName: DefaultEpisodicCollectionName, LegacyCollectionsRetired: true,
	}, qdrantDeletionJournalStub{})
	if err != nil {
		t.Fatal(err)
	}
	if adapter.DeletionCapability().ID != agentdeletion.CapabilityQdrantSubjectDelete {
		t.Fatalf("unexpected Qdrant deletion capability: %+v", adapter.DeletionCapability())
	}
}
