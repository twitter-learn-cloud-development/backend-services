package grpc

import (
	"testing"

	tweetv1 "twitter-clone/api/tweet/v1"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestCreateTweetRequestFieldCompatibilityContract(t *testing.T) {
	message := tweetv1.File_api_tweet_v1_tweet_proto.Messages().ByName("CreateTweetRequest")
	if message == nil {
		t.Fatal("CreateTweetRequest protobuf descriptor is missing")
	}

	want := map[protoreflect.Name]protoreflect.FieldNumber{
		"user_id":               1,
		"content":               2,
		"media_urls":            3,
		"parent_id":             4,
		"poll_options":          5,
		"poll_duration_minutes": 6,
		"idempotency_key":       7,
	}
	for name, number := range want {
		field := message.Fields().ByName(name)
		if field == nil {
			t.Fatalf("CreateTweetRequest.%s is missing", name)
		}
		if field.Number() != number {
			t.Fatalf("CreateTweetRequest.%s field number = %d, want %d", name, field.Number(), number)
		}
	}
}

func TestRiskControlRPCCompatibilityContract(t *testing.T) {
	service := tweetv1.File_api_tweet_v1_tweet_proto.Services().ByName("TweetService")
	if service == nil {
		t.Fatal("TweetService protobuf descriptor is missing")
	}
	for _, methodName := range []protoreflect.Name{"GetAuthorPostingStats", "ApplyTweetModeration"} {
		if service.Methods().ByName(methodName) == nil {
			t.Fatalf("TweetService.%s is missing", methodName)
		}
	}

	assertMessageFieldNumbers(t, "GetAuthorPostingStatsRequest", map[protoreflect.Name]protoreflect.FieldNumber{
		"author_id":        1,
		"lookback_seconds": 2,
	})
	assertMessageFieldNumbers(t, "GetAuthorPostingStatsResponse", map[protoreflect.Name]protoreflect.FieldNumber{
		"sample_count":        1,
		"latest_created_at":   2,
		"previous_created_at": 3,
	})
	assertMessageFieldNumbers(t, "ApplyTweetModerationRequest", map[protoreflect.Name]protoreflect.FieldNumber{
		"tweet_id":    1,
		"author_id":   2,
		"action":      3,
		"reason_code": 4,
	})
	assertMessageFieldNumbers(t, "ApplyTweetModerationResponse", map[protoreflect.Name]protoreflect.FieldNumber{
		"applied":           1,
		"timelines_cleaned": 2,
		"cleanup_queued":    3,
	})

	action := tweetv1.File_api_tweet_v1_tweet_proto.Enums().ByName("TweetModerationAction")
	if action == nil {
		t.Fatal("TweetModerationAction protobuf descriptor is missing")
	}
	shadowban := action.Values().ByName("TWEET_MODERATION_ACTION_SHADOWBAN")
	if shadowban == nil || shadowban.Number() != 1 {
		t.Fatalf("TweetModerationAction shadowban value = %v, want 1", shadowban)
	}
}

func assertMessageFieldNumbers(t *testing.T, messageName protoreflect.Name, want map[protoreflect.Name]protoreflect.FieldNumber) {
	t.Helper()
	message := tweetv1.File_api_tweet_v1_tweet_proto.Messages().ByName(messageName)
	if message == nil {
		t.Fatalf("%s protobuf descriptor is missing", messageName)
	}
	for name, number := range want {
		field := message.Fields().ByName(name)
		if field == nil {
			t.Fatalf("%s.%s is missing", messageName, name)
		}
		if field.Number() != number {
			t.Fatalf("%s.%s field number = %d, want %d", messageName, name, field.Number(), number)
		}
	}
}
