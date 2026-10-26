package handler

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"twitter-clone/internal/gateway/middleware"
)

func TestSocketReadyAfterSubscribeAndReaderStopsAtExpiry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	defer client.Close()
	policy, err := middleware.NewOriginPolicy("http://fixture.invalid")
	require.NoError(t, err)
	h := &WebSocketHandler{redisClient: client, originPolicy: policy}
	finished := make(chan struct{})
	r := gin.New()
	r.GET("/ws", func(c *gin.Context) {
		defer close(finished)
		h.serveSocket(c, 42, time.Now().Add(200*time.Millisecond))
	})
	server := httptest.NewServer(r)
	defer server.Close()
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", nil)
	require.NoError(t, err)
	defer ws.Close()
	_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, body, err := ws.ReadMessage()
	require.NoError(t, err)
	require.Contains(t, string(body), `"type":"ready"`)
	require.NoError(t, client.Publish(context.Background(), "notifications:user:42", `{"type":"message","data":{"id":"1"}}`).Err())
	_, body, err = ws.ReadMessage()
	require.NoError(t, err)
	require.Contains(t, string(body), `"id":"1"`)
	_, _, err = ws.ReadMessage()
	require.Error(t, err)
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("socket reader leaked")
	}
}
