package grpc

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
	followv1 "twitter-clone/api/follow/v1"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/module/follow/service"
	"twitter-clone/pkg/logger"
)

type pageRepository struct {
	domain.FollowRepository
	err    error
	cursor uint64
	limit  int
	calls  int
}

func (r *pageRepository) ListFollowerPage(_ context.Context, _ uint64, cursor uint64, limit int) (domain.FollowerPage, error) {
	r.calls++
	r.cursor = cursor
	r.limit = limit
	return domain.FollowerPage{FollowerIDs: []uint64{4, 2}, NextCursor: 8000, HasMore: true}, r.err
}
func (r *pageRepository) ListFolloweePage(_ context.Context, _ uint64, cursor uint64, limit int) (domain.FolloweePage, error) {
	r.calls++
	r.cursor = cursor
	r.limit = limit
	return domain.FolloweePage{FolloweeIDs: []uint64{2, 90}, NextCursor: 8000, HasMore: true}, r.err
}

func TestFollowRPCPreservesRelationPage(t *testing.T) {
	logger.InitLogger()
	repo := &pageRepository{}
	srv := NewFollowServer(service.NewFollowService(repo, nil, nil, nil))
	followers, err := srv.GetFollowers(context.Background(), &followv1.GetFollowersRequest{UserId: 42, Cursor: 9000, Limit: 2})
	if err != nil || followers.NextCursor != 8000 || !followers.HasMore || len(followers.FollowerIds) != 2 || repo.cursor != 9000 || repo.limit != 2 {
		t.Fatalf("followers: %+v %v", followers, err)
	}
	followees, err := srv.GetFollowees(context.Background(), &followv1.GetFolloweesRequest{UserId: 42, Cursor: 9000, Limit: 2})
	if err != nil || followees.NextCursor != 8000 || !followees.HasMore || len(followees.FolloweeIds) != 2 || repo.calls != 2 {
		t.Fatalf("followees: %+v %v", followees, err)
	}
}

func TestFollowRPCPaginationErrors(t *testing.T) {
	logger.InitLogger()
	repo := &pageRepository{}
	srv := NewFollowServer(service.NewFollowService(repo, nil, nil, nil))
	if _, err := srv.GetFollowers(context.Background(), nil); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("nil request: %v", err)
	}
	if _, err := srv.GetFollowees(context.Background(), nil); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("nil request: %v", err)
	}
	if _, err := srv.GetFollowers(context.Background(), &followv1.GetFollowersRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("zero owner: %v", err)
	}
	if _, err := srv.GetFollowees(context.Background(), &followv1.GetFolloweesRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("zero owner: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := srv.GetFollowers(ctx, &followv1.GetFollowersRequest{UserId: 42}); status.Code(err) != codes.Canceled {
		t.Fatalf("canceled: %v", err)
	}
	if repo.calls != 0 {
		t.Fatal("invalid request read database")
	}
	repo.err = errors.New("private SQL detail")
	if _, err := srv.GetFollowees(context.Background(), &followv1.GetFolloweesRequest{UserId: 42, Limit: 2}); status.Code(err) != codes.Internal || status.Convert(err).Message() != "failed to read follow relation page" {
		t.Fatalf("failure detail exposed: %v", err)
	}
	missing := NewFollowServer(service.NewFollowService(nil, nil, nil, nil))
	if _, err := missing.GetFollowers(context.Background(), &followv1.GetFollowersRequest{UserId: 42}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("missing pager: %v", err)
	}
}

func TestFollowPaginationWireFieldCompatibility(t *testing.T) {
	for name, fields := range map[protoreflect.Name]map[protoreflect.Name]protoreflect.FieldNumber{
		"GetFollowersRequest": {"user_id": 1, "cursor": 2, "limit": 3}, "GetFolloweesRequest": {"user_id": 1, "cursor": 2, "limit": 3},
		"GetFollowersResponse": {"follower_ids": 1, "next_cursor": 2, "has_more": 3}, "GetFolloweesResponse": {"followee_ids": 1, "next_cursor": 2, "has_more": 3},
	} {
		message := followv1.File_api_follow_v1_follow_proto.Messages().ByName(name)
		if message == nil || message.Fields().Len() != 3 {
			t.Fatalf("wire message changed: %s", name)
		}
		for fieldName, number := range fields {
			field := message.Fields().ByName(fieldName)
			if field == nil || field.Number() != number {
				t.Fatalf("wire field changed: %s.%s", name, fieldName)
			}
		}
		cursor := message.Fields().ByNumber(2)
		if cursor.Kind() != protoreflect.Uint64Kind {
			t.Fatalf("cursor type changed: %s", name)
		}
	}
}
