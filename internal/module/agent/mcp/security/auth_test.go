package security

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	protocol "github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

const testToken = "0123456789abcdef0123456789abcdef"

func TestHTTPMiddlewareRejectsMissingToken(t *testing.T) {
	auth, err := NewAuthenticator(testToken)
	require.NoError(t, err)
	called := false
	handler := auth.HTTPMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/sse", nil))

	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.False(t, called)
}

func TestHTTPMiddlewareInjectsAuthenticatedContext(t *testing.T) {
	auth, err := NewAuthenticator(testToken)
	require.NoError(t, err)
	handler := auth.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		require.True(t, Authenticated(request.Context()))
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "/sse", nil)
	request.Header.Set("Authorization", "Bearer "+testToken)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusNoContent, response.Code)
}

func TestToolMiddlewareFailsClosedWithoutHTTPPrincipal(t *testing.T) {
	auth, err := NewAuthenticator(testToken)
	require.NoError(t, err)
	called := false
	handler := auth.ToolMiddleware()(func(context.Context, protocol.CallToolRequest) (*protocol.CallToolResult, error) {
		called = true
		return protocol.NewToolResultText("ok"), nil
	})

	result, err := handler(context.Background(), protocol.CallToolRequest{})

	require.NoError(t, err)
	require.True(t, result.IsError)
	require.False(t, called)
}

func TestToolMiddlewareAcceptsPrincipalFromHTTPBoundary(t *testing.T) {
	auth, err := NewAuthenticator(testToken)
	require.NoError(t, err)
	called := false
	toolHandler := auth.ToolMiddleware()(func(context.Context, protocol.CallToolRequest) (*protocol.CallToolResult, error) {
		called = true
		return protocol.NewToolResultText("ok"), nil
	})
	handler := auth.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		result, callErr := toolHandler(request.Context(), protocol.CallToolRequest{})
		require.NoError(t, callErr)
		require.False(t, result.IsError)
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodPost, "/message", nil)
	request.Header.Set("Authorization", "Bearer "+testToken)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusNoContent, response.Code)
	require.True(t, called)
}
