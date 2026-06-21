package uow

import (
	"context"

	"gorm.io/gorm"
)

// 定义私有的 context key，防止外部冲突
type txKey struct{}

// 1. 暴露给 Service 层的纯洁接口 (没有任何 gorm 痕迹)
type Manager interface {
	// Do 执行一个包裹在事务中的闭包，向下传递包含了事务句柄的 txCtx
	Do(ctx context.Context, fn func(txCtx context.Context) error) error
}

// 2. GORM 的具体实现
type gormManager struct {
	db *gorm.DB
}

func NewGormManager(db *gorm.DB) Manager {
	return &gormManager{db: db}
}

func (m *gormManager) Do(ctx context.Context, fn func(txCtx context.Context) error) error {
	// 开启 GORM 事务
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 将 tx 悄悄塞进 context
		txCtx := context.WithValue(ctx, txKey{}, tx)
		// 执行业务闭包
		return fn(txCtx)
	})
}

// 3. 暴露给 Repository 层的提取工具
// ExtractTx 尝试从 Context 中获取事务句柄，如果没有则退化为普通 db
func ExtractTx(ctx context.Context, db *gorm.DB) *gorm.DB {
	if tx, ok := ctx.Value(txKey{}).(*gorm.DB); ok {
		return tx
	}
	return db
}
