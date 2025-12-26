package domain

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// TweetType帖子类型枚举
const (
	TweetTypeText  = 0 //纯文本
	TweetTypeImage = 1 //含图片
	TweetTypeVideo = 2 //含视频
)

// VisibleType可见性枚举
const (
	VisiblePublic  = 0 //公开
	VisibleFollows = 1 //仅粉丝可见
	VisiblePrivate = 2 //金自己可见
)

// MediaURLs 自定义类型，用于 GORM 处理 JSON 字段
type MediaURLs []string

// Value 实现 driver.Valuer 接口 (存入数据库时转 JSON)
func (m MediaURLs) Value() (driver.Value, error) {
	if m == nil {
		// 返回空 JSON 数组的字符串
		return "[]", nil
	}
	// 将 []string 序列化为 JSON 字符串
	jsonBytes, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	// 返回 string 类型（不是 []byte）
	return string(jsonBytes), nil
}

// Scan 实现 sql.Scanner 接口 (从数据库取出时转 Struct)
func (m *MediaURLs) Scan(value interface{}) error {
	if value == nil {
		*m = []string{}
		return nil
	}

	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("unsupported type: %T", value)
	}

	return json.Unmarshal(bytes, m)
}

// Tweet 推文实体
type Tweet struct {
	ID          uint64    `gorm:"primaryKey;column:id;comment:主键ID (Snowflake)" test_data:"id"`
	UserID      uint64    `gorm:"index:idx_user_created;not null;comment:用户ID" test_data:"user_id"`
	Content     string    `gorm:"type:text;comment:内容" test_data:"content"`
	MediaURLs   MediaURLs `gorm:"type:json;comment:媒体地址" json:"media_urls"`
	Type        int       `gorm:"default:0;comment:类型(0文1图2视)" test_data:"type"`
	VisibleType int       `gorm:"default:0;comment:可见性(0公开1粉丝2私密)" test_data:"visible_type"`

	CreatedAt int64 `gorm:"index:idx_user_created;not null;comment:创建时间戳" test_data:"created_at"`
	UpdatedAt int64 `gorm:"not null;comment:更新时间戳" test_data:"updated_at"`
	DeletedAt int64 `gorm:"default:0;comment:软删除时间戳" test_data:"-"`

	// 聚合字段 (不映射到 tweets 表，而是从 tweet_stats 表或 Redis 读)
	LikeCount    int `gorm:"-" test_data:"like_count"`
	CommentCount int `gorm:"-" test_data:"comment_count"`
	ShareCount   int `gorm:"-" test_data:"share_count"`

	// 额外信息 (用于前端渲染，比如是否已点赞)
	IsLiked bool `gorm:"-" test_data:"is_liked"`
}

// TableName 指定表名
func (Tweet) TableName() string {
	return "tweets"
}

// TweetRepository 推文仓储接口
// 所有的入参都尽量用 ID，不要传整个对象，保持接口纯粹
type TweetRepository interface {
	//Create 发推
	Create(ctx context.Context, tweet *Tweet) error

	//Delete 删除(软删除)
	Delete(ctx context.Context, id uint64) error

	//GetByID 查单条
	GetByID(ctx context.Context, id uint64) (*Tweet, error)

	// ListByUserID 查某个人的时间线 (游标分页)
	// cursor: 上一页最后一条 tweet 的 ID (Snowflake ID 自带时间属性，天然适合做 cursor)
	ListByUserID(ctx context.Context, userID uint64, cursor uint64, limit int) ([]*Tweet, error)

	//ListFeeds 查关注流 (最难的部分，暂留接口)
	ListFeeds(ctx context.Context, userID uint64, cursor uint64, limit int) ([]*Tweet, error)

	// GetByIDs 批量查询推文
	GetByIDs(ctx context.Context, ids []uint64) ([]*Tweet, error)
}
