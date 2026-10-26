package supportquery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	tweetv1 "twitter-clone/api/tweet/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const ContentListRecentOperation = "content.list_recent"

type TweetTimelineClient interface {
	GetUserTimeline(
		ctx context.Context,
		in *tweetv1.GetUserTimelineRequest,
		opts ...grpc.CallOption,
	) (*tweetv1.GetUserTimelineResponse, error)
}

type ContentListRecent struct {
	tweets TweetTimelineClient
	now    func() time.Time
}

func NewContentListRecent(tweets TweetTimelineClient) *ContentListRecent {
	return &ContentListRecent{tweets: tweets, now: time.Now}
}

func (o *ContentListRecent) Name() string {
	return ContentListRecentOperation
}

type contentListRecentArguments struct {
	Limit    int32  `json:"limit"`
	TimeFrom string `json:"time_from"`
}

func (o *ContentListRecent) Execute(
	ctx context.Context,
	actorUserID uint64,
	arguments json.RawMessage,
) (Result, error) {
	args, err := parseContentListRecentArguments(arguments)
	if err != nil {
		return Result{}, err
	}
	response, err := o.tweets.GetUserTimeline(ctx, &tweetv1.GetUserTimelineRequest{
		UserId:           actorUserID,
		RequestingUserId: actorUserID,
		Limit:            args.Limit,
	})
	if err != nil {
		return Result{}, mapTweetError(err)
	}
	if response == nil {
		return Result{}, upstreamInvalidResponse()
	}

	items := make([]map[string]any, 0, len(response.Tweets))
	visibilityCounts := map[string]int{"public": 0, "followers": 0, "private": 0, "restricted": 0}
	for _, tweet := range response.Tweets {
		if tweet == nil || tweet.UserId != actorUserID {
			return Result{}, upstreamInvalidResponse()
		}
		createdAt := time.UnixMilli(tweet.CreatedAt).UTC()
		if !args.timeFrom.IsZero() && createdAt.Before(args.timeFrom) {
			continue
		}
		visibility := safeVisibility(tweet.VisibleType)
		visibilityCounts[visibility]++
		items = append(items, map[string]any{
			"resource_ref": fmt.Sprintf("tweet:%d", tweet.Id),
			"visibility":   visibility,
			"created_at":   createdAt.Format(time.RFC3339),
		})
	}

	observedAt := o.now().UTC()
	evidenceID := stableEvidenceID(actorUserID, observedAt, len(items))
	summary := fmt.Sprintf(
		"The authenticated user has %d recent content items: %d public, %d followers-only, %d private, and %d restricted.",
		len(items),
		visibilityCounts["public"],
		visibilityCounts["followers"],
		visibilityCounts["private"],
		visibilityCounts["restricted"],
	)
	return Result{
		Data: map[string]any{
			"items":    items,
			"has_more": response.HasMore,
		},
		Evidence: []Evidence{
			{
				EvidenceID:  evidenceID,
				SourceType:  "business_api",
				SourceRef:   "content:authenticated-user:recent",
				ObservedAt:  observedAt,
				SafeSummary: summary,
				Attributes: map[string]any{
					"count":              len(items),
					"public_count":       visibilityCounts["public"],
					"followers_count":    visibilityCounts["followers"],
					"private_count":      visibilityCounts["private"],
					"restricted_count":   visibilityCounts["restricted"],
					"additional_results": response.HasMore,
				},
			},
		},
		DataFreshness: "live",
	}, nil
}

type parsedContentListRecentArguments struct {
	Limit    int32
	timeFrom time.Time
}

func parseContentListRecentArguments(raw json.RawMessage) (parsedContentListRecentArguments, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var arguments contentListRecentArguments
	if err := decoder.Decode(&arguments); err != nil {
		return parsedContentListRecentArguments{}, invalidArguments()
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return parsedContentListRecentArguments{}, invalidArguments()
	}
	if arguments.Limit == 0 {
		arguments.Limit = 5
	}
	if arguments.Limit < 1 || arguments.Limit > 20 {
		return parsedContentListRecentArguments{}, invalidArguments()
	}
	var timeFrom time.Time
	if arguments.TimeFrom != "" {
		parsed, err := time.Parse(time.RFC3339, arguments.TimeFrom)
		if err != nil {
			return parsedContentListRecentArguments{}, invalidArguments()
		}
		timeFrom = parsed
	}
	return parsedContentListRecentArguments{Limit: arguments.Limit, timeFrom: timeFrom}, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("unexpected trailing JSON")
	}
	return nil
}

func invalidArguments() *Error {
	return NewError(
		http.StatusBadRequest,
		CodeInvalidArguments,
		"Invalid arguments",
		"The operation arguments do not match the registered schema.",
	)
}

func upstreamInvalidResponse() *Error {
	return NewError(
		http.StatusBadGateway,
		CodeUpstreamInvalidResponse,
		"Upstream invalid response",
		"The business service returned an invalid response.",
	)
}

func mapTweetError(err error) *Error {
	switch status.Code(err) {
	case codes.InvalidArgument:
		return invalidArguments()
	case codes.NotFound:
		return NewError(http.StatusNotFound, CodeResourceNotFound, "Resource not found", "The requested resource is not available.")
	case codes.PermissionDenied:
		return NewError(http.StatusForbidden, CodeOperationForbidden, "Operation forbidden", "The authenticated actor cannot access the requested resource.")
	case codes.DeadlineExceeded:
		return NewError(http.StatusGatewayTimeout, CodeQueryTimeout, "Query timeout", "The support query timed out.")
	case codes.ResourceExhausted:
		return NewError(http.StatusTooManyRequests, CodeQueryRateLimited, "Query rate limited", "The support query was rate limited.")
	case codes.Unavailable:
		return NewError(http.StatusServiceUnavailable, CodeQueryTemporarilyUnavailable, "Query temporarily unavailable", "The support query is temporarily unavailable.")
	default:
		return upstreamInvalidResponse()
	}
}

func safeVisibility(value int32) string {
	switch value {
	case 0:
		return "public"
	case 1:
		return "followers"
	case 2:
		return "private"
	default:
		return "restricted"
	}
}

func stableEvidenceID(actorUserID uint64, observedAt time.Time, count int) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%d", actorUserID, observedAt.UnixNano(), count)))
	return "ev_" + hex.EncodeToString(digest[:12])
}
