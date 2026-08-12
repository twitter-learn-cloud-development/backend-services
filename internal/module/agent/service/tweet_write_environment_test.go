package service

import (
	"context"
	"encoding/json"
	"testing"

	tweetv1 "twitter-clone/api/tweet/v1"
	agentEnvironment "twitter-clone/internal/module/agent/environment"
	agentRuntime "twitter-clone/internal/module/agent/runtime"

	"google.golang.org/grpc"
)

type tweetTimelineClientStub struct {
	request  *tweetv1.GetUserTimelineRequest
	response *tweetv1.GetUserTimelineResponse
	err      error
}

func (stub *tweetTimelineClientStub) GetUserTimeline(
	_ context.Context,
	request *tweetv1.GetUserTimelineRequest,
	_ ...grpc.CallOption,
) (*tweetv1.GetUserTimelineResponse, error) {
	stub.request = request
	return stub.response, stub.err
}

func TestTweetServiceWriteStateReaderUsesAuthoritativeTimelineWithoutContent(t *testing.T) {
	client := &tweetTimelineClientStub{response: &tweetv1.GetUserTimelineResponse{
		Tweets: []*tweetv1.Tweet{
			{Id: 12, UserId: 99, Content: "retweet source content"},
			{Id: 11, UserId: 7, Content: "private verification content"},
		},
		HasMore: true,
	}}
	page, err := (tweetServiceWriteStateReader{client: client}).ListRecentTweetWriteState(
		context.Background(), 7, 64,
	)
	if err != nil {
		t.Fatalf("ListRecentTweetWriteState() error = %v", err)
	}
	if client.request == nil || client.request.UserId != 7 || client.request.RequestingUserId != 7 || client.request.Limit != 64 {
		t.Fatalf("timeline request = %+v", client.request)
	}
	if len(page.Tweets) != 1 || page.Tweets[0].TweetID != 11 || page.Tweets[0].AuthorID != 7 || !page.HasMore {
		t.Fatalf("state page = %+v", page)
	}
}

func TestNewTweetWriteEnvironmentRequiresBothAdapters(t *testing.T) {
	if _, err := (&AgentService{}).newTweetWriteEnvironment(7); err == nil {
		t.Fatal("environment without adapters was accepted")
	}
	client := &tweetTimelineClientStub{response: &tweetv1.GetUserTimelineResponse{}}
	service := &AgentService{
		runtimeTools: staticRuntimeToolCatalog{tools: []agentRuntime.ToolDefinition{{
			Name:     agentEnvironment.TweetPublishToolName,
			Category: agentRuntime.ToolCategoryWrite, RequiresApproval: true,
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}}},
		tweetWriteStateSource: tweetServiceWriteStateReader{client: client},
	}
	environment, err := service.newTweetWriteEnvironment(7)
	if err != nil {
		t.Fatalf("newTweetWriteEnvironment() error = %v", err)
	}
	if environment.Name() != agentEnvironment.TweetWriteEnvironmentName {
		t.Fatalf("environment name = %q", environment.Name())
	}
}
