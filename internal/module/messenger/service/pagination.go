package service

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"twitter-clone/internal/domain"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const maxMessengerPageSize = 100

func messengerPageLimit(limit int32) int {
	if limit <= 0 {
		return 20
	}
	if limit > maxMessengerPageSize {
		return maxMessengerPageSize
	}
	return int(limit)
}

func parseMessageCursor(cursor string) (uint64, error) {
	if cursor == "" || cursor == "0" {
		return 0, nil
	}
	id, err := strconv.ParseUint(cursor, 10, 64)
	if err != nil || id == 0 || strconv.FormatUint(id, 10) != cursor {
		return 0, status.Error(codes.InvalidArgument, "invalid message cursor")
	}
	return id, nil
}

func parseConversationCursor(cursor string, userID uint64) (uint64, uint64, error) {
	if cursor == "" || cursor == "0" {
		return 0, 0, nil
	}
	if len(cursor) > 65 {
		return 0, 0, status.Error(codes.InvalidArgument, "invalid conversation cursor")
	}
	parts := strings.Split(cursor, ":")
	if len(parts) != 4 || parts[0] != "c1" || parts[1] != strconv.FormatUint(userID, 10) {
		return 0, 0, status.Error(codes.InvalidArgument, "invalid conversation cursor")
	}
	snapshot, e1 := parseMessageCursor(parts[2])
	before, e2 := parseMessageCursor(parts[3])
	if e1 != nil || e2 != nil || snapshot == 0 || before == 0 || before > snapshot {
		return 0, 0, status.Error(codes.InvalidArgument, "invalid conversation cursor")
	}
	return snapshot, before, nil
}

func validMessageParties(message *domain.Message, userID, peerID uint64) bool {
	return message != nil && message.ID != 0 && message.ConversationID == getConversationID(userID, peerID) &&
		((message.SenderID == userID && message.ReceiverID == peerID) || (message.SenderID == peerID && message.ReceiverID == userID))
}

func validateMessagePage(messages []*domain.Message, userID, peerID, cursor uint64, limit int) error {
	if len(messages) > limit {
		return status.Error(codes.DataLoss, "invalid message page")
	}
	previous := cursor
	for _, message := range messages {
		if !validMessageParties(message, userID, peerID) || (previous > 0 && message.ID >= previous) {
			return status.Error(codes.DataLoss, "invalid message page")
		}
		previous = message.ID
	}
	return nil
}

func validateConversationPage(page domain.ConversationPage, userID, snapshot, before uint64, limit int) error {
	if len(page.Conversations) > limit || (snapshot > 0 && page.SnapshotID != snapshot) || (len(page.Conversations) > 0 && page.SnapshotID == 0) {
		return status.Error(codes.DataLoss, "invalid conversation page")
	}
	previous := before
	seen := make(map[uint64]bool, len(page.Conversations))
	for _, conversation := range page.Conversations {
		if conversation == nil || conversation.PeerID == 0 || conversation.UnreadCount < 0 || seen[conversation.PeerID] ||
			!validMessageParties(conversation.LatestMessage, userID, conversation.PeerID) ||
			conversation.LatestMessage.ID > page.SnapshotID || (previous > 0 && conversation.LatestMessage.ID >= previous) {
			return status.Error(codes.DataLoss, "invalid conversation page")
		}
		seen[conversation.PeerID] = true
		previous = conversation.LatestMessage.ID
	}
	return nil
}

func encodeConversationCursor(userID, snapshot, before uint64) string {
	return "c1:" + strconv.FormatUint(userID, 10) + ":" + strconv.FormatUint(snapshot, 10) + ":" + strconv.FormatUint(before, 10)
}

func messengerReadError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return status.FromContextError(ctx.Err()).Err()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return status.FromContextError(err).Err()
	}
	return status.Error(codes.Unavailable, "message history is unavailable")
}
