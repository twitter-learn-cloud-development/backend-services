package tools

import (
	"context"
	"testing"

	tweetv1 "twitter-clone/api/tweet/v1"
	externalmcp "twitter-clone/internal/module/agent/mcp/remote"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"google.golang.org/grpc"
)

type createTweetClientStub struct {
	tweetv1.TweetServiceClient
	request *tweetv1.CreateTweetRequest
}

func (stub *createTweetClientStub) CreateTweet(
	_ context.Context,
	request *tweetv1.CreateTweetRequest,
	_ ...grpc.CallOption,
) (*tweetv1.CreateTweetResponse, error) {
	stub.request = request
	return &tweetv1.CreateTweetResponse{Tweet: &tweetv1.Tweet{
		Id: request.UserId + 100, UserId: request.UserId, Content: request.Content,
	}}, nil
}

func TestCreateTweetRequiresAndForwardsIdempotencyKey(t *testing.T) {
	client := &createTweetClientStub{}
	mcpServer := server.NewMCPServer("test", "v1")
	RegisterCreateTweet(mcpServer, client)
	registered := mcpServer.GetTool("create_tweet")
	if registered == nil {
		t.Fatal("create_tweet was not registered")
	}
	if registered.Tool.Meta.AdditionalFields[externalmcp.IdempotencyKeyArgumentMetaField] != "idempotency_key" {
		t.Fatalf("tool metadata = %+v", registered.Tool.Meta.AdditionalFields)
	}

	result, err := registered.Handler(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: "create_tweet",
		Arguments: map[string]any{
			"user_id": "7",
			"content": "bounded content",
		},
	}})
	if err != nil {
		t.Fatalf("Handler(missing key) error = %v", err)
	}
	if !result.IsError || client.request != nil {
		t.Fatalf("missing key result/request = %+v/%+v", result, client.request)
	}

	result, err = registered.Handler(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: "create_tweet",
		Arguments: map[string]any{
			"user_id":         "7",
			"content":         "bounded content",
			"idempotency_key": "run-1:step-1:create_tweet",
		},
	}})
	if err != nil || result.IsError {
		t.Fatalf("Handler() result/error = %+v/%v", result, err)
	}
	if client.request == nil || client.request.UserId != 7 ||
		client.request.Content != "bounded content" ||
		client.request.IdempotencyKey != "run-1:step-1:create_tweet" {
		t.Fatalf("CreateTweet request = %+v", client.request)
	}
}
