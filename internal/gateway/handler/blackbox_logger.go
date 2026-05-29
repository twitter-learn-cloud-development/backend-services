package handler

import (
	"fmt"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// BlackboxRingBuffer 线程安全的环形黑匣子
type BlackboxRingBuffer struct {
	mu       sync.RWMutex
	logs     [20]string // 固定长度 20，避免切片动态扩容带来的 GC 开销
	index    uint64
	capacity uint64
}

func NewBlackboxRingBuffer() *BlackboxRingBuffer {
	return &BlackboxRingBuffer{capacity: 20}
}

// Write 写入最新一条 ERROR 日志
func (r *BlackboxRingBuffer) Write(logMsg string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 求模覆盖，天然形成环
	idx := r.index % r.capacity
	r.logs[idx] = logMsg
	r.index++
}

// Dump 获取当前环中按时间顺序最新的所有日志
func (r *BlackboxRingBuffer) Dump() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []string
	count := r.index
	if count > r.capacity {
		count = r.capacity
	}

	// 从最老的一条记录开始遍历，保证输出时间序正确
	var startIdx uint64
	if r.index > r.capacity {
		startIdx = r.index - r.capacity
	}

	for i := uint64(0); i < count; i++ {
		idx := (startIdx + i) % r.capacity
		if r.logs[idx] != "" {
			result = append(result, r.logs[idx])
		}
	}
	return result
}

// GlobalBlackboxLogger 全局环形日志缓冲实例
var GlobalBlackboxLogger = NewBlackboxRingBuffer()

// RecordError 手工向黑匣子中记录一条错误
func RecordError(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	GlobalBlackboxLogger.Write(fmt.Sprintf("[%s] ERROR: %s", time.Now().Format("2006-01-02 15:04:05"), msg))
}

// BlackboxLoggerMiddleware 网关级自动截留 5xx 错误日志中间件
func BlackboxLoggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if c.Writer.Status() >= 500 {
			logMsg := fmt.Sprintf("[%s] %s %s -> Status: %d",
				time.Now().Format("2006-01-02 15:04:05"),
				c.Request.Method,
				c.Request.URL.Path,
				c.Writer.Status(),
			)
			if len(c.Errors) > 0 {
				logMsg += fmt.Sprintf(" | Errors: %s", c.Errors.String())
			}
			GlobalBlackboxLogger.Write(logMsg)
		}
	}
}
