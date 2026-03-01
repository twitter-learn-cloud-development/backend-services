package repository

import (
	"context"
	"errors"
	"twitter-clone/internal/domain"
	"twitter-clone/pkg/pkg/snowflake"

	"gorm.io/gorm"
)

type pollRepo struct {
	db *gorm.DB
}

func NewPollRepository(db *gorm.DB) domain.PollRepository {
	return &pollRepo{db: db}
}

func (r *pollRepo) Create(ctx context.Context, poll *domain.Poll) error {
	if poll.ID == 0 {
		poll.ID = snowflake.GenerateID()
	}
	// 为选项生成 ID
	for i := range poll.Options {
		if poll.Options[i].ID == 0 {
			poll.Options[i].ID = snowflake.GenerateID()
		}
		poll.Options[i].PollID = poll.ID
	}

	return r.db.WithContext(ctx).Create(poll).Error
}

func (r *pollRepo) GetByTweetID(ctx context.Context, tweetID uint64) (*domain.Poll, error) {
	var poll domain.Poll
	// 预加载选项
	err := r.db.WithContext(ctx).Preload("Options").Where("tweet_id = ?", tweetID).First(&poll).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil // Return nil if no poll found
		}
		return nil, err
	}
	return &poll, nil
}

func (r *pollRepo) GetByID(ctx context.Context, id uint64) (*domain.Poll, error) {
	var poll domain.Poll
	// 预加载选项
	err := r.db.WithContext(ctx).Preload("Options").Where("id = ?", id).First(&poll).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil // Return nil if no poll found
		}
		return nil, err
	}
	return &poll, nil
}

func (r *pollRepo) GetVote(ctx context.Context, pollID, userID uint64) (*domain.PollVote, error) {
	var vote domain.PollVote
	err := r.db.WithContext(ctx).Where("poll_id = ? AND user_id = ?", pollID, userID).First(&vote).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &vote, nil
}

func (r *pollRepo) Vote(ctx context.Context, vote *domain.PollVote) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 创建投票记录
		if vote.ID == 0 {
			vote.ID = snowflake.GenerateID()
		}
		if err := tx.Create(vote).Error; err != nil {
			return err // 重复投票会违反唯一约束，返回 error
		}

		// 2. 更新选项票数
		if err := tx.Model(&domain.PollOption{}).Where("id = ?", vote.OptionID).
			UpdateColumn("vote_count", gorm.Expr("vote_count + ?", 1)).Error; err != nil {
			return err
		}

		// 3. 更新投票总数 (可选，如果 Poll 实体有 TotalVotes 字段)
		// 这里暂不更新 Poll 表的总数，前端可以通过 sum(options.vote_count) 计算，或者查询时聚合
		// 但为了性能，或者如果 Poll 实体里加了 TotalVotes...
		// 实际上 domain.Poll 没有 TotalVotes 字段明确映射到 DB (通常是计算属性)，
		// 这里的 Update 可以省略，或者如果需要的话：
		// if err := tx.Model(&domain.Poll{}).Where("id = ?", vote.PollID).UpdateColumn("total_votes", gorm.Expr("total_votes + ?", 1)).Error; err != nil { return err }

		return nil
	})
}

func (r *pollRepo) GetByTweetIDs(ctx context.Context, tweetIDs []uint64) (map[uint64]*domain.Poll, error) {
	var polls []domain.Poll
	if err := r.db.WithContext(ctx).Preload("Options").Where("tweet_id IN ?", tweetIDs).Find(&polls).Error; err != nil {
		return nil, err
	}

	result := make(map[uint64]*domain.Poll)
	for i := range polls {
		result[polls[i].TweetID] = &polls[i]
	}
	return result, nil
}

func (r *pollRepo) GetVotesByTweetIDs(ctx context.Context, tweetIDs []uint64, userID uint64) (map[uint64]uint64, error) {
	// 先查出 tweetIDs 对应的 pollIDs
	var polls []domain.Poll
	if err := r.db.WithContext(ctx).Select("id, tweet_id").Where("tweet_id IN ?", tweetIDs).Find(&polls).Error; err != nil {
		return nil, err
	}

	if len(polls) == 0 {
		return make(map[uint64]uint64), nil
	}

	pollIDMap := make(map[uint64]uint64) // pollID -> tweetID
	pollIDs := make([]uint64, 0, len(polls))
	for _, p := range polls {
		pollIDMap[p.ID] = p.TweetID
		pollIDs = append(pollIDs, p.ID)
	}

	// 查 votes
	var votes []domain.PollVote
	if err := r.db.WithContext(ctx).Where("user_id = ? AND poll_id IN ?", userID, pollIDs).Find(&votes).Error; err != nil {
		return nil, err
	}

	result := make(map[uint64]uint64) // tweetID -> optionID
	for _, v := range votes {
		if tweetID, ok := pollIDMap[v.PollID]; ok {
			result[tweetID] = v.OptionID
		}
	}

	return result, nil
}
