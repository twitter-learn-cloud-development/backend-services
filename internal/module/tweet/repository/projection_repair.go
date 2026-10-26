package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"twitter-clone/internal/domain"
)

func (r *outboxRepo) RegisterProjectionRepair(ctx context.Context, tasks []*domain.OutboxTask) (int, error) {
	if len(tasks) == 0 {
		return 0, nil
	}
	if len(tasks) > 200 {
		return 0, errors.New("repair task batch exceeds bound")
	}
	for _, task := range tasks {
		if task == nil || task.DedupKey == nil || len(*task.DedupKey) > 191 || *task.DedupKey == "" || task.ID != 0 {
			return 0, errors.New("invalid repair task")
		}
	}
	// This maintenance writer uses the existing table's database allocation.
	// Fail closed if a deployment changed that allocation contract.
	var extra string
	if r.db.WithContext(ctx).Raw("SELECT EXTRA FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='outbox_tasks' AND COLUMN_NAME='id'").Scan(&extra).Error != nil || extra != "auto_increment" {
		return 0, errors.New("repair task ID allocation unavailable")
	}
	created := 0
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, task := range tasks {
			prepareOutboxTaskTimestamps(task)
			result := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "dedup_key"}}, DoNothing: true}).Create(task)
			if result.Error != nil {
				return errors.New("repair task registration unavailable")
			}
			var persisted domain.OutboxTask
			if tx.Where("dedup_key = ?", *task.DedupKey).Take(&persisted).Error != nil || persisted.TaskType != task.TaskType || persisted.Payload != task.Payload {
				return errors.New("repair task identity conflict")
			}
			if result.RowsAffected > 0 {
				created++
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return created, nil
}
