package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

const supportQueryTestSecret = "support-query-test-secret-at-least-32-bytes"

func issueSupportQueryTestToken(t *testing.T, tokenID, actorUserID, audience string) string {
	t.Helper()
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": "support-agent-service",
		"aud": audience,
		"sub": "support-agent-service",
		"act": map[string]any{"user_id": actorUserID},
		"iat": now.Unix(),
		"nbf": now.Unix(),
		"exp": now.Add(30 * time.Second).Unix(),
		"jti": tokenID,
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(supportQueryTestSecret))
	require.NoError(t, err)
	return token
}

func TestSupportQueryAuthValidatesActorAndRejectsReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth, err := NewSupportQueryAuth(
		supportQueryTestSecret,
		"support-agent-service",
		"twitter-support-query",
		"support-agent-service",
		NewMemoryServiceTokenReplayGuard(),
		time.Minute,
	)
	require.NoError(t, err)
	router := gin.New()
	router.POST("/query", auth.Authenticate(), func(c *gin.Context) {
		actorUserID, ok := SupportQueryActorUserID(c)
		require.True(t, ok)
		c.JSON(http.StatusOK, gin.H{"actor_user_id": actorUserID})
	})
	token := issueSupportQueryTestToken(t, "token-1", "42", "twitter-support-query")

	first := httptest.NewRecorder()
	firstRequest := httptest.NewRequest(http.MethodPost, "/query", nil)
	firstRequest.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(first, firstRequest)
	require.Equal(t, http.StatusOK, first.Code)
	require.Contains(t, first.Body.String(), `"actor_user_id":42`)

	replayed := httptest.NewRecorder()
	replayedRequest := httptest.NewRequest(http.MethodPost, "/query", nil)
	replayedRequest.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(replayed, replayedRequest)
	require.Equal(t, http.StatusUnauthorized, replayed.Code)
	require.Contains(t, replayed.Header().Get("Content-Type"), "application/problem+json")
	require.NotContains(t, replayed.Body.String(), token)
}

func TestSupportQueryAuthRejectsWrongAudienceAndInvalidActor(t *testing.T) {
	auth, err := NewSupportQueryAuth(
		supportQueryTestSecret,
		"support-agent-service",
		"twitter-support-query",
		"support-agent-service",
		NewMemoryServiceTokenReplayGuard(),
		time.Minute,
	)
	require.NoError(t, err)
	router := gin.New()
	router.POST("/query", auth.Authenticate(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	for _, token := range []string{
		issueSupportQueryTestToken(t, "token-wrong-aud", "42", "other-audience"),
		issueSupportQueryTestToken(t, "token-invalid-actor", "not-a-number", "twitter-support-query"),
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/query", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(response, request)
		require.Equal(t, http.StatusUnauthorized, response.Code)
	}
}

func TestRedisServiceTokenReplayGuardIsAtomic(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	guard := NewRedisServiceTokenReplayGuard(client)
	expiresAt := time.Now().Add(time.Minute)

	require.NoError(t, guard.Consume(context.Background(), "jti-1", expiresAt))
	require.ErrorIs(t, guard.Consume(context.Background(), "jti-1", expiresAt), ErrServiceTokenReplay)
}
