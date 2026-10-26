package qdrant

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProjectionReadChecksCanonicalIDAndExactStringAliasesWithoutVectors(t *testing.T) {
	for _, kind := range []string{"canonical", "alias_only", "empty", "bad_count", "wrong_id", "failure"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request map[string]interface{}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/collections/tweets/points" {
					require.Equal(t, false, request["with_vector"])
					require.Equal(t, []interface{}{ConvertSnowflakeToQdrantID(9007199254740997)}, request["ids"])
					if kind == "failure" {
						w.WriteHeader(503)
						_, _ = w.Write([]byte(`{"private":"provider"}`))
						return
					}
					if kind == "empty" || kind == "alias_only" {
						_, _ = w.Write([]byte(`{"result":[]}`))
						return
					}
					id := ConvertSnowflakeToQdrantID(9007199254740997)
					if kind == "wrong_id" {
						id = "wrong"
					}
					_, _ = fmt.Fprintf(w, `{"result":[{"id":%q,"payload":{"tweet_id":"9007199254740997","projection_digest":"digest","projection_version":"v1","embedding_profile":"profile"}}]}`, id)
				} else {
					require.Equal(t, "/collections/tweets/points/count", r.URL.Path)
					require.Equal(t, true, request["exact"])
					filter := request["filter"].(map[string]interface{})
					clause := filter["must"].([]interface{})[0].(map[string]interface{})
					require.Equal(t, "9007199254740997", clause["match"].(map[string]interface{})["value"])
					if kind == "bad_count" {
						_, _ = w.Write([]byte(`{"result":{}}`))
						return
					}
					count := 1
					if kind == "empty" {
						count = 0
					}
					if kind == "alias_only" {
						count = 2
					}
					_, _ = fmt.Fprintf(w, `{"result":{"count":%d}}`, count)
				}
			}))
			defer server.Close()
			snapshot, err := NewClient(server.URL).ReadTweetProjection(context.Background(), 9007199254740997)
			if kind == "bad_count" || kind == "wrong_id" || kind == "failure" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				if kind == "empty" {
					require.False(t, snapshot.Present)
				} else {
					require.True(t, snapshot.Present)
				}
			}
			if kind == "alias_only" {
				require.Equal(t, int64(2), snapshot.Copies)
				require.Empty(t, snapshot.Digest)
			}
		})
	}
}

func TestTweetProjectionWriteRequiresCompletedReceiptAndNeverCreatesCollection(t *testing.T) {
	for _, kind := range []string{"completed", "acknowledged", "missing", "malformed"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPut, r.Method)
				require.Equal(t, "/collections/tweets/points", r.URL.Path)
				require.Equal(t, "true", r.URL.Query().Get("wait"))
				var request map[string]interface{}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
				point := request["points"].([]interface{})[0].(map[string]interface{})
				require.Equal(t, "9007199254740997", point["payload"].(map[string]interface{})["tweet_id"])
				if kind == "missing" {
					w.WriteHeader(404)
					_, _ = w.Write([]byte(`{"status":"private provider"}`))
					return
				}
				if kind == "malformed" {
					_, _ = w.Write([]byte(`{}`))
					return
				}
				_, _ = fmt.Fprintf(w, `{"result":{"status":%q}}`, kind)
			}))
			defer server.Close()
			err := NewClient(server.URL).UpsertTweetProjection(context.Background(), 9007199254740997, []float32{1, 2}, map[string]interface{}{"projection_digest": "digest"})
			if kind == "completed" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "private provider")
			}
		})
	}
}
