package domain

import (
	"context"
)

// User 用户实体（完全匹配你的数据库表结构）
type User struct {
	ID           uint64 `gorm:"primaryKey;column:id;comment:主键ID (Snowflake)" test_data:"id"`
	Username     string `gorm:"uniqueIndex:uk_username;type:varchar(32);not null;comment:用户名" test_data:"username"`
	PasswordHash string `gorm:"type:varchar(255);not null;comment:密码哈希 (bcrypt)" test_data:"-"` // 不返回给前端
	Email        string `gorm:"uniqueIndex:uk_email;type:varchar(128);not null;comment:邮箱" test_data:"email"`
	Avatar       string `gorm:"type:varchar(255);default:'';comment:头像URL" test_data:"avatar"`
	Bio          string `gorm:"type:varchar(255);default:'';comment:简介" test_data:"bio"`
	CoverURL     string `gorm:"type:varchar(255);default:'';comment:封面图URL" test_data:"cover_url"`
	Website      string `gorm:"type:varchar(255);default:'';comment:个人网站" test_data:"website"`
	Location     string `gorm:"type:varchar(100);default:'';comment:地理位置" test_data:"location"`
	CreatedAt    int64  `gorm:"column:created_at;not null;comment:创建时间戳 (毫秒)" test_data:"created_at"`
	UpdatedAt    int64  `gorm:"column:updated_at;not null;comment:更新时间戳 (毫秒)" test_data:"updated_at"`
	DeletedAt    int64  `gorm:"column:deleted_at;default:0;comment:软删除时间戳，0表示未删除" test_data:"deleted_at,omitempty"`
}

// TableName 指定表名
func (User) TableName() string {
	return "users"
}

// UserRepository 用户仓储接口
type UserRepository interface {
	// Create 创建用户
	Create(ctx context.Context, user *User) error

	// GetByID 根据ID获取用户
	GetByID(ctx context.Context, id uint64) (*User, error)

	// GetByIDs 批量获取用户
	GetByIDs(ctx context.Context, ids []uint64) ([]*User, error)

	// Search 搜索用户
	Search(ctx context.Context, keyword string, cursor uint64, limit int) ([]*User, error)

	// GetByEmail 根据邮箱获取用户
	GetByEmail(ctx context.Context, email string) (*User, error)

	// GetByUsername 根据用户名获取用户
	GetByUsername(ctx context.Context, username string) (*User, error)

	// Update 更新用户信息
	Update(ctx context.Context, user *User) error

	// UpdatePartial 部分更新用户信息
	UpdatePartial(ctx context.Context, id uint64, updates map[string]interface{}) error

	// Delete 删除用户（软删除）
	Delete(ctx context.Context, id uint64) error

	// List 获取用户列表（分页）
	List(ctx context.Context, offset, limit int) ([]*User, error)

	// IsEmailExist 检查邮箱是否存在
	IsEmailExist(ctx context.Context, email string) (bool, error)

	// IsUsernameExist 检查用户名是否存在
	IsUsernameExist(ctx context.Context, username string) (bool, error)
}
