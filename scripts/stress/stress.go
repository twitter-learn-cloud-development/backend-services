//go:build ignore
// +build ignore

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

const gatewayURL = "http://localhost:9638/api/v1"

type LoginResponse struct {
	Token string `json:"token"`
}

func main() {
	log.Println("🔥 Starting KEDA Autoscaling Stress Test...")

	// 1. 注册并登录测试用户
	username := fmt.Sprintf("str_%d", time.Now().Unix()%1000000)
	email := fmt.Sprintf("%s@example.com", username)
	token := registerAndLogin(username, email, "password123")

	if token == "" {
		log.Fatalf("❌ Failed to acquire auth token")
	}

	const concurrency = 10
	const requestsPerRoutine = 50
	totalRequests := concurrency * requestsPerRoutine

	log.Printf("🚀 Spawning %d concurrent workers to send %d tweets (Total: %d)...", concurrency, requestsPerRoutine, totalRequests)

	var wg sync.WaitGroup
	startTime := time.Now()

	successCount := 0
	errorCount := 0
	var mu sync.Mutex

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < requestsPerRoutine; j++ {
				content := fmt.Sprintf("[STRESS TEST] Worker %d - Tweet #%d at %s", workerID, j, time.Now().Format(time.RFC3339Nano))
				err := createTweet(token, content)
				mu.Lock()
				if err != nil {
					errorCount++
					log.Printf("❌ Tweet creation failed: %v", err)
				} else {
					successCount++
				}
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(startTime)

	log.Printf("📊 Stress Test Finished!")
	log.Printf("   - Duration: %v", duration)
	log.Printf("   - Total Requests: %d", totalRequests)
	log.Printf("   - Successful Tweets: %d", successCount)
	log.Printf("   - Failed/Throttled Requests: %d", errorCount)
	log.Printf("   - Average RPS: %.2f", float64(successCount)/duration.Seconds())
}

func registerAndLogin(username, email, password string) string {
	// Register
	regBody := map[string]string{
		"username": username,
		"email":    email,
		"password": password,
	}
	sendRequest("POST", "/auth/register", regBody, "")
	log.Printf("✅ Registered user: %s", username)

	// Login
	loginBody := map[string]string{
		"email":    email,
		"password": password,
	}
	resp := sendRequest("POST", "/auth/login", loginBody, "")

	var loginResp LoginResponse
	json.Unmarshal(resp, &loginResp)
	return loginResp.Token
}

func createTweet(token, content string) error {
	body := map[string]interface{}{
		"content": content,
	}
	jsonBody, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", gatewayURL+"/tweets", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBytes))
	}
	return nil
}

func sendRequest(method, endpoint string, body interface{}, token string) []byte {
	jsonBody, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, gatewayURL+endpoint, bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("❌ Request failed: %v", err)
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		log.Fatalf("❌ API Error (%d): %s", resp.StatusCode, string(respBytes))
	}
	return respBytes
}
