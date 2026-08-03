package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"twitter-clone/internal/domain"
)

const (
	maxOutboxClaimBatchSize = 100
	maxOutboxErrorBytes     = 4096
)

type outboxRepo struct {
	db *gorm.DB
}

// NewOutboxRepository 创建发件箱仓储
func NewOutboxRepository(db *gorm.DB) domain.OutboxRepository {
	return &outboxRepo{db: db}
}

// Create 创建 Outbox 任务
func (r *outboxRepo) Create(ctx context.Context, task *domain.OutboxTask) error {
	prepareOutboxTaskTimestamps(task)
	return r.db.WithContext(ctx).Create(task).Error
}

// CreateIdempotent inserts a task once for a non-empty deduplication key.
func (r *outboxRepo) CreateIdempotent(ctx context.Context, task *domain.OutboxTask) (bool, error) {
	if task == nil || task.DedupKey == nil || strings.TrimSpace(*task.DedupKey) == "" {
		return false, errors.New("outbox task dedup key is required")
	}
	prepareOutboxTaskTimestamps(task)
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "dedup_key"}},
		DoNothing: true,
	}).Create(task)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// Claim atomically locks eligible rows, advances their attempt counter, and installs a fencing token.
func (r *outboxRepo) Claim(ctx context.Context, request domain.OutboxClaimRequest) ([]*domain.OutboxTask, error) {
	request.LeaseOwner = strings.TrimSpace(request.LeaseOwner)
	request.LeaseToken = strings.TrimSpace(request.LeaseToken)
	if err := validateOutboxClaimRequest(request); err != nil {
		return nil, err
	}

	var tasks []*domain.OutboxTask
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := claimableOutboxTasksQuery(tx, request).
			Find(&tasks).Error; err != nil {
			return err
		}
		if len(tasks) == 0 {
			return nil
		}

		ids := outboxTaskIDs(tasks)
		result := tx.Model(&domain.OutboxTask{}).
			Where("id IN ?", ids).
			Updates(map[string]interface{}{
				"status":      domain.OutboxStatusProcessing,
				"retries":     gorm.Expr("retries + 1"),
				"error_msg":   "",
				"lease_owner": request.LeaseOwner,
				"lease_token": request.LeaseToken,
				"lease_until": request.LeaseUntilUnixMilli,
				"updated_at":  request.ClaimedAtUnixMilli,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != int64(len(tasks)) {
			return fmt.Errorf("claim outbox tasks: expected %d rows, updated %d", len(tasks), result.RowsAffected)
		}

		for _, task := range tasks {
			task.Status = domain.OutboxStatusProcessing
			task.Retries++
			task.ErrorMsg = ""
			task.LeaseOwner = request.LeaseOwner
			task.LeaseToken = request.LeaseToken
			task.LeaseUntil = request.LeaseUntilUnixMilli
			task.UpdatedAt = request.ClaimedAtUnixMilli
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return tasks, nil
}

// CompleteClaim commits success only while the exact owner/token lease is still valid.
func (r *outboxRepo) CompleteClaim(ctx context.Context, completion domain.OutboxClaimCompletion) (bool, error) {
	completion.LeaseOwner = strings.TrimSpace(completion.LeaseOwner)
	completion.LeaseToken = strings.TrimSpace(completion.LeaseToken)
	if err := validateOutboxClaimIdentity(completion.TaskID, completion.LeaseOwner, completion.LeaseToken, completion.CompletedAtUnixMilli); err != nil {
		return false, err
	}

	result := activeOutboxClaimQuery(r.db.WithContext(ctx), completion.TaskID, completion.LeaseOwner, completion.LeaseToken, completion.CompletedAtUnixMilli).
		Updates(map[string]interface{}{
			"status":      domain.OutboxStatusSuccess,
			"error_msg":   "",
			"lease_owner": "",
			"lease_token": "",
			"lease_until": 0,
			"updated_at":  completion.CompletedAtUnixMilli,
		})
	return result.RowsAffected == 1, result.Error
}

// FailClaim releases a current claim. Terminal failures cannot be claimed again.
func (r *outboxRepo) FailClaim(ctx context.Context, failure domain.OutboxClaimFailure) (bool, error) {
	failure.LeaseOwner = strings.TrimSpace(failure.LeaseOwner)
	failure.LeaseToken = strings.TrimSpace(failure.LeaseToken)
	if err := validateOutboxClaimIdentity(failure.TaskID, failure.LeaseOwner, failure.LeaseToken, failure.FailedAtUnixMilli); err != nil {
		return false, err
	}

	updates := map[string]interface{}{
		"status":      domain.OutboxStatusFailed,
		"error_msg":   boundedOutboxError(failure.ErrorMsg),
		"lease_owner": "",
		"lease_token": "",
		"lease_until": 0,
		"updated_at":  failure.FailedAtUnixMilli,
	}
	if failure.Terminal {
		updates["retries"] = gorm.Expr("max_retries")
	}

	result := activeOutboxClaimQuery(r.db.WithContext(ctx), failure.TaskID, failure.LeaseOwner, failure.LeaseToken, failure.FailedAtUnixMilli).
		Updates(updates)
	return result.RowsAffected == 1, result.Error
}

// RecoverExpiredClaims releases expired leases in a bounded, skip-locked batch.
func (r *outboxRepo) RecoverExpiredClaims(ctx context.Context, nowUnixMilli int64, limit int) (domain.OutboxLeaseRecovery, error) {
	var zero domain.OutboxLeaseRecovery
	if nowUnixMilli <= 0 {
		return zero, errors.New("outbox lease recovery time must be positive")
	}
	if limit <= 0 {
		return zero, nil
	}
	if limit > maxOutboxClaimBatchSize {
		return zero, fmt.Errorf("outbox lease recovery limit exceeds %d", maxOutboxClaimBatchSize)
	}

	var recovered domain.OutboxLeaseRecovery
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var tasks []*domain.OutboxTask
		if err := expiredOutboxClaimsQuery(tx, nowUnixMilli, limit).Find(&tasks).Error; err != nil {
			return err
		}
		if len(tasks) == 0 {
			return nil
		}

		retryableIDs, exhaustedIDs := partitionExpiredOutboxClaims(tasks)

		updated, err := releaseExpiredOutboxClaims(tx, retryableIDs, nowUnixMilli, false)
		if err != nil {
			return err
		}
		recovered.Retryable = updated

		updated, err = releaseExpiredOutboxClaims(tx, exhaustedIDs, nowUnixMilli, true)
		if err != nil {
			return err
		}
		recovered.Exhausted = updated
		return nil
	})
	if err != nil {
		return zero, err
	}
	return recovered, nil
}

// Delete 处理成功时物理删除记录，保护数据库表空间
func (r *outboxRepo) Delete(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Delete(&domain.OutboxTask{}, id).Error
}

// DeleteCompletedBefore removes a bounded batch of expired success receipts.
func (r *outboxRepo) DeleteCompletedBefore(ctx context.Context, beforeUnixMilli int64, limit int) (int64, error) {
	if limit <= 0 {
		return 0, nil
	}

	var ids []uint64
	err := r.db.WithContext(ctx).
		Model(&domain.OutboxTask{}).
		Where("status = ? AND updated_at < ?", domain.OutboxStatusSuccess, beforeUnixMilli).
		Order("updated_at ASC, id ASC").
		Limit(limit).
		Pluck("id", &ids).Error
	if err != nil || len(ids) == 0 {
		return 0, err
	}

	result := r.db.WithContext(ctx).
		Where("status = ? AND updated_at < ? AND id IN ?", domain.OutboxStatusSuccess, beforeUnixMilli, ids).
		Delete(&domain.OutboxTask{})
	return result.RowsAffected, result.Error
}

func prepareOutboxTaskTimestamps(task *domain.OutboxTask) {
	if task.CreatedAt == 0 {
		task.CreatedAt = time.Now().UnixMilli()
	}
	if task.UpdatedAt == 0 {
		task.UpdatedAt = task.CreatedAt
	}
}

func validateOutboxClaimRequest(request domain.OutboxClaimRequest) error {
	if request.Limit <= 0 {
		return errors.New("outbox claim limit must be positive")
	}
	if request.Limit > maxOutboxClaimBatchSize {
		return fmt.Errorf("outbox claim limit exceeds %d", maxOutboxClaimBatchSize)
	}
	if request.LeaseUntilUnixMilli <= request.ClaimedAtUnixMilli || request.ClaimedAtUnixMilli <= 0 {
		return errors.New("outbox claim lease window is invalid")
	}
	return validateOutboxLeaseIdentity(request.LeaseOwner, request.LeaseToken)
}

func validateOutboxClaimIdentity(taskID uint64, owner, token string, atUnixMilli int64) error {
	if taskID == 0 {
		return errors.New("outbox task ID is required")
	}
	if err := validateOutboxLeaseIdentity(owner, token); err != nil {
		return err
	}
	if atUnixMilli <= 0 {
		return errors.New("outbox claim timestamp must be positive")
	}
	return nil
}

func validateOutboxLeaseIdentity(owner, token string) error {
	if strings.TrimSpace(owner) == "" || len(owner) > 191 {
		return errors.New("outbox lease owner is invalid")
	}
	if strings.TrimSpace(token) == "" || len(token) > 64 {
		return errors.New("outbox lease token is invalid")
	}
	return nil
}

func claimableOutboxTasksQuery(db *gorm.DB, request domain.OutboxClaimRequest) *gorm.DB {
	return db.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Where("retries < max_retries AND (status = ? OR (status = ? AND updated_at + (CASE retries WHEN 0 THEN 1000 WHEN 1 THEN 2000 WHEN 2 THEN 4000 WHEN 3 THEN 8000 WHEN 4 THEN 16000 ELSE 32000 END) <= ?))",
			domain.OutboxStatusPending, domain.OutboxStatusFailed, request.ClaimedAtUnixMilli).
		Order("id ASC").
		Limit(request.Limit)
}

func activeOutboxClaimQuery(db *gorm.DB, taskID uint64, owner, token string, atUnixMilli int64) *gorm.DB {
	return db.Model(&domain.OutboxTask{}).
		Where("id = ? AND status = ? AND lease_owner = ? AND lease_token = ? AND lease_until > ?",
			taskID, domain.OutboxStatusProcessing, owner, token, atUnixMilli)
}

func expiredOutboxClaimsQuery(db *gorm.DB, nowUnixMilli int64, limit int) *gorm.DB {
	return db.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Where("status = ? AND lease_until > 0 AND lease_until <= ?", domain.OutboxStatusProcessing, nowUnixMilli).
		Order("lease_until ASC, id ASC").
		Limit(limit)
}

func releaseExpiredOutboxClaims(tx *gorm.DB, ids []uint64, nowUnixMilli int64, exhausted bool) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	errorMessage := "outbox lease expired before completion"
	updates := map[string]interface{}{
		"status":      domain.OutboxStatusFailed,
		"error_msg":   errorMessage,
		"lease_owner": "",
		"lease_token": "",
		"lease_until": 0,
		"updated_at":  nowUnixMilli,
	}
	if exhausted {
		updates["error_msg"] = "outbox lease expired after max attempts"
		updates["retries"] = gorm.Expr("max_retries")
	}
	result := tx.Model(&domain.OutboxTask{}).
		Where("id IN ? AND status = ? AND lease_until <= ?", ids, domain.OutboxStatusProcessing, nowUnixMilli).
		Updates(updates)
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected != int64(len(ids)) {
		return 0, fmt.Errorf("recover expired outbox claims: expected %d rows, updated %d", len(ids), result.RowsAffected)
	}
	return result.RowsAffected, nil
}

func outboxTaskIDs(tasks []*domain.OutboxTask) []uint64 {
	ids := make([]uint64, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	return ids
}

func partitionExpiredOutboxClaims(tasks []*domain.OutboxTask) (retryable, exhausted []uint64) {
	retryable = make([]uint64, 0, len(tasks))
	exhausted = make([]uint64, 0, len(tasks))
	for _, task := range tasks {
		if task.Retries < task.MaxRetries {
			retryable = append(retryable, task.ID)
			continue
		}
		exhausted = append(exhausted, task.ID)
	}
	return retryable, exhausted
}

func boundedOutboxError(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "outbox execution failed"
	}
	if len(value) <= maxOutboxErrorBytes {
		return value
	}
	end := maxOutboxErrorBytes
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end]
}
