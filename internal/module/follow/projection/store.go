package projection

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strconv"
	"time"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
)

func uintString(id uint64) string { return strconv.FormatUint(id, 10) }

type SQLStore struct {
	DB                *gorm.DB
	NewID             func() (uint64, error)
	MaintenanceAutoID bool
}
type sessionKey struct{}
type session struct {
	db     *gorm.DB
	author uint64
}

func (s *SQLStore) db(ctx context.Context) *gorm.DB {
	if conn, ok := ctx.Value(sessionKey{}).(session); ok {
		return conn.db.WithContext(ctx)
	}
	return s.DB.WithContext(ctx)
}
func (s *SQLStore) Register(ctx context.Context, event events.FollowRelationEvent) (bool, error) {
	if event.Validate(event.EventKey == events.FollowRelationEventKey(event.RelationID, true)) != nil || s.NewID == nil {
		return false, errors.New("invalid follow registration")
	}
	body, _ := json.Marshal(event)
	digest := fmt.Sprintf("%x", sha256.Sum256(body))
	created := false
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		marker, err := token()
		if err != nil {
			return err
		}
		receipt := IngressReceipt{EventKey: event.EventKey, Digest: digest, CreatedAt: time.Now().UnixMilli(), RegistrationToken: marker}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&receipt)
		if result.Error != nil {
			return result.Error
		}
		var existing IngressReceipt
		if err := tx.Where("event_key = ?", event.EventKey).Take(&existing).Error; err != nil {
			return err
		}
		if existing.Digest != digest {
			return errors.New("follow event identity conflict")
		}
		if existing.RegistrationToken != marker {
			return nil
		}
		payload, _ := json.Marshal(Checkpoint{Event: event})
		for _, kind := range []string{AuthorTask, PairTask} {
			id, err := s.NewID()
			if err != nil {
				return err
			}
			if id == 0 {
				return errors.New("task identity unavailable")
			}
			key := "follow-task:" + event.EventKey + ":" + kind
			if err := tx.Create(&domain.OutboxTask{ID: id, TaskType: kind, DedupKey: &key, Payload: string(payload), MaxRetries: 5, CreatedAt: time.Now().UnixMilli(), UpdatedAt: time.Now().UnixMilli()}).Error; err != nil {
				return err
			}
		}
		created = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return created, nil
}
func (s *SQLStore) WithAuthorLock(ctx context.Context, author uint64, fn func(context.Context) error) (outErr error) {
	if author == 0 || fn == nil {
		return errors.New("invalid follow lock")
	}
	pool, err := s.DB.DB()
	if err != nil {
		return err
	}
	conn, err := pool.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	key := "follow:projection:author:" + uintString(author)
	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 5)", key).Scan(&acquired); err != nil || !acquired.Valid || acquired.Int64 != 1 {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		return errors.New("follow author lock unavailable")
	}
	defer func() {
		release, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		var done sql.NullInt64
		if conn.QueryRowContext(release, "SELECT RELEASE_LOCK(?)", key).Scan(&done) != nil || !done.Valid || done.Int64 != 1 {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			outErr = errors.Join(outErr, errors.New("follow lock release unavailable"))
		}
	}()
	db := s.DB.Session(&gorm.Session{NewDB: true, SkipDefaultTransaction: true}).WithContext(ctx)
	db.Statement.ConnPool = conn
	return fn(context.WithValue(ctx, sessionKey{}, session{db: db, author: author}))
}
func (s *SQLStore) Owns(ctx context.Context, t *domain.OutboxTask) (bool, error) {
	var count int64
	err := s.active(ctx, t).Count(&count).Error
	return count == 1, err
}
func (s *SQLStore) active(ctx context.Context, t *domain.OutboxTask) *gorm.DB {
	return s.db(ctx).Model(&domain.OutboxTask{}).Where("id = ? AND status = ? AND lease_owner = ? AND lease_token = ? AND lease_until > ?", t.ID, domain.OutboxStatusProcessing, t.LeaseOwner, t.LeaseToken, time.Now().UnixMilli())
}
func (s *SQLStore) ReadPair(ctx context.Context, follower, author uint64) (PairFact, error) {
	var rows []domain.Follow
	if err := s.db(ctx).Where("follower_id = ? AND followee_id = ? AND deleted_at = 0", follower, author).Order("id DESC").Limit(2).Find(&rows).Error; err != nil {
		return PairFact{}, err
	}
	if len(rows) > 1 {
		return PairFact{}, domain.ErrFollowDuplicateActive
	}
	if len(rows) == 1 {
		return PairFact{Generation: rows[0].ID, Active: true}, nil
	}
	var latest domain.Follow
	err := s.db(ctx).Where("follower_id = ? AND followee_id = ?", follower, author).Order("id DESC").Take(&latest).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return PairFact{}, nil
	}
	return PairFact{Generation: latest.ID}, err
}
func (s *SQLStore) ReadAuthor(ctx context.Context, id uint64) (AuthorState, error) {
	var state AuthorState
	err := s.db(ctx).Where("author_id = ?", id).Take(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return AuthorState{AuthorID: id}, nil
	}
	return state, err
}
func (s *SQLStore) SaveAuthor(ctx context.Context, state AuthorState) error {
	conn, ok := ctx.Value(sessionKey{}).(session)
	if !ok || conn.author != state.AuthorID {
		return errors.New("author state requires its lock")
	}
	return s.db(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "author_id"}}, UpdateAll: true}).Create(&state).Error
}
func (s *SQLStore) FollowerCount(ctx context.Context, author uint64) (int64, error) {
	var count int64
	err := s.db(ctx).Raw("SELECT /*+ MAX_EXECUTION_TIME(2000) */ COUNT(DISTINCT follower_id) FROM follows WHERE followee_id = ? AND deleted_at = 0", author).Scan(&count).Error
	return count, err
}
func (s *SQLStore) HistoryFollowers(ctx context.Context, author, cursor uint64) (RelationPage, error) {
	q := s.db(ctx).Model(&domain.Follow{}).Select("id,follower_id").Where("followee_id = ?", author)
	if cursor > 0 {
		q = q.Where("id < ?", cursor)
	}
	var rows []RelationRow
	if err := q.Order("id DESC").Limit(PageSize + 1).Find(&rows).Error; err != nil {
		return RelationPage{}, err
	}
	more := len(rows) > PageSize
	if more {
		rows = rows[:PageSize]
	}
	next := uint64(0)
	if more {
		next = rows[len(rows)-1].ID
	}
	return RelationPage{Rows: rows, More: more, Next: next}, nil
}
func (s *SQLStore) ActiveFollowers(ctx context.Context, author uint64, ids []uint64) (map[uint64]bool, error) {
	active := map[uint64]bool{}
	if len(ids) == 0 {
		return active, nil
	}
	if len(ids) > PageSize {
		return nil, errors.New("follow scope too large")
	}
	var rows []domain.Follow
	if err := s.db(ctx).Select("follower_id").Where("followee_id = ? AND follower_id IN ? AND deleted_at = 0", author, ids).Limit(2*PageSize + 1).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		if active[row.FollowerID] {
			return nil, domain.ErrFollowDuplicateActive
		}
		active[row.FollowerID] = true
	}
	return active, nil
}
func (s *SQLStore) HistoryTweets(ctx context.Context, author, cursor uint64) (TweetPage, error) {
	q := s.db(ctx).Model(&domain.Tweet{}).Select("id,visible_type,deleted_at").Where("user_id = ?", author)
	if cursor > 0 {
		q = q.Where("id < ?", cursor)
	}
	var rows []TweetRow
	if err := q.Order("id DESC").Limit(PageSize + 1).Find(&rows).Error; err != nil {
		return TweetPage{}, err
	}
	more := len(rows) > PageSize
	if more {
		rows = rows[:PageSize]
	}
	next := uint64(0)
	if more {
		next = rows[len(rows)-1].ID
	}
	return TweetPage{Rows: rows, More: more, Next: next}, nil
}
func (s *SQLStore) Advance(ctx context.Context, t *domain.OutboxTask, p Checkpoint, done bool) (bool, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return false, err
	}
	status := domain.OutboxStatusPending
	if done {
		status = domain.OutboxStatusSuccess
	}
	result := s.active(ctx, t).Updates(map[string]interface{}{"payload": string(body), "status": status, "retries": 0, "error_msg": "", "lease_owner": "", "lease_token": "", "lease_until": 0, "updated_at": time.Now().UnixMilli()})
	return result.RowsAffected == 1, result.Error
}

func (s *SQLStore) Interrupt(ctx context.Context, t *domain.OutboxTask) (bool, error) {
	result := s.active(ctx, t).Updates(map[string]interface{}{"status": domain.OutboxStatusPending, "retries": gorm.Expr("GREATEST(retries-1,0)"), "lease_owner": "", "lease_token": "", "lease_until": 0, "updated_at": time.Now().UnixMilli()})
	return result.RowsAffected == 1, result.Error
}
