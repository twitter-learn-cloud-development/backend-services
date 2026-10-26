package es

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/stretchr/testify/require"
)

func TestESDeleteTweetIsIdempotentAndPreservesServerFailure(t *testing.T) {
	for _, status := range []int{200, 404, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodDelete, r.Method)
				require.Equal(t, "/tweets/_doc/9007199254740997", r.URL.Path)
				w.Header().Set("X-Elastic-Product", "Elasticsearch")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				if status == 200 {
					_, _ = w.Write([]byte(`{"_index":"tweets","_id":"9007199254740997","result":"deleted"}`))
				} else if status == 404 {
					_, _ = w.Write([]byte(`{"_index":"tweets","_id":"9007199254740997","result":"not_found"}`))
				} else {
					_, _ = w.Write([]byte(`{"error":{"type":"fixture_failure","reason":"fixture"},"status":` + map[int]string{404: "404", 503: "503"}[status] + `}`))
				}
			}))
			defer server.Close()
			typed, err := elasticsearch.NewTypedClient(elasticsearch.Config{Addresses: []string{server.URL}, DisableRetry: true})
			require.NoError(t, err)
			err = (&Client{TypedClient: typed}).DeleteTweet(context.Background(), "9007199254740997")
			if status == 503 {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
