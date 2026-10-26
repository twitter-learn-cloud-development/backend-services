package repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"twitter-clone/internal/domain"
)

func TestBatchFollowingSQLScopesCallerAndSoftDeletion(t *testing.T) {
	adapter := NewFollowReadRepository(nil)
	_, writable := adapter.(domain.FollowRepository)
	require.False(t, writable)
	conn, err := sql.Open("mysql", "fixture:fixture@tcp(127.0.0.1:1)/fixture")
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: conn, SkipInitializeWithVersion: true}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	require.NoError(t, err)
	r := &followRepo{db: db}
	var rows []domain.Follow
	q := r.batchFollowingQuery(context.Background(), 1, []uint64{2, 3}).Find(&rows)
	require.Contains(t, q.Statement.SQL.String(), "SELECT DISTINCT followee_id")
	require.Contains(t, q.Statement.SQL.String(), "follower_id = ? AND deleted_at = 0 AND followee_id IN")
	require.Equal(t, []interface{}{uint64(1), uint64(2), uint64(3)}, q.Statement.Vars)
	_, err = r.BatchIsFollowing(context.Background(), 1, []uint64{0})
	require.Error(t, err)
	_, err = r.BatchIsFollowing(context.Background(), 1, make([]uint64, 101))
	require.Error(t, err)
	result, err := r.BatchIsFollowing(context.Background(), 1, nil)
	require.NoError(t, err)
	require.Empty(t, result)
}
