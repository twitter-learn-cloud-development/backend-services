package events

import "encoding/json"

// EventType 事件类型
type EventType string

const (
	// EventTweetCreated 推文创建事件
	EventTweetCreated EventType = "tweet.created"

	// EventTweetDeleted 推文删除事件
	EventTweetDeleted EventType = "tweet.deleted"

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

// TweetCreatedEvent 推文创建事件
type TweetCreatedEvent struct {
	TweetID  uint64 `json:"tweet_id"`
	AuthorID uint64 `json:"author_id"`
	Content  string `json:"content"`
	Type     int    `json:"type"`
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
	TweetID   uint64 `json:"tweet_id"`
	UserID    uint64 `json:"user_id"`
	TweetUser uint64 `json:"tweet_user_id"`
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
	CommentID uint64 `json:"comment_id"`
	TweetID   uint64 `json:"tweet_id"`
	UserID    uint64 `json:"user_id"`
	Content   string `json:"content"`
	TweetUser uint64 `json:"tweet_user_id"`
	ParentID  uint64 `json:"parent_id"`
}

// ToJSON 转换为 JSON
func (e *CommentCreatedEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}
