package schema

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"time"
)

type SQLBootstrapStore struct{ DB *gorm.DB }
type bootstrapConnectionKey struct{}

func (s SQLBootstrapStore) session(ctx context.Context) *gorm.DB {
	if db, ok := ctx.Value(bootstrapConnectionKey{}).(*gorm.DB); ok {
		return db.WithContext(ctx)
	}
	return s.DB.WithContext(ctx)
}
func (s SQLBootstrapStore) Read(ctx context.Context, table string) (Metadata, error) {
	return (SQLReader{DB: s.session(ctx)}).Read(ctx, table)
}
func (s SQLBootstrapStore) Create(ctx context.Context, statement Statement) error {
	if _, ok := ctx.Value(bootstrapConnectionKey{}).(*gorm.DB); !ok {
		return errors.New("bootstrap DDL requires owned connection lock")
	}
	return s.session(ctx).Session(&gorm.Session{Logger: logger.Discard}).Exec(statement.SQL).Error
}
func (s SQLBootstrapStore) WithLock(ctx context.Context, owner string, run func(context.Context) error) error {
	if s.DB == nil {
		return errors.New("bootstrap database required")
	}
	if _, err := OwnerModels(owner); err != nil {
		return err
	}
	return s.DB.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard}).Connection(func(conn *gorm.DB) (result error) {
		var database string
		if err := conn.Raw("SELECT DATABASE()").Scan(&database).Error; err != nil || database == "" {
			return errors.New("bootstrap database identity unavailable")
		}
		key := fmt.Sprintf("schema:bootstrap:%s", planStringDigest(database+":"+owner))
		var acquired sql.NullInt64
		if err := conn.Raw("SELECT GET_LOCK(?, 2)", key).Scan(&acquired).Error; err != nil || !acquired.Valid || acquired.Int64 != 1 {
			return errors.New("bootstrap lock unavailable")
		}
		defer func() {
			release, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			var released sql.NullInt64
			if err := conn.WithContext(release).Raw("SELECT RELEASE_LOCK(?)", key).Scan(&released).Error; err != nil || !released.Valid || released.Int64 != 1 {
				result = errors.Join(result, errors.New("bootstrap release uncertain"))
			}
		}()
		return run(context.WithValue(ctx, bootstrapConnectionKey{}, conn))
	})
}
func planStringDigest(value string) string {
	plan := Plan{Version: value}
	return planDigest(plan)[:40]
}
