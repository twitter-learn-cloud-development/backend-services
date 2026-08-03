package service

import "errors"

// 推文业务错误
var (
	// ErrTweetNotFound 推文不存在
	ErrTweetNotFound = errors.New("tweet not found")

	// ErrInvalidContent 内容无效
	ErrInvalidContent = errors.New("tweet content cannot be empty")

	// ErrContentTooLong 内容过长
	ErrContentTooLong = errors.New("tweet content too long")

	// ErrUnauthorized 无权限操作
	ErrUnauthorized = errors.New("unauthorized to perform this action")

	// ErrInvalidMediaURL 无效的媒体 URL
	ErrInvalidMediaURL = errors.New("invalid media url")

	// ErrTooManyMedia 媒体过多
	ErrTooManyMedia = errors.New("too many media (max 4)")

	// ErrInvalidIdempotencyKey 幂等键格式无效
	ErrInvalidIdempotencyKey = errors.New("invalid tweet idempotency key")

	// ErrIdempotencyConflict 同一幂等键被用于不同的发布输入
	ErrIdempotencyConflict = errors.New("tweet idempotency key conflicts with a different request")

	// ErrIdempotencyUnavailable 调用方要求幂等，但服务未配置持久化仓储
	ErrIdempotencyUnavailable = errors.New("tweet idempotency repository is unavailable")

	ErrInvalidAuthorID = errors.New("invalid author id")

	ErrInvalidTweetID = errors.New("invalid tweet id")

	ErrInvalidModerationAction = errors.New("invalid tweet moderation action")

	ErrModerationUnavailable = errors.New("tweet moderation dependencies are unavailable")
)
