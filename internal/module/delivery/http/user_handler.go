package http

import (
	"errors"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/module/user/service"

	"github.com/gin-gonic/gin"
)

// UserHandler 用户 HTTP 处理器
type UserHandler struct {
	svc *service.UserService
}

// NewUserHandler 创建用户处理器
func NewUserHandler(svc *service.UserService) *UserHandler {
	return &UserHandler{
		svc: svc,
	}
}

// Register 用户注册
// @Summary 用户注册
// @Tags User
// @Accept test_data
// @Produce test_data
// @Param request body RegisterRequest true "注册信息"
// @Success 201 {object} Response{data=UserResponse}
// @Failure 400 {object} Response
// @Failure 409 {object} Response
// @Failure 500 {object} Response
// @Router /api/v1/users/register [post]
func (h *UserHandler) Register(c *gin.Context) {
	//1.绑定并校验参数
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ValidationErrorResponse(c, err)
		return
	}

	//2.调用Service层
	user, err := h.svc.Register(c.Request.Context(), req.Username, req.Email, req.Password)
	if err != nil {
		h.handleServiceError(c, err)
		return
	}

	//3. 转换为响应DTO
	resp := h.toUserResponse(user)

	//4. 返回成功响应
	CreatedResponse(c, resp)
}

// Login 用户登录
// @Summary 用户登录
// @Tags User
// @Accept test_data
// @Produce test_data
// @Param request body LoginRequest true "登录信息"
// @Success 200 {object} Response{data=LoginResponse}
// @Failure 400 {object} Response
// @Failure 401 {object} Response
// @Failure 500 {object} Response
// @Router /api/v1/users/login [post]
func (h *UserHandler) Login(c *gin.Context) {
	//1.绑定并校验参数
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ValidationErrorResponse(c, err)
		return
	}

	//2.调用service层
	token, user, err := h.svc.Login(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		h.handleServiceError(c, err)
		return
	}

	//3.构建响应
	resp := LoginResponse{
		Token: token,
		User:  h.toUserResponse(user),
	}

	//4.返回响应
	SuccessResponse(c, resp)

}

// GetProfile 获取用户资料
// @Summary 获取用户资料
// @Tags User
// @Produce test_data
// @Param id path int true "用户ID"
// @Success 200 {object} Response{data=UserResponse}
// @Failure 404 {object} Response
// @Failure 500 {object} Response
// @Router /api/v1/users/{id} [get]
func (h *UserHandler) GetProfile(c *gin.Context) {
	//1.获取路径参数
	userID, err := h.parseUserID(c)
	if err != nil {
		BadRequestResponse(c, "invalid user id")
		return
	}

	//2.调用Service
	user, err := h.svc.GetProfile(c.Request.Context(), userID)
	if err != nil {
		h.handleServiceError(c, err)
		return
	}

	//3.返回响应
	resp := h.toUserResponse(user)
	SuccessResponse(c, resp)
}

// UpdateProfile 更新用户资料
// @Summary 更新用户资料
// @Tags User
// @Accept test_data
// @Produce test_data
// @Security BearerAuth
// @Param request body UpdateProfileRequest true "更新信息"
// @Success 200 {object} Response
// @Failure 400 {object} Response
// @Failure 401 {object} Response
// @Failure 500 {object} Response
// @Router /api/v1/users/profile [put]
func (h *UserHandler) UpdateProfile(c *gin.Context) {
	// 1. 从上下文获取当前用户ID（由认证中间件设置）
	userID, exists := c.Get("user_id")
	if !exists {
		UnauthorizedResponse(c, "unauthorized")
		return
	}

	//2.绑定请求
	var req UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ValidationErrorResponse(c, err)
		return
	}

	//3.调用Service
	//3.调用Service
	err := h.svc.UpdateProfile(c.Request.Context(), userID.(uint64), req.Bio, req.Avatar, req.CoverURL, req.Website, req.Location)
	if err != nil {
		h.handleServiceError(c, err)
		return
	}

	//4.返回成功
	SuccessResponse(c, gin.H{"message": "profile updated"})
}

// ChangePassword 修改密码
// @Summary 修改密码
// @Tags User
// @Accept test_data
// @Produce test_data
// @Security BearerAuth
// @Param request body ChangePasswordRequest true "密码信息"
// @Success 200 {object} Response
// @Failure 400 {object} Response
// @Failure 401 {object} Response
// @Failure 500 {object} Response
// @Router /api/v1/users/password [put]
func (h *UserHandler) ChangePassword(c *gin.Context) {
	//1.获取当前用户ID
	userID, exists := c.Get("user_id")
	if !exists {
		UnauthorizedResponse(c, "unauthorized")
		return
	}

	//2.绑定请求
	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ValidationErrorResponse(c, err)
		return
	}

	//3.调用Service
	err := h.svc.ChangePassword(c.Request.Context(), userID.(uint64), req.OldPassword, req.NewPassword)
	if err != nil {
		h.handleServiceError(c, err)
		return
	}

	//4.返回成功
	SuccessResponse(c, gin.H{"message": "password changed"})
}

// toUserResponse 转换为响应DTO
func (h *UserHandler) toUserResponse(user *domain.User) UserResponse {
	return UserResponse{
		ID:        user.ID,
		Username:  user.Username,
		Email:     user.Email,
		Avatar:    user.Avatar,
		Bio:       user.Bio,
		CreatedAt: user.CreatedAt,
	}
}

// parseUserID 解析路径中的用户ID
func (h *UserHandler) parseUserID(c *gin.Context) (uint64, error) {
	var req struct {
		ID uint64 `uri:"id" binding:"required,min=1"`
	}

	if err := c.ShouldBindUri(&req); err != nil {
		return 0, err
	}
	return req.ID, nil

}

// handleServiceError 处理 Service 层错误
func (h *UserHandler) handleServiceError(c *gin.Context, err error) {
	// 根据错误类型返回不同的 HTTP 状态码
	switch {
	case errors.Is(err, service.ErrUserAlreadyExists):
		ConflictResponse(c, "user already exists")
	case errors.Is(err, service.ErrInvalidCredentials):
		UnauthorizedResponse(c, "invalid email or password")
	case errors.Is(err, service.ErrUserNotFound):
		NotFoundResponse(c, "user not found")
	case errors.Is(err, service.ErrInvalidPassword):
		BadRequestResponse(c, err.Error())
	case errors.Is(err, service.ErrInvalidEmail):
		BadRequestResponse(c, err.Error())
	case errors.Is(err, service.ErrInvalidUsername):
		BadRequestResponse(c, err.Error())
	default:
		// 未知错误，记录日志（生产环境）
		// log.Error("service error", zap.Error(err))
		InternalServerErrorResponse(c, "internal server error")
	}
}
