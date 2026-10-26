package es

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/stretchr/testify/require"
)

func TestProjectionReadUsesOnlyExplicitIDMetadataAndRejectsBadReceipt(t *testing.T) {
	for _, kind := range []string{"present", "missing", "proxy_404", "malformed", "wrong_id"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodGet, r.Method)
				require.Equal(t, "/tweets/_doc/9007199254740997", r.URL.Path)
				require.Contains(t, r.URL.Query().Get("_source_includes"), "projection_digest")
				require.NotContains(t, r.URL.RawQuery, "content")
				w.Header().Set("X-Elastic-Product", "Elasticsearch")
				w.Header().Set("Content-Type", "application/json")
				switch kind {
				case "present":
					_, _ = w.Write([]byte(`{"_id":"9007199254740997","found":true,"_source":{"projection_digest":"digest","projection_version":"v1"}}`))
				case "missing":
					w.WriteHeader(404)
					_, _ = w.Write([]byte(`{"_id":"9007199254740997","found":false}`))
				case "proxy_404":
					w.WriteHeader(404)
					_, _ = w.Write([]byte(`{"error":{"type":"private_proxy_error"}}`))
				case "wrong_id":
					_, _ = w.Write([]byte(`{"_id":"17","found":true,"_source":{}}`))
				default:
					_, _ = w.Write([]byte(`{}`))
				}
			}))
			defer server.Close()
			typed, err := elasticsearch.NewTypedClient(elasticsearch.Config{Addresses: []string{server.URL}, DisableRetry: true})
			require.NoError(t, err)
			snapshot, err := (&Client{TypedClient: typed}).ReadTweetProjection(context.Background(), 9007199254740997)
			if kind == "present" {
				require.NoError(t, err)
				require.True(t, snapshot.Present)
				require.Equal(t, int64(1), snapshot.Copies)
			} else if kind == "missing" {
				require.NoError(t, err)
				require.False(t, snapshot.Present)
			} else {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "private_proxy_error")
			}
		})
	}
}
