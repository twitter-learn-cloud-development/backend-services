package repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"twitter-clone/internal/domain"
)

var _ domain.ConversationPageRepository = (*messageRepo)(nil)

func (r *messageRepo) conversationSnapshotQuery(ctx context.Context, userID uint64) *gorm.DB {
	return r.db.WithContext(ctx).Model(&domain.Message{}).
		Select("COALESCE(MAX(id), 0) AS snapshot_id").Where("sender_id = ? OR receiver_id = ?", userID, userID)
}

func (r *messageRepo) conversationPageQuery(ctx context.Context, userID, snapshot, before uint64, limit int) *gorm.DB {
	latest := r.db.WithContext(ctx).Model(&domain.Message{}).
		Select("conversation_id, MAX(id) AS latest_id").
		Where("(sender_id = ? OR receiver_id = ?) AND id <= ?", userID, userID, snapshot).
		Group("conversation_id")
	query := r.db.WithContext(ctx).Table("messages AS m").Select("m.*").
		Joins("JOIN (?) AS latest ON m.id = latest.latest_id", latest).
		Where("m.sender_id = ? OR m.receiver_id = ?", userID, userID)
	if before > 0 {
		query = query.Where("m.id < ?", before)
	}
	return query.Order("m.id DESC").Limit(limit)
}

func (r *messageRepo) conversationUnreadQuery(ctx context.Context, userID, snapshot uint64, conversations []string) *gorm.DB {
	return r.db.WithContext(ctx).Model(&domain.Message{}).
		Select("conversation_id, COUNT(*) AS unread_count").
		Where("receiver_id = ? AND is_read = ? AND id <= ? AND conversation_id IN ?", userID, false, snapshot, conversations).
		Group("conversation_id")
}

func (r *messageRepo) ListConversationPage(ctx context.Context, userID, snapshot, before uint64, limit int) (domain.ConversationPage, error) {
	if userID == 0 || limit < 2 || limit > 101 || (before > 0 && (snapshot == 0 || before > snapshot)) {
		return domain.ConversationPage{}, fmt.Errorf("invalid conversation page request")
	}
	if snapshot == 0 {
		var row struct{ SnapshotID uint64 }
		if err := r.conversationSnapshotQuery(ctx, userID).Scan(&row).Error; err != nil {
			return domain.ConversationPage{}, fmt.Errorf("read conversation frontier: %w", err)
		}
		snapshot = row.SnapshotID
	}
	page := domain.ConversationPage{SnapshotID: snapshot, Conversations: make([]*domain.Conversation, 0)}
	if snapshot == 0 {
		return page, nil
	}
	var messages []*domain.Message
	if err := r.conversationPageQuery(ctx, userID, snapshot, before, limit).Find(&messages).Error; err != nil {
		return domain.ConversationPage{}, fmt.Errorf("read conversation page: %w", err)
	}
	if len(messages) == 0 {
		return page, nil
	}
	// The final row may be lookahead; do not hydrate its unread count.
	visible := messages
	if len(messages) == limit {
		visible = messages[:limit-1]
	}
	ids := make([]string, 0, len(visible))
	for _, message := range visible {
		ids = append(ids, message.ConversationID)
	}
	var counts []struct {
		ConversationID string
		UnreadCount    int64
	}
	if len(ids) > 0 {
		if err := r.conversationUnreadQuery(ctx, userID, snapshot, ids).Scan(&counts).Error; err != nil {
			return domain.ConversationPage{}, fmt.Errorf("read conversation unread counts: %w", err)
		}
	}
	byID := make(map[string]int64, len(counts))
	for _, count := range counts {
		byID[count.ConversationID] = count.UnreadCount
	}
	for _, message := range messages {
		peer := message.SenderID
		if peer == userID {
			peer = message.ReceiverID
		}
		page.Conversations = append(page.Conversations, &domain.Conversation{PeerID: peer, LatestMessage: message, UnreadCount: byID[message.ConversationID]})
	}
	return page, nil
}
