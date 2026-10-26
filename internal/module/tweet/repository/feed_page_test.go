package repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestFeedRepositoryKeepsOneHundredAndOneLookahead(t *testing.T) {
	conn, err := sql.Open("mysql", "fixture:fixture@tcp(127.0.0.1:1)/fixture")
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: conn, SkipInitializeWithVersion: true}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	require.NoError(t, err)
	var statement *gorm.Statement
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("capture_feed_sql", func(tx *gorm.DB) { statement = tx.Statement }))
	_, err = (&tweetRepo{db: db}).GetFeeds(context.Background(), []uint64{2, 3}, 9007199254740997, 101)
	require.NoError(t, err)
	require.Contains(t, statement.SQL.String(), "user_id IN")
	require.Contains(t, statement.SQL.String(), "deleted_at = 0 AND visible_type = ?")
	require.Contains(t, statement.SQL.String(), "id < ?")
	require.Contains(t, statement.SQL.String(), "ORDER BY id DESC LIMIT ?")
	require.Equal(t, []interface{}{uint64(2), uint64(3), 0, uint64(9007199254740997), 101}, statement.Vars)
}

func TestFollowingFeedSQLScopesAllCurrentRelationsAndVisibility(t *testing.T) {
	conn, err := sql.Open("mysql", "fixture:fixture@tcp(127.0.0.1:1)/fixture")
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: conn, SkipInitializeWithVersion: true}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	require.NoError(t, err)
	var statement *gorm.Statement
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("capture_current_feed_sql", func(tx *gorm.DB) { statement = tx.Statement }))
	_, err = (&tweetRepo{db: db}).ListFollowingFeed(context.Background(), 1, 9007199254740997, 101)
	require.NoError(t, err)
	require.Contains(t, statement.SQL.String(), "EXISTS (SELECT 1 FROM follows f")
	require.Contains(t, statement.SQL.String(), "f.follower_id = ? AND f.followee_id = tweets.user_id AND f.deleted_at = 0")
	require.Contains(t, statement.SQL.String(), "tweets.deleted_at = 0")
	require.Contains(t, statement.SQL.String(), "tweets.user_id = ? OR (tweets.visible_type IN")
	require.Contains(t, statement.SQL.String(), "tweets.id < ?")
	require.Contains(t, statement.SQL.String(), "ORDER BY tweets.id DESC LIMIT ?")
	require.Equal(t, []interface{}{uint64(1), 0, 1, uint64(1), uint64(9007199254740997), 101}, statement.Vars)
	_, err = (&tweetRepo{db: db}).ListFollowingFeed(context.Background(), 1, 0, 1001)
	require.Error(t, err)
}
