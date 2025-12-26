package metric

import (
	"fmt"
	"log"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Define core metrics
var (
	// HttpRequestCount counts total HTTP requests
	HttpRequestCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "endpoint", "status"},
	)

	// HttpRequestDuration measures HTTP request latency
	HttpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request latency in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "endpoint"},
	)
)

// InitMetrics registers the custom metrics
func InitMetrics() {
	prometheus.MustRegister(HttpRequestCount)
	prometheus.MustRegister(HttpRequestDuration)
}

// StartMetricsServer starts a background HTTP server for scraping metrics
func StartMetricsServer(port int) {
	go func() {
		addr := fmt.Sprintf(":%d", port)
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())

		log.Printf("📊 Metrics server listening on %s/metrics", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Printf("❌ Failed to start metrics server: %v", err)
		}
	}()
}
