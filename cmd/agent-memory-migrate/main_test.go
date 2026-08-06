package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"twitter-clone/pkg/qdrant"
)

func TestParseUserIDsDeduplicatesAndRejectsZero(t *testing.T) {
	users, err := parseUserIDs("42, 7,42")
	if err != nil {
		t.Fatalf("parse user ids: %v", err)
	}
	if len(users) != 2 || users[0] != 42 || users[1] != 7 {
		t.Fatalf("unexpected users: %#v", users)
	}
	if _, err := parseUserIDs("0"); err == nil {
		t.Fatal("expected zero user id to be rejected")
	}
}

func TestParseUserIDsAllowsEmptyInputForCallerValidation(t *testing.T) {
	users, err := parseUserIDs(" , ")
	if err != nil {
		t.Fatalf("parse empty input: %v", err)
	}
	if len(users) != 0 {
		t.Fatalf("expected no users, got %#v", users)
	}
}

func TestVerifyUserMigrationMatchesSourceIDsAndTenantPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/points/scroll") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "episodic_user_42"):
			_, _ = w.Write([]byte(`{"result":{"points":[
{"id":"memory-1","vector":[0.1,0.2],"payload":{"summary":"Go"}},
{"id":"memory-2","vector":[0.2,0.3],"payload":{"summary":"Redis"}},
{"id":"","vector":[],"payload":{"summary":"invalid"}}
],"next_page_offset":null}}`))
		case strings.Contains(r.URL.Path, sharedCollection):
			filter, ok := body["filter"].(map[string]interface{})
			if !ok {
				t.Fatalf("shared verification must send a tenant filter: %#v", body)
			}
			must, _ := filter["must"].([]interface{})
			clause, _ := must[0].(map[string]interface{})
			match, _ := clause["match"].(map[string]interface{})
			if match["value"] != "42" {
				t.Fatalf("unexpected tenant filter: %#v", filter)
			}
			_, _ = w.Write([]byte(`{"result":{"points":[
{"id":"memory-1","vector":[0.1,0.2],"payload":{"user_id":"42","collection_schema":"shared_user_payload_v1"}},
{"id":"memory-2","vector":[0.2,0.3],"payload":{"user_id":"42","collection_schema":"shared_user_payload_v1"}}
],"next_page_offset":null}}`))
		default:
			t.Fatalf("unexpected collection path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	stats, err := verifyUserMigration(t.Context(), qdrant.NewClient(server.URL), 42, 100)
	if err != nil {
		t.Fatalf("verify migration: %v", err)
	}
	if !stats.Verified || stats.SourceValidPoints != 2 || stats.SharedUserPoints != 2 {
		t.Fatalf("unexpected verification stats: %#v", stats)
	}
}

func TestVerifyUserMigrationRejectsForeignTenantPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "episodic_user_42") {
			_, _ = w.Write([]byte(`{"result":{"points":[{"id":"memory-1","vector":[0.1],"payload":{}}],"next_page_offset":null}}`))
			return
		}
		if strings.Contains(r.URL.Path, sharedCollection) {
			if _, ok := body["filter"]; !ok {
				t.Fatalf("expected tenant filter")
			}
			_, _ = w.Write([]byte(`{"result":{"points":[{"id":"memory-1","vector":[0.1],"payload":{"user_id":"7","collection_schema":"shared_user_payload_v1"}}],"next_page_offset":null}}`))
			return
		}
		t.Fatalf("unexpected path: %s", r.URL.Path)
	}))
	defer server.Close()

	stats, err := verifyUserMigration(t.Context(), qdrant.NewClient(server.URL), 42, 100)
	if err != nil {
		t.Fatalf("verify migration: %v", err)
	}
	if stats.Verified || stats.InvalidTenantPoints != 1 {
		t.Fatalf("expected foreign tenant to fail verification: %#v", stats)
	}
}

func TestWriteVerificationReportCreatesParentDirectory(t *testing.T) {
	path := t.TempDir() + "/reports/migration.json"
	report := verificationReport{
		SharedCollection: sharedCollection,
		Users:            []verificationStats{{UserID: 42, Verified: true}},
		Verified:         true,
	}
	if err := writeVerificationReport(path, report); err != nil {
		t.Fatalf("write verification report: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read verification report: %v", err)
	}
	var decoded verificationReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode verification report: %v", err)
	}
	if !decoded.Verified || len(decoded.Users) != 1 || decoded.Users[0].UserID != 42 {
		t.Fatalf("unexpected verification report: %#v", decoded)
	}
}
