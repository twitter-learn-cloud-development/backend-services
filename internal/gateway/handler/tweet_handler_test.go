package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	tweetv1 "twitter-clone/api/tweet/v1"
)

func TestTweetHandler_CreateTweet(t *testing.T) {
	// 1. Setup
	gin.SetMode(gin.TestMode)
	mockClient := new(MockTweetServiceClient)
	handler := NewTweetHandler(mockClient, nil)

	r := gin.New()

	// Mock Auth Middleware
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uint64(123))
		c.Next()
	})

	r.POST("/api/v1/tweets", handler.CreateTweet)

	// 2. Mock Expectations
	mockClient.On("CreateTweet", mock.Anything, mock.MatchedBy(func(req *tweetv1.CreateTweetRequest) bool {
		return req.UserId == 123 && req.Content == "Hello World"
	})).Return(&tweetv1.CreateTweetResponse{
		Tweet: &tweetv1.Tweet{
			Id:      1001,
			UserId:  123,
			Content: "Hello World",
		},
	}, nil)

	// 3. Request
	reqBody := CreateTweetRequest{
		Content: "Hello World",
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "/api/v1/tweets", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// 4. Assertions
	assert.Equal(t, http.StatusCreated, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)

	tweetMap := resp["tweet"].(map[string]interface{})
	assert.Equal(t, "Hello World", tweetMap["content"])
	assert.Equal(t, float64(1001), tweetMap["id"])

	mockClient.AssertExpectations(t)
}

func TestTweetHandler_CreateTweet_Unauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockClient := new(MockTweetServiceClient)
	handler := NewTweetHandler(mockClient, nil)

	r := gin.New()
	// No Auth Middleware

	r.POST("/api/v1/tweets", handler.CreateTweet)

	reqBody := CreateTweetRequest{Content: "Hello World"}
	bodyBytes, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "/api/v1/tweets", bytes.NewBuffer(bodyBytes))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
