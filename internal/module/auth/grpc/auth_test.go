package grpc

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	authv1 "twitter-clone/api/auth/v1"
	userv1 "twitter-clone/api/user/v1"
)

type passwordUserClient struct {
	userv1.UserServiceClient
	request *userv1.ChangePasswordRequest
	err     error
}

func (f *passwordUserClient) ChangePassword(_ context.Context, req *userv1.ChangePasswordRequest, _ ...grpc.CallOption) (*userv1.ChangePasswordResponse, error) {
	f.request = req
	return &userv1.ChangePasswordResponse{Message: "changed"}, f.err
}
func TestLocalLogoutAndPasswordForwarding(t *testing.T) {
	client := &passwordUserClient{err: status.Error(codes.PermissionDenied, "incorrect current password")}
	svc := &AuthService{userClient: client}
	_, err := svc.Logout(context.Background(), nil)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	receipt, err := svc.Logout(context.Background(), &authv1.LogoutRequest{UserId: 42})
	require.NoError(t, err)
	require.Contains(t, receipt.Message, "not revoked")
	for _, password := range []string{"123", strings.Repeat("密", 25)} {
		_, err = svc.ChangePassword(context.Background(), &authv1.ChangePasswordRequest{UserId: 42, OldPassword: "old-secret", NewPassword: password})
		require.Equal(t, codes.InvalidArgument, status.Code(err))
		require.Nil(t, client.request)
	}
	_, err = svc.ChangePassword(context.Background(), &authv1.ChangePasswordRequest{UserId: 42, OldPassword: "old-secret", NewPassword: "new-secret"})
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	require.Equal(t, uint64(42), client.request.UserId)
	require.Equal(t, "new-secret", client.request.NewPassword)
}
