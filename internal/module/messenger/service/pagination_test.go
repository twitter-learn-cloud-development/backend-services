package service

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
	messengerv1 "twitter-clone/api/messenger/v1"
	"twitter-clone/internal/domain"
)

type historyRepository struct {
	domain.MessageRepository
	mu       sync.Mutex
	messages []*domain.Message
	err      error
	readErr  error
	reads    [][]uint64
	limits   []int
}

func (r *historyRepository) GetMessages(ctx context.Context, conversation string, cursor uint64, limit int) ([]*domain.Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.limits = append(r.limits, limit)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if r.err != nil {
		return nil, r.err
	}
	result := []*domain.Message{}
	for _, m := range r.messages {
		if m.ConversationID == conversation && (cursor == 0 || m.ID < cursor) {
			copy := *m
			result = append(result, &copy)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID > result[j].ID })
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}
func (r *historyRepository) MarkMessagePageRead(_ context.Context, conversation string, reader uint64, ids []uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.readErr != nil {
		return r.readErr
	}
	r.reads = append(r.reads, append([]uint64(nil), ids...))
	for _, m := range r.messages {
		for _, id := range ids {
			if m.ID == id && m.ConversationID == conversation && m.ReceiverID == reader {
				m.IsRead = true
			}
		}
	}
	return nil
}
func (r *historyRepository) ListConversationPage(ctx context.Context, user, snapshot, before uint64, limit int) (domain.ConversationPage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.limits = append(r.limits, limit)
	if ctx.Err() != nil {
		return domain.ConversationPage{}, ctx.Err()
	}
	if r.err != nil {
		return domain.ConversationPage{}, r.err
	}
	if snapshot == 0 {
		for _, m := range r.messages {
			if (m.SenderID == user || m.ReceiverID == user) && m.ID > snapshot {
				snapshot = m.ID
			}
		}
	}
	latest := map[string]*domain.Message{}
	unread := map[string]int64{}
	for _, m := range r.messages {
		if (m.SenderID != user && m.ReceiverID != user) || m.ID > snapshot {
			continue
		}
		if m.ReceiverID == user && !m.IsRead {
			unread[m.ConversationID]++
		}
		if prev := latest[m.ConversationID]; prev == nil || m.ID > prev.ID {
			latest[m.ConversationID] = m
		}
	}
	convs := []*domain.Conversation{}
	for _, m := range latest {
		if before > 0 && m.ID >= before {
			continue
		}
		peer := m.SenderID
		if peer == user {
			peer = m.ReceiverID
		}
		copy := *m
		convs = append(convs, &domain.Conversation{PeerID: peer, LatestMessage: &copy, UnreadCount: unread[m.ConversationID]})
	}
	sort.Slice(convs, func(i, j int) bool { return convs[i].LatestMessage.ID > convs[j].LatestMessage.ID })
	if len(convs) > limit {
		convs = convs[:limit]
	}
	return domain.ConversationPage{Conversations: convs, SnapshotID: snapshot}, nil
}

func historyMessage(id, user, peer uint64) *domain.Message {
	return &domain.Message{ID: id, ConversationID: getConversationID(user, peer), SenderID: peer, ReceiverID: user, Content: "private history", CreatedAt: 1000}
}

func TestMessageHistoryPagesSameTimestampAndMarksOnlyDelivered(t *testing.T) {
	r := &historyRepository{messages: []*domain.Message{historyMessage(9007199254740997, 1, 2), historyMessage(9007199254740996, 1, 2), historyMessage(9007199254740995, 1, 2), historyMessage(9007199254740994, 1, 2), historyMessage(999, 3, 4)}}
	s := NewMessengerService(r, nil)
	first, err := s.GetMessages(context.Background(), &messengerv1.GetMessagesRequest{UserId: 1, PeerId: 2, Limit: 2})
	if err != nil || len(first.Messages) != 2 || first.NextCursor != "9007199254740996" || !first.HasMore {
		t.Fatalf("first: %+v %v", first, err)
	}
	if !reflect.DeepEqual(r.reads, [][]uint64{{9007199254740997, 9007199254740996}}) || r.messages[2].IsRead || r.messages[4].IsRead {
		t.Fatalf("unseen messages marked read: %v", r.reads)
	}
	r.messages = append(r.messages, historyMessage(9007199254740998, 1, 2))
	second, err := s.GetMessages(context.Background(), &messengerv1.GetMessagesRequest{UserId: 1, PeerId: 2, Limit: 2, Cursor: first.NextCursor})
	if err != nil || second.HasMore || second.NextCursor != "" || len(second.Messages) != 2 || second.Messages[0].Id != 9007199254740995 || second.Messages[1].Id != 9007199254740994 {
		t.Fatalf("second: %+v %v", second, err)
	}
	if r.messages[5].IsRead {
		t.Fatal("new message marked by older page")
	}
}

