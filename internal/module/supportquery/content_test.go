package supportquery

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	tweetv1 "twitter-clone/api/tweet/v1"
)

type fakeTimelineClient struct {
	request  *tweetv1.GetUserTimelineRequest
	response *tweetv1.GetUserTimelineResponse
	err      error
	calls    int
}

func (f *fakeTimelineClient) GetUserTimeline(
	ctx context.Context,
	in *tweetv1.GetUserTimelineRequest,
	opts ...grpc.CallOption,
) (*tweetv1.GetUserTimelineResponse, error) {
	_ = ctx
	_ = opts
	f.calls++
	f.request = in
	return f.response, f.err
}

func TestContentListRecentUsesAuthenticatedActorAndReturnsSafeEvidence(t *testing.T) {
	observedAt := time.Date(2026, 9, 28, 6, 30, 0, 0, time.UTC)
	fake := &fakeTimelineClient{response: &tweetv1.GetUserTimelineResponse{
		Tweets: []*tweetv1.Tweet{
			{Id: 100, UserId: 42, Content: "private content must not leak", VisibleType: 0, CreatedAt: observedAt.Add(-time.Minute).UnixMilli()},
			{Id: 101, UserId: 42, Content: "also secret", VisibleType: 2, CreatedAt: observedAt.Add(-2 * time.Minute).UnixMilli()},
		},
		HasMore: true,
	}}
	operation := NewContentListRecent(fake)
	operation.now = func() time.Time { return observedAt }

	result, err := operation.Execute(context.Background(), 42, json.RawMessage(`{"limit":5}`))
	require.NoError(t, err)
	require.Equal(t, uint64(42), fake.request.UserId)
	require.Equal(t, uint64(42), fake.request.RequestingUserId)
	require.Equal(t, int32(5), fake.request.Limit)
	require.Len(t, result.Evidence, 1)
	require.NotContains(t, result.Evidence[0].SafeSummary, "private content")
	require.NotContains(t, result.Evidence[0].SafeSummary, "also secret")
	require.Equal(t, "business_api", result.Evidence[0].SourceType)
	require.Equal(t, true, result.Data["has_more"])
	items := result.Data["items"].([]map[string]any)
	require.Equal(t, "tweet:100", items[0]["resource_ref"])
	require.Equal(t, "2026-09-28T06:29:00Z", items[0]["created_at"])
	require.NotContains(t, items[0], "content")
}

func TestContentListRecentFiltersMillisecondTimestamps(t *testing.T) {
	boundary := time.Date(2026, 9, 28, 6, 30, 0, 0, time.UTC)
	fake := &fakeTimelineClient{response: &tweetv1.GetUserTimelineResponse{
		Tweets: []*tweetv1.Tweet{
			{Id: 100, UserId: 42, VisibleType: 0, CreatedAt: boundary.Add(-time.Millisecond).UnixMilli()},
			{Id: 101, UserId: 42, VisibleType: 1, CreatedAt: boundary.UnixMilli()},
			{Id: 102, UserId: 42, VisibleType: 2, CreatedAt: boundary.Add(time.Millisecond).UnixMilli()},
		},
	}}
	operation := NewContentListRecent(fake)
	result, err := operation.Execute(context.Background(), 42, json.RawMessage(`{"time_from":"2026-09-28T06:30:00Z"}`))
	require.NoError(t, err)
	items := result.Data["items"].([]map[string]any)
	require.Len(t, items, 2)
	require.Equal(t, "tweet:101", items[0]["resource_ref"])
	require.Equal(t, "tweet:102", items[1]["resource_ref"])
	require.Equal(t, "2026-09-28T06:30:00Z", items[0]["created_at"])
	require.Equal(t, "2026-09-28T06:30:00Z", items[1]["created_at"])
	require.Equal(t, 2, result.Evidence[0].Attributes["count"])
	require.Equal(t, 1, result.Evidence[0].Attributes["followers_count"])
	require.Equal(t, 1, result.Evidence[0].Attributes["private_count"])
}

func TestContentListRecentRejectsForgedUserIDBeforeCallingTweetService(t *testing.T) {
	fake := &fakeTimelineClient{}
	operation := NewContentListRecent(fake)

	_, err := operation.Execute(
		context.Background(),
		42,
		json.RawMessage(`{"limit":5,"user_id":"999"}`),
	)

	var queryError *Error
	require.ErrorAs(t, err, &queryError)
	require.Equal(t, CodeInvalidArguments, queryError.Code)
	require.Zero(t, fake.calls)
}

func TestContentListRecentFailsClosedOnCrossActorUpstreamData(t *testing.T) {
	fake := &fakeTimelineClient{response: &tweetv1.GetUserTimelineResponse{
		Tweets: []*tweetv1.Tweet{{Id: 100, UserId: 999, CreatedAt: time.Now().UnixMilli()}},
	}}
	operation := NewContentListRecent(fake)

	_, err := operation.Execute(context.Background(), 42, json.RawMessage(`{}`))

	var queryError *Error
	require.ErrorAs(t, err, &queryError)
	require.Equal(t, CodeUpstreamInvalidResponse, queryError.Code)
}

func TestContentListRecentMapsStableDownstreamErrors(t *testing.T) {
	tests := []struct {
		name       string
		grpcCode   codes.Code
		queryCode  string
		httpStatus int
	}{
		{"not found", codes.NotFound, CodeResourceNotFound, 404},
		{"forbidden", codes.PermissionDenied, CodeOperationForbidden, 403},
		{"rate limited", codes.ResourceExhausted, CodeQueryRateLimited, 429},
		{"unavailable", codes.Unavailable, CodeQueryTemporarilyUnavailable, 503},
		{"timeout", codes.DeadlineExceeded, CodeQueryTimeout, 504},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation := NewContentListRecent(&fakeTimelineClient{
				err: status.Error(test.grpcCode, "sensitive downstream error"),
			})
			_, err := operation.Execute(context.Background(), 42, json.RawMessage(`{}`))
			var queryError *Error
			require.ErrorAs(t, err, &queryError)
			require.Equal(t, test.queryCode, queryError.Code)
			require.Equal(t, test.httpStatus, queryError.Status)
			require.NotContains(t, queryError.Detail, "sensitive")
		})
	}
}

func TestRegistryAndIdempotencyGuard(t *testing.T) {
	registry, err := NewRegistry(NewContentListRecent(&fakeTimelineClient{}))
	require.NoError(t, err)
	_, err = registry.Execute(context.Background(), 42, Request{Operation: "unknown.read"})
	var queryError *Error
	require.ErrorAs(t, err, &queryError)
	require.Equal(t, CodeOperationNotFound, queryError.Code)

	guard := NewMemoryIdempotencyGuard()
	require.NoError(t, guard.Check(context.Background(), 42, "call-1", "fingerprint-a", time.Minute))
	require.NoError(t, guard.Check(context.Background(), 42, "call-1", "fingerprint-a", time.Minute))
	require.True(t, errors.Is(
		guard.Check(context.Background(), 42, "call-1", "fingerprint-b", time.Minute),
		ErrToolCallConflict,
	))
}
