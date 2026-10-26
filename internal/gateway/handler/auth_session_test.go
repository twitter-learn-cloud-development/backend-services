package handler

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	authv1 "twitter-clone/api/auth/v1"
)

type sessionAuthClient struct {
	authv1.AuthServiceClient
	called int
	err    error
}

func (f *sessionAuthClient) check(ctx context.Context, id uint64) {
	f.called++
	if id != 42 {
		panic("untrusted identity forwarded")
	}
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > 5*time.Second {
		panic("missing bounded timeout")
	}
}
func (f *sessionAuthClient) Logout(ctx context.Context, req *authv1.LogoutRequest, _ ...grpc.CallOption) (*authv1.LogoutResponse, error) {
	f.check(ctx, req.UserId)
	return &authv1.LogoutResponse{}, f.err
}
func (f *sessionAuthClient) ChangePassword(ctx context.Context, req *authv1.ChangePasswordRequest, _ ...grpc.CallOption) (*authv1.ChangePasswordResponse, error) {
	f.check(ctx, req.UserId)
	return &authv1.ChangePasswordResponse{}, f.err
}

func TestAuthSessionContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, path, body string
		id               bool
		err              error
		want, calls      int
		contains         string
	}{
		{"local logout", "/logout", `{"scope":"local","user_id":999}`, true, nil, 200, 1, `"token_revoked":false`},
		{"default local", "/logout", `{}`, true, nil, 200, 1, `"scope":"local"`},
		{"all unsupported", "/logout", `{"scope":"all"}`, true, nil, 501, 0, `false`},
		{"bad scope", "/logout", `{"scope":"unknown"}`, true, nil, 400, 0, `error`},
		{"unauthenticated", "/logout", `{}`, false, nil, 401, 0, `unauthorized`},
		{"password", "/password", `{"user_id":999,"old_password":"old-secret","new_password":"new-secret"}`, true, nil, 200, 1, `"tokens_revoked":false`},
		{"short password", "/password", `{"old_password":"old-secret","new_password":"123"}`, true, nil, 400, 0, `error`},
		{"byte limit", "/password", `{"old_password":"old-secret","new_password":"` + strings.Repeat("密", 25) + `"}`, true, nil, 400, 0, `error`},
		{"wrong password", "/password", `{"old_password":"wrong","new_password":"new-secret"}`, true, status.Error(codes.PermissionDenied, "sensitive downstream details"), 403, 1, `incorrect current password`},
		{"unavailable", "/logout", `{}`, true, status.Error(codes.Unavailable, "secret DSN"), 503, 1, `unavailable`},
		{"uncertain timeout", "/password", `{"old_password":"old-secret","new_password":"new-secret"}`, true, status.Error(codes.DeadlineExceeded, "secret DSN"), 504, 1, `outcome may be unknown`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &sessionAuthClient{err: tc.err}
			h := NewUserHandler(nil, nil, nil, client)
			r := gin.New()
			r.Use(func(c *gin.Context) {
				if tc.id {
					c.Set("user_id", uint64(42))
				}
			})
			r.POST("/logout", h.Logout)
			r.POST("/password", h.ChangePassword)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body)))
			require.Equal(t, tc.want, w.Code, w.Body.String())
			require.Equal(t, tc.calls, client.called)
			require.Contains(t, w.Body.String(), tc.contains)
			require.NotContains(t, w.Body.String(), "secret DSN")
			require.NotContains(t, w.Body.String(), "sensitive downstream")
		})
	}
}
