package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"twitter-clone/internal/domain"
)

type publicationRepo struct{ db *gorm.DB }

func NewOutboxPublicationRepository(db *gorm.DB) domain.OutboxPublicationRepository {
	return &publicationRepo{db: db}
}
func (r *publicationRepo) RecordPublication(ctx context.Context, receipt domain.OutboxPublication) error {
	if receipt.EventID == 0 || receipt.EventType == "" || len(receipt.PayloadDigest) != 64 || receipt.ConfirmedAt <= 0 {
		return errors.New("invalid publication receipt")
	}
	if r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "event_id"}}, DoNothing: true}).Create(&receipt).Error != nil {
		return errors.New("publication receipt write unavailable")
	}
	persisted, err := r.ReadPublication(ctx, receipt.EventID)
	if err != nil || persisted == nil || persisted.EventType != receipt.EventType || persisted.PayloadDigest != receipt.PayloadDigest {
		return errors.New("publication receipt identity conflict")
	}
	return nil
}
func (r *publicationRepo) ReadPublication(ctx context.Context, id uint64) (*domain.OutboxPublication, error) {
	var receipt domain.OutboxPublication
	err := r.db.WithContext(ctx).Where("event_id = ?", id).Take(&receipt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("publication receipt read unavailable")
	}
	return &receipt, nil
}
func (r *publicationRepo) ReadEvents(ctx context.Context, ids []uint64) ([]domain.OutboxEvent, error) {
	if len(ids) == 0 || len(ids) > 100 {
		return nil, errors.New("publication event scope exceeds bound")
	}
	var rows []domain.OutboxEvent
	if r.db.WithContext(ctx).Where("id IN ?", ids).Order("id ASC").Find(&rows).Error != nil {
		return nil, errors.New("publication event read unavailable")
	}
	if len(rows) != len(ids) {
		return nil, errors.New("publication scope contains missing events")
	}
	return rows, nil
}
