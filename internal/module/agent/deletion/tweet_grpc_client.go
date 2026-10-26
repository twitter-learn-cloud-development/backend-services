package deletion

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"

	tweetv1 "twitter-clone/api/tweet/v1"
	"twitter-clone/internal/domain"
)

type tweetSubjectDeletionRPC interface {
	ObserveTweetSubjectData(context.Context, *tweetv1.ObserveTweetSubjectDataRequest, ...grpc.CallOption) (*tweetv1.ObserveTweetSubjectDataResponse, error)
	EnforceTweetSubjectWriteFence(context.Context, *tweetv1.EnforceTweetSubjectWriteFenceRequest, ...grpc.CallOption) (*tweetv1.EnforceTweetSubjectWriteFenceResponse, error)
	DeleteTweetSubjectData(context.Context, *tweetv1.DeleteTweetSubjectDataRequest, ...grpc.CallOption) (*tweetv1.DeleteTweetSubjectDataResponse, error)
}

const tweetServiceWriteFenceAuthority = "tweet-service.mysql"

type TweetSubjectWriteFenceClient interface {
	EnforceSubjectWriteFence(context.Context, WriteFenceRequest) (WriteFenceAuthorityEvidence, error)
}

type TweetSubjectDataClient interface {
	TweetSubjectDeletionClient
	TweetSubjectWriteFenceClient
}

type grpcTweetSubjectDeletionClient struct {
	client tweetSubjectDeletionRPC
	now    func() time.Time
}

func NewGRPCTweetSubjectDeletionClient(client tweetSubjectDeletionRPC) (TweetSubjectDataClient, error) {
	if client == nil {
		return nil, errors.New("tweet subject deletion RPC client is nil")
	}
	return &grpcTweetSubjectDeletionClient{client: client, now: time.Now}, nil
}

func (client *grpcTweetSubjectDeletionClient) EnforceSubjectWriteFence(
	ctx context.Context,
	request WriteFenceRequest,
) (WriteFenceAuthorityEvidence, error) {
	now := client.now().UTC()
	if err := ValidateWriteFenceRequest(request, now); err != nil {
		return WriteFenceAuthorityEvidence{}, err
	}
	response, err := client.client.EnforceTweetSubjectWriteFence(ctx, &tweetv1.EnforceTweetSubjectWriteFenceRequest{
		SchemaVersion: domain.TweetSubjectWriteFenceSchemaV1,
		RequestId:     strings.TrimSpace(request.RequestID), UserId: request.UserID,
		SubjectDigest: request.SubjectDigest, PlanDigest: request.PlanDigest,
		CapabilityDigest: request.CapabilityDigest, GovernanceDigest: request.Governance.Digest,
		DeadlineUnixMs: request.Deadline.UnixMilli(),
	})
	if err != nil {
		return WriteFenceAuthorityEvidence{}, err
	}
	if response == nil || response.SchemaVersion != domain.TweetSubjectWriteFenceSchemaV1 ||
		response.RequestId != strings.TrimSpace(request.RequestID) || response.FenceRevision < 1 ||
		response.EnforcedAtUnixMs <= 0 {
		return WriteFenceAuthorityEvidence{}, errors.New("tweet subject write fence response is invalid")
	}
	enforcedAt := time.UnixMilli(response.EnforcedAtUnixMs).UTC()
	receivedAt := client.now().UTC()
	if enforcedAt.After(receivedAt) {
		return WriteFenceAuthorityEvidence{}, errors.New("tweet subject write fence response time is invalid")
	}
	receiptDigest := sha256Hex(strings.Join([]string{
		"tweet-subject-write-fence-authority-receipt/v1", tweetServiceWriteFenceAuthority,
		response.SchemaVersion, response.RequestId, request.SubjectDigest, request.PlanDigest,
		request.CapabilityDigest, request.Governance.Digest,
		strconv.FormatInt(response.FenceRevision, 10), enforcedAt.Format(time.RFC3339Nano),
	}, "\x00"))
	return WriteFenceAuthorityEvidence{
		Authority: tweetServiceWriteFenceAuthority, ReceiptDigest: receiptDigest,
		FenceRevision: response.FenceRevision, CompletedAt: enforcedAt,
	}, nil
}

func (client *grpcTweetSubjectDeletionClient) ObserveSubjectContent(ctx context.Context, userID uint64) (TweetSubjectInventory, error) {
	if userID == 0 {
		return TweetSubjectInventory{}, errors.New("tweet subject user id is required")
	}
	response, err := client.client.ObserveTweetSubjectData(ctx, &tweetv1.ObserveTweetSubjectDataRequest{UserId: userID})
	if err != nil {
		return TweetSubjectInventory{}, err
	}
	if response == nil || response.Inventory == nil {
		return TweetSubjectInventory{}, errors.New("tweet subject observation response is empty")
	}
	inventory := tweetSubjectInventoryFromProto(response.Inventory)
	if err := ValidateTweetSubjectInventory(inventory); err != nil {
		return TweetSubjectInventory{}, err
	}
	return inventory, nil
}

func (client *grpcTweetSubjectDeletionClient) DeleteSubjectContent(
	ctx context.Context,
	command TweetSubjectDeletionCommand,
) (TweetSubjectDeletionOutcome, error) {
	now := client.now().UTC()
	if err := ValidateTweetSubjectDeletionCommand(command, now); err != nil {
		return TweetSubjectDeletionOutcome{}, err
	}
	response, err := client.client.DeleteTweetSubjectData(ctx, &tweetv1.DeleteTweetSubjectDataRequest{
		SchemaVersion: command.SchemaVersion, OperationKey: command.OperationKey,
		UserId: command.UserID, SubjectDigest: command.SubjectDigest,
		GovernanceDigest: command.GovernanceDigest, DeadlineUnixMs: command.Deadline.UnixMilli(),
	})
	if err != nil {
		return TweetSubjectDeletionOutcome{}, err
	}
	if response == nil || response.Before == nil || response.After == nil {
		return TweetSubjectDeletionOutcome{}, errors.New("tweet subject deletion response is empty")
	}
	outcome := TweetSubjectDeletionOutcome{
		SchemaVersion: response.SchemaVersion, OperationKey: response.OperationKey,
		ReceiptDigest: response.ReceiptDigest,
		Before:        tweetSubjectInventoryFromProto(response.Before),
		After:         tweetSubjectInventoryFromProto(response.After),
		Replayed:      response.Replayed, CompletedAt: time.UnixMilli(response.CompletedAtUnixMs).UTC(),
	}
	if err := ValidateTweetSubjectDeletionOutcome(outcome, command.OperationKey); err != nil {
		return TweetSubjectDeletionOutcome{}, err
	}
	return outcome, nil
}

func tweetSubjectInventoryFromProto(inventory *tweetv1.TweetSubjectInventory) TweetSubjectInventory {
	if inventory == nil {
		return TweetSubjectInventory{}
	}
	return TweetSubjectInventory{
		AuthoredTweets: inventory.AuthoredTweets, AuthoredComments: inventory.AuthoredComments,
		Likes: inventory.Likes, Bookmarks: inventory.Bookmarks,
		Retweets: inventory.Retweets, PollVotes: inventory.PollVotes,
	}
}
