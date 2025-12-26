package service

import "errors"

// 业务错误定义
// 这些错误用于区分业务逻辑错误和系统错误
// Controller 层可以根据这些错误返回不同的 HTTP 状态码
var (
	// ErrUserAlreadyExists 用户已存在（邮箱或用户名重复）
	ErrUserAlreadyExists = errors.New("user already exists")

	// ErrInvalidCredentials 无效的凭证（邮箱或密码错误）
	ErrInvalidCredentials = errors.New("invalid email or password")

	// ErrUserNotFound 用户不存在
	ErrUserNotFound = errors.New("user not found")

	// ErrInvalidPassword 密码格式无效
	ErrInvalidPassword = errors.New("password must be at least 6 characters")

	// ErrInvalidEmail 邮箱格式无效
	ErrInvalidEmail = errors.New("invalid email format")

	// ErrInvalidUsername 用户名格式无效
	ErrInvalidUsername = errors.New("username must be 3-32 characters, alphanumeric only")
)
