package qdrant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeleteTweetWaitsForCanonicalAndLegacyAliasRemoval(t *testing.T) {
	var bodies []map[string]interface{}
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/collections/tweets/points/delete", r.URL.Path)
		require.Equal(t, "true", r.URL.Query().Get("wait"))
		var body map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"status":"completed"},"status":"ok"}`))
	}))
	defer server.Close()
	require.NoError(t, NewClient(server.URL).DeleteTweet(context.Background(), 9007199254740997))
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, bodies, 2)
	require.Equal(t, []interface{}{ConvertSnowflakeToQdrantID(9007199254740997)}, bodies[0]["points"])
	filter := bodies[1]["filter"].(map[string]interface{})
	must := filter["must"].([]interface{})
	require.Len(t, must, 1)
	condition := must[0].(map[string]interface{})
	require.Equal(t, "tweet_id", condition["key"])
	require.Equal(t, "9007199254740997", condition["match"].(map[string]interface{})["value"])
}

func TestDeleteTweetRejectsBadOrIncompleteReceiptsAndIsRepeatableWhenAbsent(t *testing.T) {
	for _, kind := range []string{"missing", "unknown_404", "failed", "incomplete", "malformed", "invalid_id", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			var mu sync.Mutex
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				mu.Lock()
				calls++
				mu.Unlock()
				switch kind {
				case "missing":
					w.WriteHeader(404)
					_, _ = w.Write([]byte(`{"status":{"error":"Not found: Collection tweets doesn't exist!"}}`))
				case "unknown_404":
					w.WriteHeader(404)
					_, _ = w.Write([]byte(`{"status":{"error":"proxy rejection"}}`))
				case "failed":
					w.WriteHeader(503)
					_, _ = w.Write([]byte("private provider body"))
				case "incomplete":
					_, _ = w.Write([]byte(`{"result":{"status":"acknowledged"}}`))
				default:
					_, _ = w.Write([]byte("private provider body"))
				}
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			id := uint64(17)
			if kind == "invalid_id" {
				id = 0
			}
			if kind == "cancelled" {
				cancel()
			}
			err := NewClient(server.URL).DeleteTweet(ctx, id)
			mu.Lock()
			defer mu.Unlock()
			if kind == "missing" {
				require.NoError(t, err)
				require.Equal(t, 1, calls)
			} else {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "private provider body")
			}
			if kind == "invalid_id" || kind == "cancelled" {
				require.Zero(t, calls)
			}
		})
	}
}