func TestConversationPagesUseSnapshotBeforeGrouping(t *testing.T) {
	r := &historyRepository{messages: []*domain.Message{historyMessage(900, 1, 2), historyMessage(800, 1, 3), historyMessage(700, 1, 2), historyMessage(600, 1, 4), historyMessage(500, 1, 5), historyMessage(400, 99, 88)}}
	s := NewMessengerService(r, nil)
	first, err := s.GetConversations(context.Background(), &messengerv1.GetConversationsRequest{UserId: 1, Limit: 2})
	if err != nil || !first.HasMore || first.NextCursor != "c1:1:900:800" || len(first.Conversations) != 2 || first.Conversations[0].PeerId != 2 || first.Conversations[0].UnreadCount != 2 {
		t.Fatalf("first: %+v %v", first, err)
	}
	// Previously returned and not-yet-returned conversations both receive new messages.
	r.messages = append(r.messages, historyMessage(1000, 1, 2), historyMessage(1100, 1, 4), historyMessage(1200, 1, 9))
	last, err := s.GetConversations(context.Background(), &messengerv1.GetConversationsRequest{UserId: 1, Limit: 2, Cursor: first.NextCursor})
	if err != nil || last.HasMore || last.NextCursor != "" || len(last.Conversations) != 2 || last.Conversations[0].PeerId != 4 || last.Conversations[0].LatestMessage.Id != 600 || last.Conversations[0].UnreadCount != 1 || last.Conversations[1].PeerId != 5 {
		t.Fatalf("last moved/repeated: %+v %v", last, err)
	}
}

func TestMessengerPageLimitsEmptyAndErrors(t *testing.T) {
	for _, limit := range []int32{-1, 0, 1, 100, 101, 2147483647} {
		r := &historyRepository{}
		for i := 0; i < 101; i++ {
			r.messages = append(r.messages, historyMessage(uint64(1000-i), 1, 2))
		}
		got, err := NewMessengerService(r, nil).GetMessages(context.Background(), &messengerv1.GetMessagesRequest{UserId: 1, PeerId: 2, Limit: limit})
		want := messengerPageLimit(limit)
		if err != nil || len(got.Messages) != want || !got.HasMore || r.limits[0] != want+1 {
			t.Fatalf("limit %d: %+v %v", limit, got, err)
		}
	}
	r := &historyRepository{}
	s := NewMessengerService(r, nil)
	messages, err := s.GetMessages(context.Background(), &messengerv1.GetMessagesRequest{UserId: 1, PeerId: 2})
	if err != nil || messages.Messages == nil || messages.HasMore || messages.NextCursor != "" {
		t.Fatalf("empty messages: %+v %v", messages, err)
	}
	convs, err := s.GetConversations(context.Background(), &messengerv1.GetConversationsRequest{UserId: 1})
	if err != nil || convs.Conversations == nil || convs.HasMore || convs.NextCursor != "" {
		t.Fatalf("empty convs: %+v %v", convs, err)
	}
	r.err = errors.New("private SQL detail")
	_, err = s.GetMessages(context.Background(), &messengerv1.GetMessagesRequest{UserId: 1, PeerId: 2})
	if status.Code(err) != codes.Unavailable || status.Convert(err).Message() != "message history is unavailable" {
		t.Fatalf("error leaked: %v", err)
	}
	r.err = nil
	r.messages = []*domain.Message{historyMessage(1, 1, 2)}
	r.readErr = errors.New("read commit failed")
	if _, err = s.GetMessages(context.Background(), &messengerv1.GetMessagesRequest{UserId: 1, PeerId: 2}); status.Code(err) != codes.Unavailable {
		t.Fatalf("read commit failure hidden: %v", err)
	}
}

