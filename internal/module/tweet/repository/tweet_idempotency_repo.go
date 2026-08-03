package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/pkg/database/uow"
)

type tweetCreateIdempotencyRepo struct {
	db *gorm.DB
}

func NewTweetCreateIdempotencyRepository(db *gorm.DB) domain.TweetCreateIdempotencyRepository {
	return &tweetCreateIdempotencyRepo{db: db}
}

func (r *tweetCreateIdempotencyRepo) Create(ctx context.Context, record *domain.TweetCreateIdempotency) error {
	if record == nil {
		return errors.New("tweet create idempotency record is nil")
	}
	if record.CreatedAt == 0 {
		record.CreatedAt = time.Now().UnixMilli()
	}
	db := uow.ExtractTx(ctx, r.db)
	if err := db.WithContext(ctx).Create(record).Error; err != nil {
		if isDuplicateKeyError(err) {
			return domain.ErrTweetCreateIdempotencyExists
		}
		return fmt.Errorf("create tweet idempotency record: %w", err)
	}
	return nil
}

func (r *tweetCreateIdempotencyRepo) Get(ctx context.Context, userID uint64, idempotencyKey string) (*domain.TweetCreateIdempotency, error) {
	var record domain.TweetCreateIdempotency
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND idempotency_key = ?", userID, idempotencyKey).
		First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrTweetCreateIdempotencyNotFound
		}
		return nil, fmt.Errorf("get tweet idempotency record: %w", err)
	}
	return &record, nil
}

func isDuplicateKeyError(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	var mysqlErr *mysqlDriver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
