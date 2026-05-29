//go:build ignore
// +build ignore

package main

import (
	"bytes"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	gatewayURL := flag.String("url", "http://127.0.0.1:9638", "Gateway base URL")
	concurrency := flag.Int("c", 30, "Concurrency level (number of goroutines)")
	duration := flag.Duration("d", 20*time.Second, "Stress testing duration")
	flag.Parse()

	fmt.Printf("==================================================\n")
	fmt.Printf("🔥 Go Feeds Stress Test - Sentinel Resiliency Benchmark\n")
	fmt.Printf("   Target Gateway: %s\n", *gatewayURL)
	fmt.Printf("   Concurrency:    %d\n", *concurrency)
	fmt.Printf("   Duration:       %v\n", *duration)
	fmt.Printf("==================================================\n")

	// 🎯 核心自愈触发：在 3 秒后，由压测进程内部向网关发送 Firing 告警通知，触发 AIOps 自愈！
	go func() {
		time.Sleep(3 * time.Second)
		fmt.Printf("🔔 [AIOps Stress Tool] Automatically triggering simulated alert to gateway...\n")
		
		alertURL := fmt.Sprintf("%s/alerts", *gatewayURL)
		alertPayload := `{"status":"firing","groupKey":"redis-error-group"}`
		
		req, err := http.NewRequest("POST", alertURL, bytes.NewBufferString(alertPayload))
		if err != nil {
			fmt.Printf("❌ [AIOps Stress Tool] Failed to create alert request: %v\n", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Alertmanager-Token", "twitter-clone-secret-alert-token")
		
		// 独立客户端，避免干扰
		alertClient := &http.Client{Timeout: 5 * time.Second}
		resp, err := alertClient.Do(req)
		if err != nil {
			fmt.Printf("❌ [AIOps Stress Tool] Failed to send alert to gateway: %v\n", err)
			return
		}
		defer resp.Body.Close()
		
		respBytes, _ := io.ReadAll(resp.Body)
		fmt.Printf("✅ [AIOps Stress Tool] Gateway Alert Webhook responded (%d): %s\n", resp.StatusCode, string(respBytes))
	}()

	// 禁用 Keep-Alive 或者配置连接池，防止被连接占满
	tr := &http.Transport{
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
		DisableKeepAlives: false,
		MaxIdleConns:      100,
	}
	client := &http.Client{
		Transport: tr,
		Timeout:   2 * time.Second,
	}

	var successCount int64
	var error503Count int64 // 503 / 429 Sentinel CB
	var otherErrorCount int64
	var totalRequests int64

	stopChan := make(chan struct{})
	var wg sync.WaitGroup

	// 启动压测协程
	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			url := fmt.Sprintf("%s/api/v1/feeds?limit=20", *gatewayURL)

			for {
				select {
				case <-stopChan:
					return
				default:
					req, err := http.NewRequest("GET", url, nil)
					if err != nil {
						atomic.AddInt64(&otherErrorCount, 1)
						atomic.AddInt64(&totalRequests, 1)
						continue
					}

					// 携带混沌测试万能安全 Token
					req.Header.Set("Authorization", "Bearer CHAOS_MOCK_UNIVERSAL_TOKEN_999")
					
					resp, err := client.Do(req)
					atomic.AddInt64(&totalRequests, 1)
					if err != nil {
						atomic.AddInt64(&otherErrorCount, 1)
						continue
					}

					if resp.StatusCode == http.StatusOK {
						atomic.AddInt64(&successCount, 1)
					} else if resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusTooManyRequests {
						// Sentinel 熔断时返回 503 或限流 429
						atomic.AddInt64(&error503Count, 1)
					} else {
						atomic.AddInt64(&otherErrorCount, 1)
					}
					resp.Body.Close()
					
					// 适当微调请求间隔，避免把本地 CPU 打爆
					time.Sleep(10 * time.Millisecond)
				}
			}
		}(i)
	}

	// 监控输出进度
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		
		elapsed := 0
		for {
			select {
			case <-stopChan:
				return
			case <-ticker.C:
				elapsed++
				reqs := atomic.LoadInt64(&totalRequests)
				succs := atomic.LoadInt64(&successCount)
				cbs := atomic.LoadInt64(&error503Count)
				errs := atomic.LoadInt64(&otherErrorCount)
				fmt.Printf("[%ds] Total Reqs: %d | Success (200): %d | Sentinel CB (503/429): %d | Other Errs: %d\n", elapsed, reqs, succs, cbs, errs)
			}
		}
	}()

	// 运行指定时间
	time.Sleep(*duration)
	close(stopChan)
	wg.Wait()

	total := atomic.LoadInt64(&totalRequests)
	success := atomic.LoadInt64(&successCount)
	cb := atomic.LoadInt64(&error503Count)
	errs := atomic.LoadInt64(&otherErrorCount)

	durationSecs := duration.Seconds()
	rps := float64(total) / durationSecs

	fmt.Printf("==================================================\n")
	fmt.Printf("📊 Stress Test Results:\n")
	fmt.Printf("   - Duration:                  %.2fs\n", durationSecs)
	fmt.Printf("   - Total Requests:            %d\n", total)
	fmt.Printf("   - Successful (200 OK):       %d (%.2f%%)\n", success, float64(success)/float64(total)*100)
	fmt.Printf("   - Sentinel Intercepted:      %d (%.2f%%)\n", cb, float64(cb)/float64(total)*100)
	fmt.Printf("   - Failed/Network Errors:     %d (%.2f%%)\n", errs, float64(errs)/float64(total)*100)
	fmt.Printf("   - Average RPS:               %.2f req/sec\n", rps)
	fmt.Printf("==================================================\n")
}
