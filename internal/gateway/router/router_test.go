package router

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alibaba/sentinel-golang/core/circuitbreaker"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"

	aiAgentv1 "twitter-clone/api/aiAgent/v1"
	"twitter-clone/internal/gateway/handler"
	"twitter-clone/internal/gateway/middleware"
)

// MockAiAgentServiceClient 仅 mock AnalyzeAlert
type MockAiAgentServiceClient struct {
	aiAgentv1.AiAgentServiceClient
	CallCount int32
	MockRCA   string
}

func (m *MockAiAgentServiceClient) AnalyzeAlert(ctx context.Context, in *aiAgentv1.AnalyzeAlertRequest, opts ...grpc.CallOption) (*aiAgentv1.AnalyzeAlertResponse, error) {
	atomic.AddInt32(&m.CallCount, 1)
	return &aiAgentv1.AnalyzeAlertResponse{
		Msg:           "success",
		StructuredRca: m.MockRCA,
	}, nil
}

func TestRouter_AlertsWebhook(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockClient := &MockAiAgentServiceClient{}
	agentHandler := handler.NewAgentHandler(mockClient)

	// 最小化路由初始化，传入空指针或占位符
	r := SetupRouter(
		&handler.TweetHandler{},
		&handler.FollowHandler{},
		&handler.UserHandler{},
		&handler.UploadHandler{},
		&handler.NotificationHandler{},
		&handler.BookmarkHandler{},
		&handler.MessengerHandler{},
		&handler.WebSocketHandler{},
		agentHandler,
		&middleware.JWTMiddleware{},
		nil,
	)

	// 用例 1: 未提供 Header Token
	{
		req, _ := http.NewRequest(http.MethodPost, "/alerts", bytes.NewBufferString(`{"status":"firing","groupKey":"key-1"}`))
		resp := httptest.NewRecorder()
		r.ServeHTTP(resp, req)

		if resp.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.Code)
		}
	}

	// 用例 2: 状态不是 firing (是 resolved)
	{
		req, _ := http.NewRequest(http.MethodPost, "/alerts", bytes.NewBufferString(`{"status":"resolved","groupKey":"key-2"}`))
		req.Header.Set("X-Alertmanager-Token", "twitter-clone-secret-alert-token")
		resp := httptest.NewRecorder()
		r.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.Code)
		}
		var body map[string]interface{}
		_ = json.Unmarshal(resp.Body.Bytes(), &body)
		if body["status"] != "ignored" {
			t.Errorf("expected status 'ignored', got %v", body["status"])
		}
	}

	// 用例 3: 状态是 firing，应该接收并异步触发诊断
	{
		req, _ := http.NewRequest(http.MethodPost, "/alerts", bytes.NewBufferString(`{"status":"firing","groupKey":"key-3"}`))
		req.Header.Set("X-Alertmanager-Token", "twitter-clone-secret-alert-token")
		resp := httptest.NewRecorder()
		r.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.Code)
		}
		var body map[string]interface{}
		_ = json.Unmarshal(resp.Body.Bytes(), &body)
		if body["status"] != "accepted" {
			t.Errorf("expected status 'accepted', got %v", body["status"])
		}

		// 等待异步协程执行完
		time.Sleep(100 * time.Millisecond)
		if count := atomic.LoadInt32(&mockClient.CallCount); count != 1 {
			t.Errorf("expected 1 AnalyzeAlert call, got %d", count)
		}
	}

	// 用例 4: 相同 groupKey 短时间内再次触发，应该被防抖机制过滤
	{
		req, _ := http.NewRequest(http.MethodPost, "/alerts", bytes.NewBufferString(`{"status":"firing","groupKey":"key-3"}`))
		req.Header.Set("X-Alertmanager-Token", "twitter-clone-secret-alert-token")
		resp := httptest.NewRecorder()
		r.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.Code)
		}
		var body map[string]interface{}
		_ = json.Unmarshal(resp.Body.Bytes(), &body)
		if body["status"] != "debounced" {
			t.Errorf("expected status 'debounced', got %v", body["status"])
		}

		// 应该依然只有 1 次 AnalyzeAlert 调用，不会增加
		time.Sleep(100 * time.Millisecond)
		if count := atomic.LoadInt32(&mockClient.CallCount); count != 1 {
			t.Errorf("expected call count to remain 1, got %d", count)
		}
	}

	// 用例 5: 状态是 firing，且大模型成功返回自愈指令 (TriggerCircuitBreaker)，触发动态熔断
	{
		mockClient.MockRCA = `{"root_cause":"RedisDown","action":"TriggerCircuitBreaker","resource":"GET:/api/v1/feeds"}`
		
		req, _ := http.NewRequest(http.MethodPost, "/alerts", bytes.NewBufferString(`{"status":"firing","groupKey":"key-5"}`))
		req.Header.Set("X-Alertmanager-Token", "twitter-clone-secret-alert-token")
		resp := httptest.NewRecorder()
		r.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.Code)
		}

		// 等待异步协程执行完并触发熔断
		time.Sleep(150 * time.Millisecond)
		
		// 验证 Sentinel 中是否已经成功动态写入了该规则
		found := false
		rules := circuitbreaker.GetRules()
		for _, rule := range rules {
			if rule.Resource == "GET:/api/v1/feeds" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("self-healing failed: dynamic circuit breaker for 'GET:/api/v1/feeds' was not injected")
		}
	}
}
