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

func dryMessengerRepo(t *testing.T) *messageRepo {
	t.Helper()
	conn, err := sql.Open("mysql", "fixture:fixture@tcp(127.0.0.1:1)/fixture")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: conn, SkipInitializeWithVersion: true}), &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	return &messageRepo{db: db}
}

func TestMessageSQLUsesConversationExclusiveCursorAndLookahead(t *testing.T) {
	r := dryMessengerRepo(t)
	var rows []*domain.Message
	result := r.messagePageQuery(context.Background(), "1_2", 9007199254740993, 101).Find(&rows)
	text := result.Statement.SQL.String()
	if result.Error != nil || !strings.Contains(text, "conversation_id = ?") || !strings.Contains(text, "id < ?") || !strings.Contains(text, "ORDER BY id DESC LIMIT ?") {
		t.Fatalf("message SQL: %s %v", text, result.Error)
	}
	if !reflect.DeepEqual(result.Statement.Vars, []interface{}{"1_2", uint64(9007199254740993), 101}) {
		t.Fatalf("message params: %v", result.Statement.Vars)
	}
}

func TestConversationSQLBoundsBeforeGroupingAndSeeksLatestRows(t *testing.T) {
	r := dryMessengerRepo(t)
	var rows []*domain.Message
	result := r.conversationPageQuery(context.Background(), 1, 900, 800, 101).Find(&rows)
	text := result.Statement.SQL.String()
	for _, fragment := range []string{"MAX(id) AS latest_id", "id <= ?", "GROUP BY", "JOIN (", "m.id = latest.latest_id", "m.sender_id = ? OR m.receiver_id = ?", "m.id < ?", "ORDER BY m.id DESC LIMIT ?"} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("conversation SQL missing %q: %s", fragment, text)
		}
	}
	if !reflect.DeepEqual(result.Statement.Vars, []interface{}{uint64(1), uint64(1), uint64(900), uint64(1), uint64(1), uint64(800), 101}) {
		t.Fatalf("conversation params: %v", result.Statement.Vars)
	}
	if strings.Index(text, "id <= ?") > strings.Index(text, "GROUP BY") || strings.Contains(text, "id < ? GROUP BY") {
		t.Fatalf("before applied to messages before grouping: %s", text)
	}
}

func TestUnreadAndReadCommitSQLRemainScoped(t *testing.T) {
	r := dryMessengerRepo(t)
	var counts []struct {
		ConversationID string
		UnreadCount    int64
	}
	query := r.conversationUnreadQuery(context.Background(), 1, 900, []string{"1_2", "1_3"}).Find(&counts)
	text := query.Statement.SQL.String()
	for _, fragment := range []string{"receiver_id = ?", "is_read = ?", "id <= ?", "conversation_id IN", "GROUP BY"} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("unread SQL missing %q: %s", fragment, text)
		}
	}
	update := r.messageReadPageQuery(context.Background(), "1_2", 1, []uint64{900, 800}).Update("is_read", true)
	text = update.Statement.SQL.String()
	for _, fragment := range []string{"UPDATE", "conversation_id = ?", "receiver_id = ?", "is_read = ?", "id IN"} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("read commit not scoped: %s", text)
		}
	}
	if !reflect.DeepEqual(update.Statement.Vars, []interface{}{true, "1_2", uint64(1), false, uint64(900), uint64(800)}) {
		t.Fatalf("read commit params: %v", update.Statement.Vars)
	}
	if err := r.MarkMessagePageRead(context.Background(), "1_2", 1, nil); err == nil {
		t.Fatal("empty read set accepted")
	}
}
