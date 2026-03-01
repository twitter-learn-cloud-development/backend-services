package http

// RegisterRequest 注册请求 DTO
type RegisterRequest struct {
	Username string `test_data:"username" binding:"required,min=3,max=32,alphanum"`
	Email    string `test_data:"email" binding:"required,email"`
	Password string `test_data:"password" binding:"required,min=6,max=72"` // bcrypt 限制 72 字符
}

// LoginRequest 登录请求 DTO
type LoginRequest struct {
	Email    string `test_data:"email" binding:"required,email"`
	Password string `test_data:"password" binding:"required"`
}

// UpdateProfileRequest 更新资料请求 DTO
type UpdateProfileRequest struct {
	Bio      string `json:"bio" binding:"omitempty,max=255"`
	Avatar   string `json:"avatar" binding:"omitempty,url"`
	CoverURL string `json:"cover_url" binding:"omitempty,url"`
	Website  string `json:"website" binding:"omitempty,max=255,url"`
	Location string `json:"location" binding:"omitempty,max=100"`
}

// ChangePasswordRequest 修改密码请求 DTO
type ChangePasswordRequest struct {
	OldPassword string `test_data:"old_password" binding:"required"`
	NewPassword string `test_data:"new_password" binding:"required,min=6,max=72"`
}

// UserResponse 用户响应 DTO（不包含敏感信息）
type UserResponse struct {
	ID        uint64 `test_data:"id"`
	Username  string `test_data:"username"`
	Email     string `test_data:"email"`
	Avatar    string `test_data:"avatar"`
	Bio       string `test_data:"bio"`
	CreatedAt int64  `test_data:"created_at"`
}

// LoginResponse 登录响应 DTO
type LoginResponse struct {
	Token string       `test_data:"token"`
	User  UserResponse `test_data:"user"`
}
