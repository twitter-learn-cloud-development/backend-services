package repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"twitter-clone/internal/domain"
)

var _ domain.MessagePageReadRepository = (*messageRepo)(nil)

func (r *messageRepo) messagePageQuery(ctx context.Context, conversationID string, cursor uint64, limit int) *gorm.DB {
	query := r.db.WithContext(ctx).Model(&domain.Message{}).Where("conversation_id = ?", conversationID)
	if cursor > 0 {
		query = query.Where("id < ?", cursor)
	}
	return query.Order("id DESC").Limit(limit)
}

func (r *messageRepo) messageReadPageQuery(ctx context.Context, conversationID string, readerID uint64, ids []uint64) *gorm.DB {
	return r.db.WithContext(ctx).Model(&domain.Message{}).
		Where("conversation_id = ? AND receiver_id = ? AND is_read = ? AND id IN ?", conversationID, readerID, false, ids)
}

func (r *messageRepo) MarkMessagePageRead(ctx context.Context, conversationID string, readerID uint64, ids []uint64) error {
	if conversationID == "" || readerID == 0 || len(ids) == 0 || len(ids) > 100 {
		return fmt.Errorf("invalid message read page")
	}
	for _, id := range ids {
		if id == 0 {
			return fmt.Errorf("invalid message read page")
		}
	}
	return r.messageReadPageQuery(ctx, conversationID, readerID, ids).Update("is_read", true).Error
}
