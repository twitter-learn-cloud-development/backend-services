package grpc

import (
	"context"
	"log"

	notificationv1 "twitter-clone/api/notification/v1"
	"twitter-clone/internal/domain"
)

// NotificationServer 通知 gRPC 服务器
type NotificationServer struct {
	notificationv1.UnimplementedNotificationServiceServer
	repo domain.NotificationRepository
}

// NewNotificationServer 创建通知 gRPC 服务器
func NewNotificationServer(repo domain.NotificationRepository) *NotificationServer {
	return &NotificationServer{repo: repo}
}

func (s *NotificationServer) ListNotifications(ctx context.Context, req *notificationv1.ListNotificationsRequest) (*notificationv1.ListNotificationsResponse, error) {
	log.Printf("gRPC: ListNotifications - user_id=%d, cursor=%d, limit=%d", req.UserId, req.Cursor, req.Limit)

	limit := int(req.Limit)
	if limit <= 0 {
		limit = 20
	}

	// 查 limit + 1 判断 has_more
	list, err := s.repo.List(ctx, req.UserId, req.Cursor, limit+1)
	if err != nil {
		log.Printf("❌ ListNotifications error: %v", err)
		return nil, err
	}

	hasMore := len(list) > limit
	if hasMore {
		list = list[:limit]
	}

	var nextCursor uint64
	if len(list) > 0 {
		nextCursor = list[len(list)-1].ID
	}

	protoList := make([]*notificationv1.Notification, len(list))
	for i, n := range list {
		protoList[i] = &notificationv1.Notification{
			Id:        n.ID,
			Type:      string(n.Type),
			ActorId:   n.ActorID,
			TargetId:  n.TargetID,
			Content:   n.Content,
			IsRead:    n.IsRead,
			CreatedAt: n.CreatedAt,
		}
	}

	return &notificationv1.ListNotificationsResponse{
		Notifications: protoList,
		NextCursor:    nextCursor,
		HasMore:       hasMore,
	}, nil
}

func (s *NotificationServer) MarkAsRead(ctx context.Context, req *notificationv1.MarkAsReadRequest) (*notificationv1.MarkAsReadResponse, error) {
	log.Printf("gRPC: MarkAsRead - user_id=%d, ids=%v", req.UserId, req.Ids)
	err := s.repo.MarkAsRead(ctx, req.Ids)
	if err != nil {
		log.Printf("❌ MarkAsRead error: %v", err)
		return nil, err
	}
	return &notificationv1.MarkAsReadResponse{Message: "ok"}, nil
}

func (s *NotificationServer) MarkAllAsRead(ctx context.Context, req *notificationv1.MarkAllAsReadRequest) (*notificationv1.MarkAllAsReadResponse, error) {
	log.Printf("gRPC: MarkAllAsRead - user_id=%d", req.UserId)
	err := s.repo.MarkAllAsRead(ctx, req.UserId)
	if err != nil {
		log.Printf("❌ MarkAllAsRead error: %v", err)
		return nil, err
	}
	return &notificationv1.MarkAllAsReadResponse{Message: "ok"}, nil
}

func (s *NotificationServer) GetUnreadCount(ctx context.Context, req *notificationv1.GetUnreadCountRequest) (*notificationv1.GetUnreadCountResponse, error) {
	log.Printf("gRPC: GetUnreadCount - user_id=%d", req.UserId)
	count, err := s.repo.UnreadCount(ctx, req.UserId)
	if err != nil {
		log.Printf("❌ GetUnreadCount error: %v", err)
		return nil, err
	}
	return &notificationv1.GetUnreadCountResponse{Count: count}, nil
}
