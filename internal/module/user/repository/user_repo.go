package repository

import (
	"context"
	"fmt"
	"gorm.io/gorm"
	"time"
	"twitter-clone/internal/domain"
	"twitter-clone/pkg/pkg/snowflake"
)

type userRepo struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) domain.UserRepository {
	return &userRepo{db: db}
}

func (r *userRepo) Create(ctx context.Context, user *domain.User) error {
	if user.ID == 0 {
		// 确保你有一个真实的 snowflake 实现，如果没有，暂时用 uint64(time.Now().UnixNano()) 代替
		user.ID = uint64(snowflake.GenerateID())
	}

	now := time.Now().UnixMilli()
	if user.CreatedAt == 0 {
		user.CreatedAt = now
	}
	if user.UpdatedAt == 0 {
		user.UpdatedAt = now
	}
	user.DeletedAt = 0

	return r.db.WithContext(ctx).Create(user).Error
}

func (r *userRepo) GetByID(ctx context.Context, id uint64) (*domain.User, error) {
	var user domain.User
	// ✅ 修复：必须手动加 deleted_at = 0
	err := r.db.WithContext(ctx).
		Where("id = ? AND deleted_at = 0", id).
		First(&user).Error

	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepo) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	var user domain.User
	// ✅ 修复：必须手动加 deleted_at = 0
	err := r.db.WithContext(ctx).
		Where("email = ? AND deleted_at = 0", email).
		First(&user).Error

	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepo) GetByUsername(ctx context.Context, username string) (*domain.User, error) {
	var user domain.User // 这里的指针定义微调了一下，更安全
	err := r.db.WithContext(ctx).
		Where("username = ? AND deleted_at = 0", username).
		First(&user).Error

	if err != nil {
		return nil, err
	}
	return &user, nil
}

// Update ✅ 修复：实现了真正的保存逻辑
func (r *userRepo) Update(ctx context.Context, user *domain.User) error {
	user.UpdatedAt = time.Now().UnixMilli()

	// Save 会更新所有字段（包括零值），所以必须确保 user 结构体是完整的
	// 如果只想更新非零字段，请用 Updates
	return r.db.WithContext(ctx).Save(user).Error
}

func (r *userRepo) UpdatePartial(ctx context.Context, id uint64, updates map[string]interface{}) error {
	updates["updated_at"] = time.Now().UnixMilli()

	result := r.db.WithContext(ctx).
		Model(&domain.User{}).
		Where("id = ? AND deleted_at = 0", id).
		Updates(updates)

	if result.Error != nil {
		return fmt.Errorf("failed to update user: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		// 这里不一定是 error，可能是 id 不存在，视业务逻辑而定
		return fmt.Errorf("user not found")
	}
	return nil
}

func (r *userRepo) Delete(ctx context.Context, id uint64) error {
	now := time.Now().UnixMilli()

	// ✅ 逻辑正确：更新删除时间戳，而不是真的 DELETE
	result := r.db.WithContext(ctx).
		Model(&domain.User{}).
		Where("id = ? AND deleted_at = 0", id).
		Updates(map[string]interface{}{
			"deleted_at": now,
			"updated_at": now,
		})

	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("user not found")
	}
	return nil
}

func (r *userRepo) List(ctx context.Context, offset, limit int) ([]*domain.User, error) {
	// ✅ 修复：切片初始化
	users := make([]*domain.User, 0)

	err := r.db.WithContext(ctx).
		Where("deleted_at = 0"). // ✅ 修复：过滤已删除
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&users).Error // ✅ 修复：传入指针地址 &users

	if err != nil {
		return nil, err
	}
	return users, nil
}

func (r *userRepo) IsEmailExist(ctx context.Context, email string) (bool, error) {
	var count int64
	// ✅ 修复：过滤已删除
	err := r.db.WithContext(ctx).
		Model(&domain.User{}). // 加上 Model 指定表，否则可能报错
		Where("email = ? AND deleted_at = 0", email).
		Count(&count).Error

	return count > 0, err
}

func (r *userRepo) IsUsernameExist(ctx context.Context, username string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&domain.User{}).
		Where("username = ? AND deleted_at = 0", username).
		Count(&count).Error

	return count > 0, err
}
