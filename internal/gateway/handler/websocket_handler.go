package handler

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"twitter-clone/internal/gateway/middleware"
	"twitter-clone/pkg/logger"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// 允许跨域
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// WebSocketHandler WebSocket 处理器
type WebSocketHandler struct {
	redisClient *redis.Client
	jwtMW       *middleware.JWTMiddleware
}

// NewWebSocketHandler 创建 WebSocket 处理器
func NewWebSocketHandler(redisClient *redis.Client, jwtMW *middleware.JWTMiddleware) *WebSocketHandler {
	return &WebSocketHandler{
		redisClient: redisClient,
		jwtMW:       jwtMW,
	}
}

// HandleConnection 处理 WebSocket 连接
func (h *WebSocketHandler) HandleConnection(c *gin.Context) {
	// 1. 获取 UserID
	// 优先尝试从 Query Param 获取
	token := c.Query("token")
	var userID uint64

	if token != "" {
		// 手动验证 Token
		claims, err := h.jwtMW.ParseToken(token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		userID = claims.UserID
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

	// 2. 升级连接
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Error(c, "Failed to upgrade websocket", zap.Error(err))
		return
	}
	defer ws.Close()

	logger.Info(c, "🔌 WebSocket connected", zap.Uint64("user_id", userID))

	// 3. 订阅 Redis
	channel := fmt.Sprintf("notifications:user:%d", userID)
	pubsub := h.redisClient.Subscribe(c, channel)
	defer pubsub.Close()

	// 4. 处理消息循环
	// 创建一个 channel 来控制退出
	done := make(chan struct{})

	// 启动一个 goroutine 读取 Redis 消息并推送到 WebSocket
	go func() {
		ch := pubsub.Channel()
		for {
			select {
			case msg, ok := <-ch:
				if !ok {
					return
				}
				// msg.Payload 是 JSON 字符串
				logger.Info(c, "📨 Gateway received Redis message", zap.String("channel", msg.Channel), zap.String("payload", msg.Payload))
				if err := ws.WriteMessage(websocket.TextMessage, []byte(msg.Payload)); err != nil {
					logger.Warn(c, "Failed to write websocket message", zap.Error(err))
					close(done)
					return
				}
			case <-done:
				return
			}
		}
	}()

	// 主循环：处理 WebSocket PING/PONG 和 关闭
	// 也可以由客户端发消息过来，这里暂时只需要服务端推送
	for {
		// 阻塞读取，检测连接状态
		_, _, err := ws.ReadMessage()
		if err != nil {
			logger.Info(c, "🔌 WebSocket disconnected", zap.Uint64("user_id", userID), zap.Error(err))
			close(done)
			break
		}
	}
}
