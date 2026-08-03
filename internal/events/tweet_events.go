package events

import (
	"encoding/json"
	"errors"
	"fmt"
)

// EventType 事件类型
type EventType string

const (
	// EventTweetCreated 推文创建事件
	EventTweetCreated EventType = "tweet.created"

	// EventTweetDeleted 推文删除事件
	EventTweetDeleted EventType = "tweet.deleted"

	// EventTweetModerated requests asynchronous projection cleanup.
	EventTweetModerated EventType = "tweet.moderated"

	// EventUserFollowed 用户关注事件
	EventUserFollowed EventType = "user.followed"

	// EventUserUnfollowed 用户取关事件
	// EventTweetLiked 推文点赞事件
	EventTweetLiked EventType = "tweet.liked"

	// EventTweetUnliked 推文取消点赞事件
	EventTweetUnliked EventType = "tweet.unliked"

	// EventCommentCreated 评论创建事件
	EventCommentCreated EventType = "comment.created"
)

const (
	OutboxEventTypeTweetModerated = "TWEET_MODERATED"
	TweetModerationSchemaVersion  = 1
	TweetModerationShadowban      = "shadowban"
)

// TweetCreatedEvent 推文创建事件
type TweetCreatedEvent struct {
	TweetID     uint64 `json:"tweet_id"`
	AuthorID    uint64 `json:"author_id"`
	ParentID    uint64 `json:"parent_id"` // 新增
	Content     string `json:"content"`
	Type        int    `json:"type"`
	VisibleType int    `json:"visible_type"` // 新增
	CreatedAt   int64  `json:"created_at"`   // 新增
}

// ToJSON 转换为 JSON
func (e *TweetCreatedEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// TweetDeletedEvent 推文删除事件
type TweetDeletedEvent struct {
	TweetID  uint64 `json:"tweet_id"`
	AuthorID uint64 `json:"author_id"`
}

// ToJSON 转换为 JSON
func (e *TweetDeletedEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// TweetModeratedEvent is content-free by design. Projection consumers only
// need stable identity and the moderation action.
type TweetModeratedEvent struct {
	SchemaVersion    int    `json:"schema_version"`
	EventKey         string `json:"event_key"`
	TweetID          uint64 `json:"tweet_id"`
	AuthorID         uint64 `json:"author_id"`
	Action           string `json:"action"`
	OccurredAtUnixMS int64  `json:"occurred_at_unix_ms"`
}

func NewTweetModeratedEvent(tweetID, authorID uint64, action string, occurredAtUnixMS int64) TweetModeratedEvent {
	return TweetModeratedEvent{
		SchemaVersion:    TweetModerationSchemaVersion,
		EventKey:         TweetModerationEventKey(tweetID, action),
		TweetID:          tweetID,
		AuthorID:         authorID,
		Action:           action,
		OccurredAtUnixMS: occurredAtUnixMS,
	}
}

func TweetModerationEventKey(tweetID uint64, action string) string {
	return fmt.Sprintf("tweet-moderated:v%d:%d:%s", TweetModerationSchemaVersion, tweetID, action)
}

func (e TweetModeratedEvent) Validate() error {
	if e.SchemaVersion != TweetModerationSchemaVersion {
		return fmt.Errorf("unsupported tweet moderation schema version %d", e.SchemaVersion)
	}
	if e.TweetID == 0 || e.AuthorID == 0 {
		return errors.New("tweet_id and author_id are required")
	}
	if e.Action != TweetModerationShadowban {
		return fmt.Errorf("unsupported tweet moderation action %q", e.Action)
	}
	if e.EventKey != TweetModerationEventKey(e.TweetID, e.Action) {
		return errors.New("invalid tweet moderation event key")
	}
	return nil
}

// UserFollowedEvent 用户关注事件
type UserFollowedEvent struct {
	FollowerID uint64 `json:"follower_id"`
	FolloweeID uint64 `json:"followee_id"`
}

// ToJSON 转换为 JSON
func (e *UserFollowedEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// UserUnfollowedEvent 用户取关事件
type UserUnfollowedEvent struct {
	FollowerID uint64 `json:"follower_id"`
	FolloweeID uint64 `json:"followee_id"`
}

// ToJSON 转换为 JSON
func (e *UserUnfollowedEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// TweetLikedEvent 推文点赞事件
type TweetLikedEvent struct {
	TweetID          uint64 `json:"tweet_id"`
	UserID           uint64 `json:"user_id"`
	TweetUser        uint64 `json:"tweet_user_id"`
	OccurredAtUnixMS int64  `json:"occurred_at_unix_ms,omitempty"`
}

// ToJSON 转换为 JSON
func (e *TweetLikedEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// TweetUnlikedEvent 推文取消点赞事件
type TweetUnlikedEvent struct {
	TweetID   uint64 `json:"tweet_id"`
	UserID    uint64 `json:"user_id"`
	TweetUser uint64 `json:"tweet_user_id"`
}

// ToJSON 转换为 JSON
func (e *TweetUnlikedEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// CommentCreatedEvent 评论创建事件
type CommentCreatedEvent struct {
	CommentID        uint64 `json:"comment_id"`
	TweetID          uint64 `json:"tweet_id"`
	UserID           uint64 `json:"user_id"`
	Content          string `json:"content"`
	TweetUser        uint64 `json:"tweet_user_id"`
	ParentID         uint64 `json:"parent_id"`
	OccurredAtUnixMS int64  `json:"occurred_at_unix_ms,omitempty"`
}

// ToJSON 转换为 JSON
func (e *CommentCreatedEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}
