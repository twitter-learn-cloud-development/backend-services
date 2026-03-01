package domain

import "context"

// Poll 投票实体
type Poll struct {
	ID        uint64 `gorm:"primaryKey;column:id;comment:主键ID (Snowflake)"`
	TweetID   uint64 `gorm:"index:idx_tweet;not null;comment:关联推文ID"`
	Question  string `gorm:"type:varchar(255);not null;comment:投票问题"` // 虽然通常推文内容就是问题，但有些设计允许独立问题
	EndTime   int64  `gorm:"not null;comment:结束时间戳"`
	CreatedAt int64  `gorm:"not null;comment:创建时间戳"`

	// 关联
	Options []PollOption `gorm:"foreignKey:PollID"`

	// 计算字段 (不存库)
	IsExpired     bool   `gorm:"-"`
	IsVoted       bool   `gorm:"-"`
	VotedOptionID uint64 `gorm:"-"`
	TotalVotes    int    `gorm:"-"`
}

// TableName 指定表名
func (Poll) TableName() string {
	return "polls"
}

// PollOption 投票选项实体
type PollOption struct {
	ID        uint64 `gorm:"primaryKey;column:id;comment:主键ID (Snowflake)"`
	PollID    uint64 `gorm:"index:idx_poll;not null;comment:关联投票ID"`
	Text      string `gorm:"type:varchar(255);not null;comment:选项文本"`
	VoteCount int    `gorm:"default:0;comment:票数"`

	// 计算字段
	Percentage float32 `gorm:"-"`
}

// TableName 指定表名
func (PollOption) TableName() string {
	return "poll_options"
}

// PollVote 用户投票记录实体 (用于防刷票和状态查询)
type PollVote struct {
	ID        uint64 `gorm:"primaryKey;column:id;comment:主键ID (Snowflake)"`
	PollID    uint64 `gorm:"index:idx_poll_user,unique;not null;comment:关联投票ID"` // 复合唯一索引
	OptionID  uint64 `gorm:"not null;comment:投的选项ID"`
	UserID    uint64 `gorm:"index:idx_poll_user,unique;not null;comment:用户ID"` // 复合唯一索引
	CreatedAt int64  `gorm:"not null;comment:投票时间戳"`
}

// TableName 指定表名
func (PollVote) TableName() string {
	return "poll_votes"
}

// PollRepository 投票仓储接口
type PollRepository interface {
	// Create 创建投票 (包含选项)
	Create(ctx context.Context, poll *Poll) error

	// GetByTweetID 获取推文的投票
	GetByTweetID(ctx context.Context, tweetID uint64) (*Poll, error)

	// GetByID 根据 ID 查询
	GetByID(ctx context.Context, id uint64) (*Poll, error)

	// GetVote 查询用户由于对某投票的记录
	GetVote(ctx context.Context, pollID, userID uint64) (*PollVote, error)

	// Vote 投票操作 (事务：创建记录 + 更新计数)
	Vote(ctx context.Context, vote *PollVote) error

	// GetByTweetIDs 批量获取
	GetByTweetIDs(ctx context.Context, tweetIDs []uint64) (map[uint64]*Poll, error)

	// GetVotesByTweetIDs 批量获取用户在这些推文下的投票记录 (返回 map[tweetID]voteOptionID)
	GetVotesByTweetIDs(ctx context.Context, tweetIDs []uint64, userID uint64) (map[uint64]uint64, error)
}
