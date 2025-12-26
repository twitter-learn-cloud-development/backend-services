package service

import "errors"

// 推文业务错误
var (
	// ErrTweetNotFound 推文不存在
	ErrTweetNotFound = errors.New("tweet not found")

	// ErrInvalidContent 内容无效
	ErrInvalidContent = errors.New("tweet content cannot be empty")

	// ErrContentTooLong 内容过长
	ErrContentTooLong = errors.New("tweet content too long (max 280 characters)")

	// ErrUnauthorized 无权限操作
	ErrUnauthorized = errors.New("unauthorized to perform this action")

	// ErrInvalidMediaURL 无效的媒体 URL
	ErrInvalidMediaURL = errors.New("invalid media url")

	// ErrTooManyMedia 媒体过多
	ErrTooManyMedia = errors.New("too many media (max 4)")
)
