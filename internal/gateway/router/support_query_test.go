package router

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	tweetv1 "twitter-clone/api/tweet/v1"
	"twitter-clone/internal/gateway/handler"
	"twitter-clone/internal/gateway/middleware"
	"twitter-clone/internal/module/supportquery"
)

const routeSupportQuerySecret = "support-query-route-test-secret-at-least-32-bytes"

type routeTimelineClient struct {
	calls   int
	request *tweetv1.GetUserTimelineRequest
}

func (f *routeTimelineClient) GetUserTimeline(
	ctx context.Context,
	in *tweetv1.GetUserTimelineRequest,
	opts ...grpc.CallOption,
) (*tweetv1.GetUserTimelineResponse, error) {
	_ = ctx
	_ = opts
	f.calls++
	f.request = in
	return &tweetv1.GetUserTimelineResponse{
		Tweets: []*tweetv1.Tweet{
			{
				Id:          100,
				UserId:      in.UserId,
				Content:     "content must not cross the support boundary",
				VisibleType: 0,
				CreatedAt:   time.Now().Unix(),
			},
		},
	}, nil
}

func TestSupportQueryRouteEnforcesIdentityContractAndIdempotency(t *testing.T) {
	gin.SetMode(gin.TestMode)
	timeline := &routeTimelineClient{}
	registry, err := supportquery.NewRegistry(supportquery.NewContentListRecent(timeline))
	require.NoError(t, err)
	queryHandler, err := handler.NewSupportQueryHandler(
		registry,
		supportquery.NewMemoryIdempotencyGuard(),
		time.Second,
		time.Minute,
	)
	require.NoError(t, err)
	auth, err := middleware.NewSupportQueryAuth(
		routeSupportQuerySecret,
		"support-agent-service",
		"twitter-support-query",
		"support-agent-service",
		middleware.NewMemoryServiceTokenReplayGuard(),
		time.Minute,
	)
	require.NoError(t, err)
	engine := gin.New()
	RegisterSupportQuery(engine, auth, queryHandler)

	valid := performSupportQuery(t, engine, "token-1", `{
		"operation":"content.list_recent",
		"arguments":{"limit":5},
		"request_context":{"case_id":"case-1","task_id":"task-1","tool_call_id":"call-1"}
	}`)
	require.Equal(t, http.StatusOK, valid.Code)
	require.Contains(t, valid.Body.String(), `"resource_ref":"tweet:100"`)
	require.NotContains(t, valid.Body.String(), "content must not cross")
	require.Equal(t, uint64(42), timeline.request.UserId)
	require.Equal(t, uint64(42), timeline.request.RequestingUserId)

	conflict := performSupportQuery(t, engine, "token-2", `{
		"operation":"content.list_recent",
		"arguments":{"limit":6},
		"request_context":{"case_id":"case-1","task_id":"task-1","tool_call_id":"call-1"}
	}`)
	require.Equal(t, http.StatusConflict, conflict.Code)
	require.Contains(t, conflict.Body.String(), supportquery.CodeToolCallConflict)

	forged := performSupportQuery(t, engine, "token-3", `{
		"operation":"content.list_recent",
		"arguments":{"limit":5,"user_id":"999"},
		"request_context":{"case_id":"case-1","task_id":"task-1","tool_call_id":"call-2"}
	}`)
	require.Equal(t, http.StatusBadRequest, forged.Code)
	require.Contains(t, forged.Body.String(), supportquery.CodeInvalidArguments)
	require.Equal(t, 1, timeline.calls)

	unknown := performSupportQuery(t, engine, "token-4", `{
		"operation":"unknown.read",
		"arguments":{},
		"request_context":{"case_id":"case-1","task_id":"task-1","tool_call_id":"call-3"}
	}`)
	require.Equal(t, http.StatusNotFound, unknown.Code)
	require.Contains(t, unknown.Body.String(), supportquery.CodeOperationNotFound)
}

func performSupportQuery(
	t *testing.T,
	engine *gin.Engine,
	tokenID string,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": "support-agent-service",
		"aud": "twitter-support-query",
		"sub": "support-agent-service",
		"act": map[string]any{"user_id": "42"},
		"iat": now.Unix(),
		"nbf": now.Unix(),
		"exp": now.Add(30 * time.Second).Unix(),
		"jti": tokenID,
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(routeSupportQuerySecret))
	require.NoError(t, err)
	request := httptest.NewRequest(
		http.MethodPost,
		"/internal/v1/support/query",
		bytes.NewBufferString(body),
	)
	request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	return response
}
