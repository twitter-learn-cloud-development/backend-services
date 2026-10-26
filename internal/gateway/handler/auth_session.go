package handler

import (
	"context"
	"net/http"
	"time"

	authv1 "twitter-clone/api/auth/v1"
	"twitter-clone/internal/gateway/middleware"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (h *UserHandler) AuthSession(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"logout_scope": "local", "revocation_supported": false, "password_change_revokes_tokens": false})
}

func (h *UserHandler) Logout(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok || userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req struct {
		Scope string `json:"scope" binding:"omitempty,oneof=local all"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "scope must be local or all"})
		return
	}
	if req.Scope == "all" {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "session revocation is not supported", "revocation_supported": false})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	if _, err := h.authClient.Logout(ctx, &authv1.LogoutRequest{UserId: userID}); err != nil {
		authSessionError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"scope": "local", "token_revoked": false, "revocation_supported": false, "message": "clear local session; issued tokens remain valid until expiry"})
}

func (h *UserHandler) ChangePassword(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok || userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req struct {
		OldPassword string `json:"old_password" binding:"required"`
		NewPassword string `json:"new_password" binding:"required"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	if err := c.ShouldBindJSON(&req); err != nil || len(req.NewPassword) < 6 || len(req.NewPassword) > 72 || len(req.OldPassword) > 72 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password must contain 6 to 72 UTF-8 bytes"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	if _, err := h.authClient.ChangePassword(ctx, &authv1.ChangePasswordRequest{UserId: userID, OldPassword: req.OldPassword, NewPassword: req.NewPassword}); err != nil {
		authSessionError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "password changed; sign in again on this client", "reauthenticate": true, "tokens_revoked": false})
}

func authSessionError(c *gin.Context, err error) {
	code, message := http.StatusServiceUnavailable, "authentication service unavailable"
	switch status.Code(err) {
	case codes.InvalidArgument:
		code, message = http.StatusBadRequest, "invalid password change request"
	case codes.PermissionDenied:
		code, message = http.StatusForbidden, "incorrect current password"
	case codes.Unauthenticated:
		code, message = http.StatusUnauthorized, "unauthorized"
	case codes.NotFound:
		code, message = http.StatusNotFound, "user not found"
	case codes.DeadlineExceeded:
		code, message = http.StatusGatewayTimeout, "authentication request timed out; outcome may be unknown"
	case codes.Unimplemented:
		code, message = http.StatusNotImplemented, "authentication operation is not supported"
	}
	c.JSON(code, gin.H{"error": message})
}
