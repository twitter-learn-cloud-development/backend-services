package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

const gatewayURL = "http://localhost:9638/api/v1"

type RegisterResponse struct {
	User struct {
		ID       uint64 `json:"id"`
		Username string `json:"username"`
	} `json:"user"`
}

type LoginResponse struct {
	Token string `json:"token"`
}

type TweetResponse struct {
	Tweet struct {
		ID uint64 `json:"id"`
	} `json:"tweet"`
}

func main() {
	log.Println("🌱 Starting Database Seeding via API...")

	// 1. Register Users
	tokenA := registerAndLogin("user_a", "user_a@example.com", "password123")
	tokenB := registerAndLogin("user_b", "user_b@example.com", "password123")

	// 2. User A posts Tweets
	log.Println("📝 User A is posting tweets...")
	for i := 1; i <= 5; i++ {
		content := fmt.Sprintf("Hello World! This is tweet #%d from User A 🚀", i)
		createTweet(tokenA, content)
		time.Sleep(100 * time.Millisecond) // Avoid rate limit if strict
	}

	// 3. User B posts Tweets
	log.Println("📝 User B is posting tweets...")
	createTweet(tokenB, "I am User B, nice to meet you!")

	// 4. User B follows User A
	log.Println("👥 User B is following User A...")
	// We need User A's ID. For simplicity, we assume we can get it from the register response or just know it.
	// But `registerAndLogin` returns token. Let's make it return user ID too?
	// Or we just fetch User A's profile.
	// For this script, I'll update registerAndLogin to return ID.
	// Re-implementing simplified logic above for clarity.
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
	log.Printf("🔑 Logged in %s, Token len: %d", username, len(loginResp.Token))
	return loginResp.Token
}

func createTweet(token, content string) {
	body := map[string]interface{}{
		"content": content,
	}
	resp := sendRequest("POST", "/tweets", body, token)
	var tweetResp TweetResponse
	json.Unmarshal(resp, &tweetResp)
	log.Printf("   -> Created Tweet ID: %d", tweetResp.Tweet.ID)
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

	if resp.StatusCode >= 400 {
		respBytes, _ := io.ReadAll(resp.Body)
		// Ignore "user already exists" for idempotency
		if method == "POST" && endpoint == "/auth/register" {
			return nil
		}
		log.Printf("⚠️ API Error (%d): %s", resp.StatusCode, string(respBytes))
	}

	respBytes, _ := io.ReadAll(resp.Body)
	return respBytes
}
