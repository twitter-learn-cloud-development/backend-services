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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	tweetv1 "twitter-clone/api/tweet/v1"
	userv1 "twitter-clone/api/user/v1"
)

func TestTweetHandler_CreateTweet(t *testing.T) {
	// 1. Setup
	gin.SetMode(gin.TestMode)
	mockClient := new(MockTweetServiceClient)
	mockUserClient := new(MockUserServiceClient)
	handler := NewTweetHandler(mockClient, mockUserClient)

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

	mockUserClient.On("GetProfile", mock.Anything, mock.MatchedBy(func(req *userv1.GetProfileRequest) bool {
		return req.UserId == 123
	})).Return(&userv1.GetProfileResponse{
		User: &userv1.User{
			Id:       123,
			Username: "testuser",
			Avatar:   "avatar.jpg",
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
	assert.Equal(t, "1001", tweetMap["id"])

	mockClient.AssertExpectations(t)
	mockUserClient.AssertExpectations(t)
}

func TestTweetHandler_CreateTweet_Unauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockClient := new(MockTweetServiceClient)
	mockUserClient := new(MockUserServiceClient)
	handler := NewTweetHandler(mockClient, mockUserClient)

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

func TestTweetHandler_CreateTweet_InvalidMediaURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockClient := new(MockTweetServiceClient)
	handler := NewTweetHandler(mockClient, new(MockUserServiceClient))
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uint64(123))
		c.Next()
	})
	r.POST("/api/v1/tweets", handler.CreateTweet)
	mockClient.On("CreateTweet", mock.Anything, mock.Anything).Return(
		(*tweetv1.CreateTweetResponse)(nil), status.Error(codes.InvalidArgument, "invalid media url"),
	)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/tweets", bytes.NewBufferString(`{"content":"hello","media_urls":["/media/other-bucket/a.png"]}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	assert.Equal(t, http.StatusBadRequest, response.Code)
	mockClient.AssertExpectations(t)
}
