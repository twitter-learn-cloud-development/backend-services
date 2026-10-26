package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm/clause"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/pkg/database/uow"
	"twitter-clone/pkg/pkg/snowflake"
)

func (r *followRepo) ApplyFollowTransition(ctx context.Context, follower, followee uint64, following bool) (domain.FollowTransition, error) {
	if r == nil || r.db == nil {
		return domain.FollowTransition{}, domain.ErrFollowMutationUnavailable
	}
	if follower == 0 || followee == 0 || follower == followee {
		return domain.FollowTransition{}, errors.New("invalid follow identity")
	}
	tx := uow.ExtractTx(ctx, r.db)
	if tx == nil || tx == r.db {
		return domain.FollowTransition{}, domain.ErrFollowMutationUnavailable
	}
	tx = tx.WithContext(ctx)
	lock := domain.FollowPairLock{FollowerID: follower, FolloweeID: followee}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&lock).Error; err != nil {
		return domain.FollowTransition{}, err
	}
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("follower_id = ? AND followee_id = ?", follower, followee).Take(&lock).Error; err != nil {
		return domain.FollowTransition{}, err
	}
	var rows []domain.Follow
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("follower_id = ? AND followee_id = ? AND deleted_at = 0", follower, followee).Order("id ASC").Limit(2).Find(&rows).Error; err != nil {
		return domain.FollowTransition{}, err
	}
	if len(rows) > 1 {
		return domain.FollowTransition{}, domain.ErrFollowDuplicateActive
	}
	now := time.Now().UnixMilli()
	if following {
		if len(rows) == 1 {
			return domain.FollowTransition{}, domain.ErrFollowAlreadyActive
		}
		id, err := r.relationID()
		if err != nil {
			return domain.FollowTransition{}, err
		}
		if id == 0 {
			return domain.FollowTransition{}, errors.New("invalid relation identity")
		}
		relation := domain.Follow{ID: id, FollowerID: follower, FolloweeID: followee, CreatedAt: now}
		if err := tx.Create(&relation).Error; err != nil {
			return domain.FollowTransition{}, err
		}
		return domain.FollowTransition{RelationID: id, OccurredAt: now}, nil
	}
	if len(rows) == 0 {
		return domain.FollowTransition{}, domain.ErrFollowNotActive
	}
	result := tx.Model(&domain.Follow{}).Where("id = ? AND follower_id = ? AND followee_id = ? AND deleted_at = 0", rows[0].ID, follower, followee).Update("deleted_at", now)
	if result.Error != nil {
		return domain.FollowTransition{}, result.Error
	}
	if result.RowsAffected != 1 {
		return domain.FollowTransition{}, errors.New("follow transition row count mismatch")
	}
	return domain.FollowTransition{RelationID: rows[0].ID, OccurredAt: now}, nil
}
func (r *followRepo) relationID() (uint64, error) {
	if r.newRelationID != nil {
		return r.newRelationID()
	}
	return snowflake.GenerateID()
}
