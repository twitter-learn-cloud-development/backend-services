package repository

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"twitter-clone/internal/domain"
)

func TestFollowPagesKeepLookaheadAtPublicLimit(t *testing.T) {
	for _, count := range []int{0, 100, 101} {
		t.Run(fmtPageCount(count), func(t *testing.T) {
			rows := make([]domain.Follow, count)
			for i := range rows {
				rows[i] = domain.Follow{ID: uint64(10000 - i), FollowerID: uint64(i + 1), FolloweeID: uint64(500 + i)}
			}
			followers := buildFollowerPage(rows, 100)
			followees := buildFolloweePage(rows, 100)
			wantCount := count
			if wantCount > 100 {
				wantCount = 100
			}
			if len(followers.FollowerIDs) != wantCount || len(followees.FolloweeIDs) != wantCount || followers.FollowerIDs == nil || followees.FolloweeIDs == nil {
				t.Fatalf("page sizes: %+v %+v", followers, followees)
			}
			if followers.HasMore != (count == 101) || followees.HasMore != followers.HasMore {
				t.Fatal("lookahead lost")
			}
			var wantCursor uint64
			if count == 101 {
				wantCursor = rows[99].ID
			}
			if followers.NextCursor != wantCursor || followees.NextCursor != wantCursor {
				t.Fatalf("cursor: %d/%d want %d", followers.NextCursor, followees.NextCursor, wantCursor)
			}
		})
	}
}

func fmtPageCount(count int) string {
	if count == 0 {
		return "empty"
	}
	if count == 100 {
		return "exact_limit"
	}
	return "lookahead"
}

func TestBuildFolloweePageUsesRelationID(t *testing.T) {
	page := buildFolloweePage([]domain.Follow{{ID: 900, FolloweeID: 8000}, {ID: 800, FolloweeID: 2}, {ID: 700, FolloweeID: 6000}}, 2)
	if !reflect.DeepEqual(page.FolloweeIDs, []uint64{8000, 2}) || page.NextCursor != 800 || !page.HasMore {
		t.Fatalf("user ID used as cursor: %+v", page)
	}
}

func TestFollowPageQueriesUseOwnerAndExclusiveRelationCursor(t *testing.T) {
	// sql.Open creates a lazy handle; DryRun and disabled ping issue no network IO.
	conn, err := sql.Open("mysql", "fixture:fixture@tcp(127.0.0.1:1)/fixture")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: conn, SkipInitializeWithVersion: true}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	repo := &followRepo{db: db}
	ctx := context.Background()
	for name, query := range map[string]func(context.Context, uint64, uint64, int) *gorm.DB{"followers": repo.followerPageQuery, "followees": repo.followeePageQuery} {
		t.Run(name, func(t *testing.T) {
			for _, cursor := range []uint64{0, 9007199254740993} {
				var rows []domain.Follow
				result := query(ctx, 42, cursor, 100).Find(&rows)
				if result.Error != nil {
					t.Fatal(result.Error)
				}
				statement := result.Statement
				column := "followee_id"
				if name == "followees" {
					column = "follower_id"
				}
				text := statement.SQL.String()
				if !strings.Contains(text, column+" = ? AND deleted_at = 0") || !strings.Contains(text, "ORDER BY id DESC LIMIT ?") {
					t.Fatalf("query lost scope/order: %s", text)
				}
				want := []interface{}{uint64(42)}
				if cursor > 0 {
					want = append(want, cursor)
					if !strings.Contains(text, "id < ?") {
						t.Fatalf("nonexclusive cursor: %s", text)
					}
				} else if strings.Contains(text, "id < ?") {
					t.Fatalf("first-page cursor: %s", text)
				}
				want = append(want, 101)
				if !reflect.DeepEqual(statement.Vars, want) {
					t.Fatalf("lookahead vars: %v want %v", statement.Vars, want)
				}
				if statement.Context != ctx {
					t.Fatal("query dropped context")
				}
			}
		})
	}
}
