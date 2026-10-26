package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"twitter-clone/internal/domain"
)

type projectionConnectionKey struct{}
type projectionConnection struct {
	db *gorm.DB
	id uint64
}

var _ domain.TweetProjectionRepository = (*tweetRepo)(nil)

func (r *tweetRepo) WithProjectionLock(ctx context.Context, id uint64, fn func(context.Context) error) (outErr error) {
	if id == 0 || fn == nil {
		return errors.New("invalid projection lock request")
	}
	pool, err := r.db.DB()
	if err != nil {
		return errors.New("projection connection pool unavailable")
	}
	conn, err := pool.Conn(ctx)
	if err != nil {
		return errors.New("projection connection unavailable")
	}
	defer conn.Close()
	key := fmt.Sprintf("tweet:projection:%d", id)
	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 5)", key).Scan(&acquired); err != nil || !acquired.Valid || acquired.Int64 != 1 {
		// An uncertain GET_LOCK result must never leave a session lock in pool.
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		return errors.New("projection lock unavailable")
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		var released sql.NullInt64
		if err := conn.QueryRowContext(releaseCtx, "SELECT RELEASE_LOCK(?)", key).Scan(&released); err != nil || !released.Valid || released.Int64 != 1 {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			outErr = errors.Join(outErr, errors.New("projection lock release unavailable"))
		}
	}()
	db := r.db.Session(&gorm.Session{NewDB: true}).WithContext(ctx)
	db.Statement.ConnPool = conn
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(context.WithValue(ctx, projectionConnectionKey{}, projectionConnection{db: db, id: id}))
}

func (r *tweetRepo) ReadProjectionTweet(ctx context.Context, id uint64) (*domain.Tweet, error) {
	connection, ok := ctx.Value(projectionConnectionKey{}).(projectionConnection)
	if !ok || id == 0 || connection.id != id {
		return nil, errors.New("projection read requires its lock")
	}
	var tweet domain.Tweet
	err := connection.db.WithContext(ctx).Where("id = ?", id).Take(&tweet).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("current projection fact unavailable")
	}
	return &tweet, nil
}
