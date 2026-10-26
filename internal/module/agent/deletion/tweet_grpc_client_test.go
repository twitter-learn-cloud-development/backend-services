package deletion

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"

	tweetv1 "twitter-clone/api/tweet/v1"
	"twitter-clone/internal/domain"
)

type tweetSubjectDeletionRPCFake struct {
	observeResponse *tweetv1.ObserveTweetSubjectDataResponse
	fenceResponse   *tweetv1.EnforceTweetSubjectWriteFenceResponse
	fenceRequest    *tweetv1.EnforceTweetSubjectWriteFenceRequest
	deleteResponse  *tweetv1.DeleteTweetSubjectDataResponse
	deleteRequest   *tweetv1.DeleteTweetSubjectDataRequest
}

func (fake *tweetSubjectDeletionRPCFake) ObserveTweetSubjectData(context.Context, *tweetv1.ObserveTweetSubjectDataRequest, ...grpc.CallOption) (*tweetv1.ObserveTweetSubjectDataResponse, error) {
	return fake.observeResponse, nil
}

func (fake *tweetSubjectDeletionRPCFake) EnforceTweetSubjectWriteFence(_ context.Context, request *tweetv1.EnforceTweetSubjectWriteFenceRequest, _ ...grpc.CallOption) (*tweetv1.EnforceTweetSubjectWriteFenceResponse, error) {
	fake.fenceRequest = request
	return fake.fenceResponse, nil
}

func (fake *tweetSubjectDeletionRPCFake) DeleteTweetSubjectData(_ context.Context, request *tweetv1.DeleteTweetSubjectDataRequest, _ ...grpc.CallOption) (*tweetv1.DeleteTweetSubjectDataResponse, error) {
	fake.deleteRequest = request
	return fake.deleteResponse, nil
}

