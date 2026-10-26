package repository

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/pkg/database/uow"
)

var _ domain.TweetDeletionRepository = (*tweetRepo)(nil)

func (r *tweetRepo) GetForDeletion(ctx context.Context, id uint64) (*domain.Tweet, error) {
	db := uow.ExtractTx(ctx, r.db)
	if db == r.db {
		return nil, errors.New("tweet deletion requires a unit of work")
	}
	var tweet domain.Tweet
	err := db.WithContext(ctx).Select("id", "user_id", "deleted_at").
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND deleted_at = 0", id).Take(&tweet).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrTweetDeletionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read tweet deletion identity: %w", err)
	}
	return &tweet, nil
}
