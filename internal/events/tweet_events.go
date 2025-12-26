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
	EventUserUnfollowed EventType = "user.unfollowed"
)

// TweetCreatedEvent 推文创建事件
type TweetCreatedEvent struct {
	TweetID  uint64 `test_data:"tweet_id"`
	AuthorID uint64 `test_data:"author_id"`
	Content  string `test_data:"content"`
	Type     int    `test_data:"type"`
}

// ToJSON 转换为 JSON
func (e *TweetCreatedEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// TweetDeletedEvent 推文删除事件
type TweetDeletedEvent struct {
	TweetID  uint64 `test_data:"tweet_id"`
	AuthorID uint64 `test_data:"author_id"`
}

// ToJSON 转换为 JSON
func (e *TweetDeletedEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// UserFollowedEvent 用户关注事件
type UserFollowedEvent struct {
	FollowerID uint64 `test_data:"follower_id"`
	FolloweeID uint64 `test_data:"followee_id"`
}

// ToJSON 转换为 JSON
func (e *UserFollowedEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// UserUnfollowedEvent 用户取关事件
type UserUnfollowedEvent struct {
	FollowerID uint64 `test_data:"follower_id"`
	FolloweeID uint64 `test_data:"followee_id"`
}

// ToJSON 转换为 JSON
func (e *UserUnfollowedEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}
