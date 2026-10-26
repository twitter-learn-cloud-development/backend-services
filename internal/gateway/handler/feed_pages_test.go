package handler

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	tweetv1 "twitter-clone/api/tweet/v1"
	userv1 "twitter-clone/api/user/v1"
)

type feedPageClient struct {
	tweetv1.TweetServiceClient
	request *tweetv1.GetFeedsRequest
	err     error
}

func (f *feedPageClient) ListTweets(_ context.Context, _ *tweetv1.ListTweetsRequest, _ ...grpc.CallOption) (*tweetv1.ListTweetsResponse, error) {
	return &tweetv1.ListTweetsResponse{NextCursor: 9007199254740997, HasMore: true}, nil
}

func TestPublicHomeTabPreservesLargeCursorString(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/tweets/public", NewTweetHandler(&feedPageClient{}, nil).ListTweets)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/tweets/public", nil))
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"next_cursor":"9007199254740997"`)
}

type feedProfileClient struct {
	userv1.UserServiceClient
	ids   []uint64
	calls int
}

func (f *feedProfileClient) GetBatchUsers(_ context.Context, req *userv1.GetBatchUsersRequest, _ ...grpc.CallOption) (*userv1.GetBatchUsersResponse, error) {
	f.ids = req.UserIds
	f.calls++
	return &userv1.GetBatchUsersResponse{Users: []*userv1.User{{Id: 2, Username: "author"}}}, nil
}
func TestFeedProfilesBatchDeduplication(t *testing.T) {
	f := &feedProfileClient{}
	rows := NewTweetHandler(nil, f).enrichFeedTweets(context.Background(), []*tweetv1.Tweet{{Id: 3, UserId: 2, IsLiked: true}, {Id: 2, UserId: 2}, {Id: 1, UserId: 4}})
	require.Equal(t, 1, f.calls)
	require.Equal(t, []uint64{2, 4}, f.ids)
	require.Len(t, rows, 3)
	require.Equal(t, true, rows[0]["is_liked"])
	require.Equal(t, "3", rows[0]["sort_id"])
	require.Equal(t, "author", rows[0]["user"].(gin.H)["username"])
	require.Equal(t, "unknown", rows[2]["user"].(gin.H)["username"])
}

func (f *feedPageClient) GetFeeds(_ context.Context, req *tweetv1.GetFeedsRequest, _ ...grpc.CallOption) (*tweetv1.GetFeedsResponse, error) {
	f.request = req
	if f.err != nil {
		return nil, f.err
	}
	return &tweetv1.GetFeedsResponse{NextCursor: 9007199254740997, HasMore: true}, nil
}
func feedPageRouter(client *feedPageClient, auth bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	if auth {
		r.Use(func(c *gin.Context) { c.Set("user_id", uint64(1)); c.Next() })
	}
	r.GET("/feeds", NewTweetHandler(client, nil).GetFeeds)
	return r
}
func TestFeedHTTPExactCursorEmptyContinuationAndQuery(t *testing.T) {
	f := &feedPageClient{}
	w := httptest.NewRecorder()
	feedPageRouter(f, true).ServeHTTP(w, httptest.NewRequest("GET", "/feeds?cursor=9007199254740999&limit=100", nil))
	require.Equal(t, 200, w.Code)
	require.EqualValues(t, 9007199254740999, f.request.Cursor)
	require.EqualValues(t, 1, f.request.UserId)
	require.EqualValues(t, 1, f.request.RequestingUserId)
	require.Contains(t, w.Body.String(), `"next_cursor":"9007199254740997"`)
	require.Contains(t, w.Body.String(), `"tweets":[]`)
	require.Contains(t, w.Body.String(), `"has_more":true`)
	for _, query := range []string{"?cursor=bad", "?cursor=-1", "?cursor=01", "?cursor=18446744073709551616", "?cursor=", "?cursor=1&cursor=2", "?limit=bad", "?limit=2147483648", "?limit=1&limit=2"} {
		f := &feedPageClient{}
		w := httptest.NewRecorder()
		feedPageRouter(f, true).ServeHTTP(w, httptest.NewRequest("GET", "/feeds"+query, nil))
		require.Equal(t, 400, w.Code, query)
		require.Nil(t, f.request, query)
	}
}
func TestFeedHTTPAuthenticationAndLowSensitiveErrors(t *testing.T) {
	w := httptest.NewRecorder()
	feedPageRouter(&feedPageClient{}, false).ServeHTTP(w, httptest.NewRequest("GET", "/feeds", nil))
	require.Equal(t, 401, w.Code)
	for code, want := range map[codes.Code]int{codes.InvalidArgument: 400, codes.Unavailable: 503, codes.DeadlineExceeded: 504, codes.Internal: 500} {
		w := httptest.NewRecorder()
		f := &feedPageClient{err: status.Error(code, "private SQL body")}
		feedPageRouter(f, true).ServeHTTP(w, httptest.NewRequest("GET", "/feeds", nil))
		require.Equal(t, want, w.Code)
		require.False(t, strings.Contains(w.Body.String(), "private"))
	}
}
