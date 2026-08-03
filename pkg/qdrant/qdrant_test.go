package qdrant

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

func TestConvertSnowflakeToQdrantID(t *testing.T) {
	tests := []struct {
		name        string
		snowflakeID uint64
		expected    string
	}{
		{
			name:        "Min Snowflake ID",
			snowflakeID: 0,
			expected:    "00000000-0000-0000-0000-000000000000",
		},
		{
			name:        "Normal Snowflake ID",
			snowflakeID: 2024791560905822208, // 0x1c19813264001000 -> 0x1c19 0x813264001000
			expected:    "00000000-0000-0000-1c19-813264001000",
		},
		{
			name:        "Max Snowflake ID",
			snowflakeID: 18446744073709551615, // 0xffffffffffffffff
			expected:    "00000000-0000-0000-ffff-ffffffffffff",
		},
	}

	// UUID 格式正则：8-4-4-4-12 hex characters
	uuidRegex := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertSnowflakeToQdrantID(tt.snowflakeID)
			if got != tt.expected {
				t.Errorf("ConvertSnowflakeToQdrantID() = %v, expected %v", got, tt.expected)
			}
			if !uuidRegex.MatchString(got) {
				t.Errorf("ConvertSnowflakeToQdrantID() output %v is not a valid UUID format", got)
			}
		})
	}
}

func TestSearchWithFilterSendsTenantConstraintToQdrant(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/collections/agent_episodic_memory/points/search" {
			t.Fatalf("unexpected search path: %s", r.URL.Path)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		filter, ok := body["filter"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected filter in search body: %#v", body)
		}
		must, ok := filter["must"].([]interface{})
		if !ok || len(must) != 1 {
			t.Fatalf("expected one filter clause: %#v", filter)
		}
		clause := must[0].(map[string]interface{})
		if clause["key"] != "user_id" {
			t.Fatalf("unexpected filter key: %#v", clause["key"])
		}
		match := clause["match"].(map[string]interface{})
		if match["value"] != "42" {
			t.Fatalf("unexpected user filter value: %#v", match["value"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":[{"id":"memory-1","score":0.91,"payload":{"summary":"uses Go","user_id":"42"}}]}`))
	}))
	defer server.Close()

	results, err := NewClient(server.URL).SearchWithFilter(
		t.Context(),
		"agent_episodic_memory",
		[]float32{0.1, 0.2},
		5,
		map[string]interface{}{
			"must": []interface{}{
				map[string]interface{}{
					"key":   "user_id",
					"match": map[string]interface{}{"value": "42"},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("search with filter: %v", err)
	}
	if len(results) != 1 || results[0].ID != "memory-1" {
		t.Fatalf("unexpected results: %#v", results)
	}
}

func TestSharedCollectionSearchIsolatesUsers(t *testing.T) {
	points := []map[string]interface{}{
		{"id": "memory-user-42", "score": 0.95, "payload": map[string]interface{}{"user_id": "42", "summary": "Go preference"}},
		{"id": "memory-user-7", "score": 0.99, "payload": map[string]interface{}{"user_id": "7", "summary": "Rust preference"}},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		filter, _ := body["filter"].(map[string]interface{})
		must, _ := filter["must"].([]interface{})
		if len(must) != 1 {
			t.Fatalf("missing tenant filter: %#v", body)
		}
		clause, _ := must[0].(map[string]interface{})
		match, _ := clause["match"].(map[string]interface{})
		userID, _ := match["value"].(string)

		selected := make([]map[string]interface{}, 0, 1)
		for _, point := range points {
			payload := point["payload"].(map[string]interface{})
			if payload["user_id"] == userID {
				selected = append(selected, point)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{"result": selected}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL)
	for _, test := range []struct {
		userID string
		wantID string
	}{
		{userID: "42", wantID: "memory-user-42"},
		{userID: "7", wantID: "memory-user-7"},
	} {
		results, err := client.SearchWithFilter(t.Context(), "agent_episodic_memory", []float32{0.1}, 10, map[string]interface{}{
			"must": []interface{}{map[string]interface{}{
				"key": "user_id", "match": map[string]interface{}{"value": test.userID},
			}},
		})
		if err != nil {
			t.Fatalf("search user %s: %v", test.userID, err)
		}
		if len(results) != 1 || results[0].ID != test.wantID {
			t.Fatalf("user %s received cross-tenant results: %#v", test.userID, results)
		}
		if got := results[0].Payload["user_id"]; got != test.userID {
			t.Fatalf("user %s received payload for %#v", test.userID, got)
		}
	}
}

func TestScrollReturnsVectorsPayloadAndCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/collections/episodic_user_42/points/scroll" {
			t.Fatalf("unexpected scroll path: %s", r.URL.Path)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode scroll request: %v", err)
		}
		if body["with_vector"] != true || body["with_payload"] != true {
			t.Fatalf("scroll must request vector and payload: %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"points":[{"id":"point-1","vector":[0.1,0.2],"payload":{"summary":"uses Go"}}],"next_page_offset":"point-1"}}`))
	}))
	defer server.Close()

	points, next, err := NewClient(server.URL).Scroll(t.Context(), "episodic_user_42", 100, nil)
	if err != nil {
		t.Fatalf("scroll: %v", err)
	}
	if len(points) != 1 || points[0].ID != "point-1" || len(points[0].Vector) != 2 {
		t.Fatalf("unexpected points: %#v", points)
	}
	if next != "point-1" {
		t.Fatalf("unexpected next page cursor: %#v", next)
	}
}

func TestScrollWithFilterSendsPayloadConstraintToQdrant(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode scroll request: %v", err)
		}
		filter, ok := body["filter"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected scroll filter: %#v", body)
		}
		must, _ := filter["must"].([]interface{})
		clause, _ := must[0].(map[string]interface{})
		match, _ := clause["match"].(map[string]interface{})
		if match["value"] != "42" {
			t.Fatalf("unexpected user filter: %#v", filter)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"points":[],"next_page_offset":null}}`))
	}))
	defer server.Close()

	_, _, err := NewClient(server.URL).ScrollWithFilter(t.Context(), "agent_episodic_memory", 10, nil, map[string]interface{}{
		"must": []interface{}{map[string]interface{}{
			"key": "user_id", "match": map[string]interface{}{"value": "42"},
		}},
	})
	if err != nil {
		t.Fatalf("scroll with filter: %v", err)
	}
}
