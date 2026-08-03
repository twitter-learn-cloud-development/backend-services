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
	VisiblePublic    = 0 //公开
	VisibleFollows   = 1 //仅粉丝可见
	VisiblePrivate   = 2 //仅自己可见
	VisibleShadowban = 4 //影子封禁
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
	ParentID    uint64    `gorm:"index:idx_parent;default:0;comment:父推文ID (回复)" test_data:"parent_id"`
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

	// 新增交互状态与转发显示字段
	IsBookmarked       bool   `gorm:"-"`
	IsRetweeted        bool   `gorm:"-"`
	IsRetweetedDisplay bool   `gorm:"-"`
	RetweetedAt        int64  `gorm:"-"`
	SortID             uint64 `gorm:"-"`

	// 投票信息 (聚合)
	Poll *Poll `gorm:"-"`
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

	// UpdateVisibleType updates tweet visibility when the tweet still belongs to authorID.
	// The bool reports whether this call changed the stored value.
	UpdateVisibleType(ctx context.Context, id uint64, authorID uint64, visibleType int) (bool, error)

	// ListByUserID 查某个人的时间线 (游标分页)
	// cursor: 上一页最后一条 tweet 的 ID (Snowflake ID 自带时间属性，天然适合做 cursor)
	ListByUserID(ctx context.Context, userID uint64, cursor uint64, limit int) ([]*Tweet, error)

	// GetFeeds 获取关注者的推文
	GetFeeds(ctx context.Context, userIDs []uint64, cursor uint64, limit int) ([]*Tweet, error)

	// Search 搜索推文
	Search(ctx context.Context, query string, cursor uint64, limit int) ([]*Tweet, error)

	// GetByIDs 批量查询推文
	GetByIDs(ctx context.Context, ids []uint64) ([]*Tweet, error)

	// ListAll 查询所有推文（全站最新）
	ListAll(ctx context.Context, cursor uint64, limit int) ([]*Tweet, error)
	GetReplies(ctx context.Context, parentID uint64, cursor uint64, limit int) ([]*Tweet, uint64, error)

	// ListRepliesByUserID 获取用户回复的推文列表
	ListRepliesByUserID(ctx context.Context, userID uint64, cursor uint64, limit int) ([]*Tweet, error)

	// ListMediaByUserID 获取用户带媒体的推文列表
	ListMediaByUserID(ctx context.Context, userID uint64, cursor uint64, limit int) ([]*Tweet, error)
}

// TrendingTopic 热门话题实体
type TrendingTopic struct {
	Topic string `json:"topic"`
	Score int32  `json:"score"`
}

// TweetCreatedPayload 对应发件箱中 TWEET_CREATED 事件的 JSON 结构
type TweetCreatedPayload struct {
	TweetID     uint64   `json:"tweet_id"`
	AuthorID    uint64   `json:"author_id"`
	ParentID    uint64   `json:"parent_id,omitempty"` // 回复父推文时需要
	Content     string   `json:"content"`
	MediaURLs   []string `json:"media_urls,omitempty"` // 附带的媒体流
	Type        int      `json:"type"`
	VisibleType int      `json:"visible_type"` // 控制下游是否推送到公网
	CreatedAt   int64    `json:"created_at"`   // 发生时间的时间戳

	// 投票等聚合子实体
	HasPoll bool  `json:"has_poll"`
	Poll    *Poll `json:"poll,omitempty"`
}