func TestGRPCTweetSubjectDeletionClientValidatesRoundTrip(t *testing.T) {
	now := time.Date(2026, 9, 25, 8, 0, 0, 0, time.UTC)
	command := testTweetSubjectDeletionCommand(now)
	before := TweetSubjectInventory{AuthoredTweets: 2, Likes: 3, PollVotes: 1}
	outcome, err := NewTweetSubjectDeletionOutcome(command.OperationKey, before, TweetSubjectInventory{}, now)
	if err != nil {
		t.Fatal(err)
	}
	fake := &tweetSubjectDeletionRPCFake{
		observeResponse: &tweetv1.ObserveTweetSubjectDataResponse{Inventory: inventoryToTestProto(before)},
		deleteResponse: &tweetv1.DeleteTweetSubjectDataResponse{
			SchemaVersion: outcome.SchemaVersion, OperationKey: outcome.OperationKey,
			ReceiptDigest: outcome.ReceiptDigest, Before: inventoryToTestProto(outcome.Before),
			After: inventoryToTestProto(outcome.After), Replayed: true,
			CompletedAtUnixMs: outcome.CompletedAt.UnixMilli(),
		},
	}
	adapterValue, err := NewGRPCTweetSubjectDeletionClient(fake)
	if err != nil {
		t.Fatal(err)
	}
	adapter := adapterValue.(*grpcTweetSubjectDeletionClient)
	adapter.now = func() time.Time { return now }

	observed, err := adapter.ObserveSubjectContent(context.Background(), command.UserID)
	if err != nil || observed != before {
		t.Fatalf("observation = %+v, err = %v", observed, err)
	}
	result, err := adapter.DeleteSubjectContent(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Replayed || result.ReceiptDigest != outcome.ReceiptDigest {
		t.Fatalf("unexpected deletion result: %+v", result)
	}
	if fake.deleteRequest == nil || fake.deleteRequest.UserId != command.UserID ||
		fake.deleteRequest.GovernanceDigest != command.GovernanceDigest {
		t.Fatalf("unexpected RPC request: %+v", fake.deleteRequest)
	}
}

func TestGRPCTweetSubjectDeletionClientRejectsMalformedReceipt(t *testing.T) {
	now := time.Date(2026, 9, 25, 8, 0, 0, 0, time.UTC)
	command := testTweetSubjectDeletionCommand(now)
	fake := &tweetSubjectDeletionRPCFake{deleteResponse: &tweetv1.DeleteTweetSubjectDataResponse{
		SchemaVersion: TweetSubjectDeletionContractV1, OperationKey: command.OperationKey,
		ReceiptDigest: sha256Hex("tampered"), Before: inventoryToTestProto(TweetSubjectInventory{}),
		After: inventoryToTestProto(TweetSubjectInventory{}), CompletedAtUnixMs: now.UnixMilli(),
	}}
	adapterValue, err := NewGRPCTweetSubjectDeletionClient(fake)
	if err != nil {
		t.Fatal(err)
	}
	adapter := adapterValue.(*grpcTweetSubjectDeletionClient)
	adapter.now = func() time.Time { return now }
	if _, err := adapter.DeleteSubjectContent(context.Background(), command); err == nil {
		t.Fatal("malformed Tweet service receipt was accepted")
	}
}

func TestGRPCTweetSubjectDeletionClientEnforcesWriteFence(t *testing.T) {
	now := time.Date(2026, 9, 26, 8, 0, 0, 0, time.UTC)
	governance, err := NewGovernanceDecision(
		42, GovernanceClear, 3, sha256Hex("clear"), now, now.Add(time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	request := WriteFenceRequest{
		RequestID: "request-1", UserID: 42, SubjectDigest: DeletionSubjectDigest(42),
		PlanDigest: sha256Hex("plan"), CapabilityDigest: sha256Hex("capabilities"),
		Governance: governance, Deadline: now.Add(time.Minute),
	}
	fake := &tweetSubjectDeletionRPCFake{fenceResponse: &tweetv1.EnforceTweetSubjectWriteFenceResponse{
		SchemaVersion: domain.TweetSubjectWriteFenceSchemaV1, RequestId: request.RequestID,
		FenceRevision: 4, Replayed: true, EnforcedAtUnixMs: now.UnixMilli(),
	}}
	clientValue, err := NewGRPCTweetSubjectDeletionClient(fake)
	if err != nil {
		t.Fatal(err)
	}
	client := clientValue.(*grpcTweetSubjectDeletionClient)
	client.now = func() time.Time { return now.Add(time.Second) }
	evidence, err := client.EnforceSubjectWriteFence(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Authority != tweetServiceWriteFenceAuthority || evidence.FenceRevision != 4 ||
		evidence.ReceiptDigest == "" || fake.fenceRequest == nil ||
		fake.fenceRequest.GovernanceDigest != request.Governance.Digest {
		t.Fatalf("unexpected fence evidence=%+v request=%+v", evidence, fake.fenceRequest)
	}
}

func TestNewGRPCTweetSubjectDeletionClientRejectsNil(t *testing.T) {
	if _, err := NewGRPCTweetSubjectDeletionClient(nil); err == nil {
		t.Fatal("nil Tweet service RPC client was accepted")
	}
}

func testTweetSubjectDeletionCommand(now time.Time) TweetSubjectDeletionCommand {
	const userID uint64 = 42
	return TweetSubjectDeletionCommand{
		SchemaVersion: TweetSubjectDeletionContractV1,
		OperationKey:  sha256Hex("operation"), SubjectDigest: DeletionSubjectDigest(userID),
		UserID: userID, GovernanceDigest: sha256Hex("governance"), Deadline: now.Add(time.Minute),
	}
}

func inventoryToTestProto(inventory TweetSubjectInventory) *tweetv1.TweetSubjectInventory {
	return &tweetv1.TweetSubjectInventory{
		AuthoredTweets: inventory.AuthoredTweets, AuthoredComments: inventory.AuthoredComments,
		Likes: inventory.Likes, Bookmarks: inventory.Bookmarks,
		Retweets: inventory.Retweets, PollVotes: inventory.PollVotes,
	}
}
