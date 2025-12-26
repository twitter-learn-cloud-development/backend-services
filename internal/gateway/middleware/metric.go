package middleware

import (
	"strconv"
	"time"

	"twitter-clone/pkg/metric"

	"github.com/gin-gonic/gin"
)

// MetricsMiddleware records HTTP metrics for Prometheus
func MetricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		c.Next()

		status := strconv.Itoa(c.Writer.Status())
		duration := time.Since(start).Seconds()

		// Record metrics
		metric.HttpRequestCount.WithLabelValues(c.Request.Method, path, status).Inc()
		metric.HttpRequestDuration.WithLabelValues(c.Request.Method, path).Observe(duration)
	}
}
