package grpc

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	userv1 "twitter-clone/api/user/v1"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/module/user/service"
)

type passwordRepository struct {
	domain.UserRepository
	user    *domain.User
	writes  int
	loadErr error
}

func (r *passwordRepository) GetByID(context.Context, uint64) (*domain.User, error) {
	return r.user, r.loadErr
}
func (r *passwordRepository) GetByEmail(context.Context, string) (*domain.User, error) {
	return r.user, nil
}
func (r *passwordRepository) UpdatePartial(_ context.Context, id uint64, updates map[string]interface{}) error {
	if id != r.user.ID {
		panic("wrong owner")
	}
	r.writes++
	r.user.PasswordHash = updates["password_hash"].(string)
	return nil
}
func TestPasswordChangeChecksCurrentAndPersistsOnlyHash(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("old-secret"), bcrypt.MinCost)
	require.NoError(t, err)
	repo := &passwordRepository{user: &domain.User{ID: 42, PasswordHash: string(hash)}}
	svc := service.NewUserService(repo)
	server := NewUserServer(svc)
	for _, tc := range []struct {
		old, password string
		code          codes.Code
	}{{"wrong", "new-secret", codes.PermissionDenied}, {"old-secret", "123", codes.InvalidArgument}, {"old-secret", strings.Repeat("密", 25), codes.InvalidArgument}} {
		_, err = server.ChangePassword(context.Background(), &userv1.ChangePasswordRequest{UserId: 42, OldPassword: tc.old, NewPassword: tc.password})
		require.Equal(t, tc.code, status.Code(err))
		require.Zero(t, repo.writes)
	}
	_, err = server.ChangePassword(context.Background(), &userv1.ChangePasswordRequest{UserId: 42, OldPassword: "old-secret", NewPassword: "new-secret"})
	require.NoError(t, err)
	require.Equal(t, 1, repo.writes)
	require.NotEqual(t, "new-secret", repo.user.PasswordHash)
	_, err = svc.Login(context.Background(), "user@example.com", "old-secret")
	require.ErrorIs(t, err, service.ErrInvalidCredentials)
	_, err = svc.Login(context.Background(), "user@example.com", "new-secret")
	require.NoError(t, err)
}

func TestPasswordOwnerLookupDoesNotMaskDatabaseFailuresAsMissingUsers(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code codes.Code
	}{
		{domain.ErrUserNotFound, codes.NotFound},
		{errors.New("private database connection details"), codes.Internal},
	} {
		repo := &passwordRepository{loadErr: tc.err}
		server := NewUserServer(service.NewUserService(repo))
		_, err := server.ChangePassword(context.Background(), &userv1.ChangePasswordRequest{UserId: 42, OldPassword: "old-secret", NewPassword: "new-secret"})
		require.Equal(t, tc.code, status.Code(err))
		require.NotContains(t, err.Error(), "private database")
		require.Zero(t, repo.writes)
	}
}
