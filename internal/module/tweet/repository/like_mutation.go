package repository

import (
	"context"
	"errors"
	"gorm.io/gorm/clause"
	"time"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/pkg/database/uow"
	"twitter-clone/pkg/pkg/snowflake"
)

func (r *likeRepo) ApplyLike(ctx context.Context, userID, tweetID uint64) (*domain.Like, bool, error) {
	if userID == 0 || tweetID == 0 {
		return nil, false, errors.New("invalid like identity")
	}
	tx := uow.ExtractTx(ctx, r.db)
	if tx == nil || tx == r.db {
		return nil, false, errors.New("like mutation requires unit of work")
	}
	var id uint64
	var err error
	if r.newLikeID != nil {
		id, err = r.newLikeID()
	} else {
		id, err = snowflake.GenerateID()
	}
	if err != nil {
		return nil, false, err
	}
	if id == 0 {
		return nil, false, errors.New("like identity unavailable")
	}
	requested := domain.Like{ID: id, UserID: userID, TweetID: tweetID, CreatedAt: time.Now().UnixMilli()}
	if err := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&requested).Error; err != nil {
		return nil, false, err
	}
	var persisted domain.Like
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND tweet_id = ?", userID, tweetID).Take(&persisted).Error; err != nil {
		return nil, false, err
	}
	if persisted.ID == 0 || persisted.UserID != userID || persisted.TweetID != tweetID || persisted.CreatedAt <= 0 {
		return nil, false, errors.New("like identity conflict")
	}
	return &persisted, persisted.ID == id, nil
}
