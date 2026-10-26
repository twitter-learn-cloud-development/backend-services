package service

import (
	"errors"

	"twitter-clone/internal/domain"
)

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

	ErrTweetDeletionUnavailable = errors.New("tweet deletion transaction dependencies are unavailable")

	ErrInvalidModerationAction = errors.New("invalid tweet moderation action")

	ErrModerationUnavailable = errors.New("tweet moderation dependencies are unavailable")

	ErrSubjectDeletionInvalid     = errors.New("tweet subject deletion command is invalid")
	ErrSubjectDeletionUnavailable = errors.New("tweet subject deletion dependencies are unavailable")
	ErrSubjectDeletionConflict    = errors.New("tweet subject deletion operation conflicts with a different command")
	ErrSubjectDeletionTooLarge    = errors.New("tweet subject deletion requires a bounded batch protocol")
	ErrSubjectDeletionResiduals   = errors.New("tweet subject deletion left owned records behind")

	ErrSubjectWriteFenced           = domain.ErrTweetSubjectWriteFenced
	ErrSubjectWriteFenceMissing     = domain.ErrTweetSubjectWriteFenceMissing
	ErrSubjectWriteFenceConflict    = domain.ErrTweetSubjectWriteFenceConflict
	ErrSubjectWriteFenceCorrupt     = domain.ErrTweetSubjectWriteFenceCorrupt
	ErrSubjectWriteFenceInvalid     = errors.New("tweet subject write fence command is invalid")
	ErrSubjectWriteFenceUnavailable = errors.New("tweet subject write fence dependencies are unavailable")
)