func TestMessengerRejectsBadCursorBeforeRepository(t *testing.T) {
	r := &historyRepository{}
	s := NewMessengerService(r, nil)
	for _, cursor := range []string{"x", "-1", "01", "18446744073709551616", "c1:1:9:8"} {
		if _, err := s.GetMessages(context.Background(), &messengerv1.GetMessagesRequest{UserId: 1, PeerId: 2, Cursor: cursor}); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("message cursor accepted: %s %v", cursor, err)
		}
	}
	for _, cursor := range []string{"900", "c2:1:9:8", "c1:2:9:8", "c1:1:9:10", "c1:1:09:8", "c1:1:0:0", "c1:1:9:", "c1:1:18446744073709551616:8"} {
		if _, err := s.GetConversations(context.Background(), &messengerv1.GetConversationsRequest{UserId: 1, Cursor: cursor}); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("conversation cursor accepted: %s %v", cursor, err)
		}
	}
	if _, err := s.GetMessages(context.Background(), nil); status.Code(err) != codes.InvalidArgument {
		t.Fatal(err)
	}
	if _, err := s.GetConversations(context.Background(), nil); status.Code(err) != codes.InvalidArgument {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.GetMessages(ctx, &messengerv1.GetMessagesRequest{UserId: 1, PeerId: 2}); status.Code(err) != codes.Canceled {
		t.Fatal(err)
	}
	if len(r.limits) != 0 || len(r.reads) != 0 {
		t.Fatal("bad request touched data")
	}
}

func TestMessagePageRejectsWrongOwnerAndBrokenOrder(t *testing.T) {
	for _, messages := range [][]*domain.Message{{nil}, {historyMessage(2, 3, 4)}, {historyMessage(3, 1, 2), historyMessage(4, 1, 2)}, {historyMessage(3, 1, 2), historyMessage(3, 1, 2)}} {
		if err := validateMessagePage(messages, 1, 2, 0, 3); status.Code(err) != codes.DataLoss {
			t.Fatalf("invalid page accepted: %v", err)
		}
	}
	if err := validateConversationPage(domain.ConversationPage{SnapshotID: 10, Conversations: []*domain.Conversation{{PeerID: 2, LatestMessage: historyMessage(11, 1, 2)}}}, 1, 10, 0, 2); status.Code(err) != codes.DataLoss {
		t.Fatal("snapshot crossed")
	}
	if _, err := NewMessengerService(nil, nil).GetConversations(context.Background(), &messengerv1.GetConversationsRequest{UserId: 1}); status.Code(err) != codes.FailedPrecondition {
		t.Fatal("legacy pager fallback")
	}
}

func TestMessengerCursorAndWireCompatibility(t *testing.T) {
	max := ^uint64(0)
	cursor := encodeConversationCursor(max, max, max-1)
	snapshot, before, err := parseConversationCursor(cursor, max)
	if err != nil || snapshot != max || before != max-1 {
		t.Fatalf("uint64 cursor: %s %v", cursor, err)
	}
	if id, err := parseMessageCursor(strconv.FormatUint(max, 10)); err != nil || id != max {
		t.Fatal("large message ID lost")
	}
	for name, fields := range map[protoreflect.Name]map[protoreflect.Name]protoreflect.FieldNumber{
		"GetMessagesRequest": {"user_id": 1, "peer_id": 2, "limit": 3, "cursor": 4}, "GetMessagesResponse": {"messages": 1, "next_cursor": 2, "has_more": 3},
		"GetConversationsRequest": {"user_id": 1, "limit": 2, "cursor": 3}, "GetConversationsResponse": {"conversations": 1, "next_cursor": 2, "has_more": 3},
	} {
		message := messengerv1.File_api_messenger_v1_messenger_proto.Messages().ByName(name)
		for field, n := range fields {
			if message.Fields().ByName(field).Number() != n {
				t.Fatalf("field moved: %s.%s", name, field)
			}
		}
	}
}

func TestConversationPageHasLookaheadAtOneHundred(t *testing.T) {
	r := &historyRepository{}
	for i := 0; i < 101; i++ {
		r.messages = append(r.messages, historyMessage(uint64(1000-i), 1, uint64(100+i)))
	}
	s := NewMessengerService(r, nil)
	first, err := s.GetConversations(context.Background(), &messengerv1.GetConversationsRequest{UserId: 1, Limit: 100})
	if err != nil || len(first.Conversations) != 100 || !first.HasMore || first.NextCursor != "c1:1:1000:901" {
		t.Fatalf("lookahead: %+v %v", first, err)
	}
	last, err := s.GetConversations(context.Background(), &messengerv1.GetConversationsRequest{UserId: 1, Limit: 100, Cursor: first.NextCursor})
	if err != nil || len(last.Conversations) != 1 || last.HasMore || last.NextCursor != "" || last.Conversations[0].LatestMessage.Id != 900 {
		t.Fatalf("last page: %+v %v", last, err)
	}
}
