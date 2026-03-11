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

	userv1 "twitter-clone/api/user/v1"
)

func TestUserHandler_Register(t *testing.T) {
	// 1. Setup
	gin.SetMode(gin.TestMode)
	mockClient := new(MockUserServiceClient)
	handler := NewUserHandler(mockClient, nil, nil)

	r := gin.New()
	r.POST("/api/v1/auth/register", handler.Register)

	// 2. Mock Expectations
	mockClient.On("Register", mock.Anything, mock.MatchedBy(func(req *userv1.RegisterRequest) bool {
		return req.Username == "alice" && req.Email == "alice@example.com"
	})).Return(&userv1.RegisterResponse{
		User: &userv1.User{
			Id:       1,
			Username: "alice",
			Email:    "alice@example.com",
		},
	}, nil)

	// 3. Request
	reqBody := RegisterRequest{
		Username: "alice",
		Email:    "alice@example.com",
		Password: "password123",
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "/api/v1/auth/register", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// 4. Assertions
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)

	userMap := resp["user"].(map[string]interface{})
	assert.Equal(t, "alice", userMap["username"])
	assert.Equal(t, "1", userMap["id"])

	mockClient.AssertExpectations(t)
}

func TestUserHandler_Register_InvalidInput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockClient := new(MockUserServiceClient)
	handler := NewUserHandler(mockClient, nil, nil)

	r := gin.New()
	r.POST("/api/v1/auth/register", handler.Register)

	// Invalid Request (missing password)
	reqBody := RegisterRequest{
		Username: "alice",
		Email:    "alice@example.com",
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "/api/v1/auth/register", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
