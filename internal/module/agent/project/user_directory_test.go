package project

import (
	"context"
	"errors"
	"testing"

	userv1 "twitter-clone/api/user/v1"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type userDirectoryClientFake struct {
	userv1.UserServiceClient
	request *userv1.GetBatchUsersRequest
	users   []*userv1.User
	err     error
}

func (fake *userDirectoryClientFake) GetBatchUsers(
	_ context.Context,
	request *userv1.GetBatchUsersRequest,
	_ ...grpc.CallOption,
) (*userv1.GetBatchUsersResponse, error) {
	fake.request = request
	if fake.err != nil {
		return nil, fake.err
	}
	return &userv1.GetBatchUsersResponse{Users: fake.users}, nil
}

func TestGRPCUserDirectoryRequiresExactUserServiceMatch(t *testing.T) {
	client := &userDirectoryClientFake{users: []*userv1.User{{Id: 41}, {Id: 42}}}
	directory := NewGRPCUserDirectory(client)

	exists, err := directory.UserExists(context.Background(), 42)

	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, []uint64{42}, client.request.UserIds)
}

func TestGRPCUserDirectoryRejectsMissingUserAndFailsClosedOnServiceError(t *testing.T) {
	directory := NewGRPCUserDirectory(&userDirectoryClientFake{users: []*userv1.User{{Id: 41}}})
	exists, err := directory.UserExists(context.Background(), 42)
	require.NoError(t, err)
	require.False(t, exists)

	directory = NewGRPCUserDirectory(&userDirectoryClientFake{err: errors.New("service unavailable")})
	exists, err = directory.UserExists(context.Background(), 42)
	require.ErrorContains(t, err, "query User Service")
	require.False(t, exists)
}
