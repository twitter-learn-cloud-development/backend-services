package handler

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"twitter-clone/internal/gateway/middleware"
	"twitter-clone/pkg/logger"
)

var socketResults = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "gateway_realtime_events_total", Help: "Socket lifecycle and hint outcomes; not device acknowledgement."}, []string{"outcome"})

func init() { prometheus.MustRegister(socketResults) }

// WebSocketHandler WebSocket 处理器
type WebSocketHandler struct {
	redisClient  *redis.Client
	authMW       *middleware.GatewayAuthMiddleware
	originPolicy *middleware.OriginPolicy
}

// NewWebSocketHandler 创建 WebSocket 处理器
func NewWebSocketHandler(redisClient *redis.Client, authMW *middleware.GatewayAuthMiddleware) *WebSocketHandler {
	originPolicy, err := middleware.OriginPolicyFromEnvironment()
	if err != nil {
		panic("invalid websocket origin policy: " + err.Error())
	}
	return &WebSocketHandler{
		redisClient:  redisClient,
		authMW:       authMW,
		originPolicy: originPolicy,
	}
}

// HandleConnection 处理 WebSocket 连接
func (h *WebSocketHandler) HandleConnection(c *gin.Context) {
	// 1. 获取 UserID
	// 优先尝试从 Query Param 获取
	token := c.Query("token")
	var userID uint64
	var expiresAt time.Time

	if token != "" {
		// 手动验证 Token
		claims, err := h.authMW.ParseTokenViaJWKS(token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		userID = claims.UserID
		expiresAt = claims.ExpiresAt.Time
	} else {
		// 尝试从 Context 获取 (如果通过了 AuthRequired 中间件)
		// 但 WS 路由通常不经过 AuthRequired，因为握手是 GET 请求且 header 有限
		uid, exists := middleware.GetUserID(c)
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "token required"})
			return
		}
		userID = uid
	}
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid identity"})
		return
	}
	h.serveSocket(c, userID, expiresAt)
}

func (h *WebSocketHandler) serveSocket(c *gin.Context, userID uint64, expiresAt time.Time) {
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	if h.redisClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "realtime unavailable"})
		return
	}
	pubsub := h.redisClient.Subscribe(ctx, fmt.Sprintf("notifications:user:%d", userID))
	defer pubsub.Close()
	ackCtx, ackCancel := context.WithTimeout(ctx, 3*time.Second)
	_, ackErr := pubsub.Receive(ackCtx)
	ackCancel()
	if ackErr != nil {
		socketResults.WithLabelValues("subscribe_error").Inc()
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "realtime unavailable"})
		return
	}

	// 2. 升级连接
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     h.originPolicy.AllowsRequest,
	}
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Error(c, "Failed to upgrade websocket", zap.Error(err))
		return
	}
	defer ws.Close()
	socketResults.WithLabelValues("connected").Inc()
	ws.SetReadLimit(4096)
	_ = ws.SetReadDeadline(time.Now().Add(60 * time.Second))
	ws.SetPongHandler(func(string) error { return ws.SetReadDeadline(time.Now().Add(60 * time.Second)) })
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}()
	defer func() { cancel(); _ = ws.Close(); <-done; socketResults.WithLabelValues("closed").Inc() }()
	write := func(body []byte) error {
		_ = ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
		return ws.WriteMessage(websocket.TextMessage, body)
	}
	if err := write([]byte(`{"type":"ready","history_sync_required":true}`)); err != nil {
		return
	}
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	lifetime := 24 * time.Hour
	if !expiresAt.IsZero() {
		lifetime = time.Until(expiresAt)
	}
	expiry := time.NewTimer(lifetime)
	defer expiry.Stop()
	ch := pubsub.Channel(redis.WithChannelSize(64), redis.WithChannelSendTimeout(time.Second))
	for {
		select {
		case <-ctx.Done():
			return
		case <-expiry.C:
			_ = ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "reauthenticate"), time.Now().Add(time.Second))
			return
		case <-ping.C:
			if err := ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
				socketResults.WithLabelValues("write_error").Inc()
				return
			}
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if len(msg.Payload) > 65536 {
				socketResults.WithLabelValues("oversize").Inc()
				continue
			}
			if err := write([]byte(msg.Payload)); err != nil {
				socketResults.WithLabelValues("write_error").Inc()
				logger.Warn(ctx, "realtime socket write failed")
				return
			}
		}
	}
}
