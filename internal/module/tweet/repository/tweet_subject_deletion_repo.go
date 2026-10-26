package repository

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/pkg/database/uow"
)

type tweetSubjectDeletionRepo struct {
	db *gorm.DB
}

func NewTweetSubjectDeletionRepository(db *gorm.DB) domain.TweetSubjectDeletionRepository {
	return &tweetSubjectDeletionRepo{db: db}
}

func (r *tweetSubjectDeletionRepo) GetOperation(ctx context.Context, operationKey string) (*domain.TweetSubjectDeletionOperation, error) {
	var operation domain.TweetSubjectDeletionOperation
	db := uow.ExtractTx(ctx, r.db)
	err := db.WithContext(ctx).Where("operation_key = ?", operationKey).First(&operation).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrTweetSubjectDeletionNotFound
		}
		return nil, fmt.Errorf("get tweet subject deletion operation: %w", err)
	}
	return &operation, nil
}

func (r *tweetSubjectDeletionRepo) CreateOperation(ctx context.Context, operation *domain.TweetSubjectDeletionOperation) error {
	if operation == nil {
		return errors.New("tweet subject deletion operation is nil")
	}
	db := uow.ExtractTx(ctx, r.db)
	if err := db.WithContext(ctx).Create(operation).Error; err != nil {
		if isDuplicateKeyError(err) {
			return domain.ErrTweetSubjectDeletionExists
		}
		return fmt.Errorf("create tweet subject deletion operation: %w", err)
	}
	return nil
}

func (r *tweetSubjectDeletionRepo) ObserveSubject(ctx context.Context, userID uint64) (domain.TweetSubjectInventory, error) {
	db := uow.ExtractTx(ctx, r.db).WithContext(ctx)
	inventory := domain.TweetSubjectInventory{}
	counts := []struct {
		model any
		field *int64
	}{
		{&domain.Tweet{}, &inventory.AuthoredTweets},
		{&domain.Comment{}, &inventory.AuthoredComments},
		{&domain.Like{}, &inventory.Likes},
		{&domain.Bookmark{}, &inventory.Bookmarks},
		{&domain.Retweet{}, &inventory.Retweets},
		{&domain.PollVote{}, &inventory.PollVotes},
	}
	for _, count := range counts {
		if err := db.Model(count.model).Where("user_id = ?", userID).Count(count.field).Error; err != nil {
			return domain.TweetSubjectInventory{}, fmt.Errorf("count tweet subject data: %w", err)
		}
	}
	return inventory, nil
}

func (r *tweetSubjectDeletionRepo) ListAuthoredTweetIDs(ctx context.Context, userID uint64, limit int) ([]uint64, error) {
	if limit <= 0 {
		return nil, errors.New("tweet subject deletion list limit must be positive")
	}
	var ids []uint64
	db := uow.ExtractTx(ctx, r.db)
	if err := db.WithContext(ctx).Model(&domain.Tweet{}).
		Where("user_id = ?", userID).Order("id ASC").Limit(limit).Pluck("id", &ids).Error; err != nil {
		return nil, fmt.Errorf("list authored tweet ids for deletion: %w", err)
	}
	return ids, nil
}

func (r *tweetSubjectDeletionRepo) DeleteOwnedData(ctx context.Context, userID uint64, authoredTweetIDs []uint64) error {
	db := uow.ExtractTx(ctx, r.db).WithContext(ctx)
	if len(authoredTweetIDs) > 0 {
		var pollIDs []uint64
		if err := db.Model(&domain.Poll{}).Where("tweet_id IN ?", authoredTweetIDs).Pluck("id", &pollIDs).Error; err != nil {
			return fmt.Errorf("list authored poll ids for deletion: %w", err)
		}
		if len(pollIDs) > 0 {
			if err := db.Where("poll_id IN ?", pollIDs).Delete(&domain.PollVote{}).Error; err != nil {
				return fmt.Errorf("delete votes for authored polls: %w", err)
			}
			if err := db.Where("poll_id IN ?", pollIDs).Delete(&domain.PollOption{}).Error; err != nil {
				return fmt.Errorf("delete options for authored polls: %w", err)
			}
		}
		if err := db.Where("tweet_id IN ?", authoredTweetIDs).Delete(&domain.Poll{}).Error; err != nil {
			return fmt.Errorf("delete authored polls: %w", err)
		}
		for _, model := range []any{&domain.Like{}, &domain.Bookmark{}, &domain.Retweet{}} {
			if err := db.Where("tweet_id IN ?", authoredTweetIDs).Delete(model).Error; err != nil {
				return fmt.Errorf("delete authored tweet relations: %w", err)
			}
		}
	}
	for _, model := range []any{&domain.Comment{}, &domain.Like{}, &domain.Bookmark{}, &domain.Retweet{}, &domain.PollVote{}} {
		if err := db.Where("user_id = ?", userID).Delete(model).Error; err != nil {
			return fmt.Errorf("delete tweet subject owned data: %w", err)
		}
	}
	if err := db.Where("user_id = ?", userID).Delete(&domain.TweetCreateIdempotency{}).Error; err != nil {
		return fmt.Errorf("delete tweet create idempotency data: %w", err)
	}
	if err := db.Where("user_id = ?", userID).Delete(&domain.Tweet{}).Error; err != nil {
		return fmt.Errorf("delete authored tweets: %w", err)
	}
	return nil
}
