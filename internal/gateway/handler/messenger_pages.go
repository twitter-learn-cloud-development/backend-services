package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	messengerv1 "twitter-clone/api/messenger/v1"
	userv1 "twitter-clone/api/user/v1"
)

func parseMessengerPageQuery(c *gin.Context) (int32, string, bool) {
	query := c.Request.URL.Query()
	limitText := "20"
	cursor := ""
	if values, ok := query["limit"]; ok {
		if len(values) != 1 {
			c.JSON(400, gin.H{"error": "invalid limit"})
			return 0, "", false
		}
		limitText = values[0]
	}
	if values, ok := query["cursor"]; ok {
		if len(values) != 1 || len(values[0]) > 65 {
			c.JSON(400, gin.H{"error": "invalid cursor"})
			return 0, "", false
		}
		cursor = values[0]
	}
	limit, err := strconv.ParseInt(limitText, 10, 32)
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid limit"})
		return 0, "", false
	}
	return int32(limit), cursor, true
}

func writeMessengerPageError(c *gin.Context, err error) {
	code := http.StatusInternalServerError
	switch status.Code(err) {
	case codes.InvalidArgument:
		code = 400
	case codes.FailedPrecondition, codes.Unavailable:
		code = 503
	case codes.DeadlineExceeded:
		code = 504
	}
	c.JSON(code, gin.H{"error": "failed to read message history"})
}

func (h *MessengerHandler) messengerPageProfiles(ctx context.Context, ids []uint64) map[uint64]*userv1.User {
	profiles := make(map[uint64]*userv1.User)
	if h.userClient == nil {
		return profiles
	}
	seen := make(map[uint64]bool)
	unique := make([]uint64, 0, len(ids))
	for _, id := range ids {
		if id != 0 && !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	// Keep profile hydration bounded to 100 unique IDs per batch.
	for start := 0; start < len(unique); start += 100 {
		end := start + 100
		if end > len(unique) {
			end = len(unique)
		}
		response, err := h.userClient.GetBatchUsers(ctx, &userv1.GetBatchUsersRequest{UserIds: unique[start:end]})
		if err != nil || response == nil {
			continue
		}
		for _, user := range response.Users {
			if user != nil && seen[user.Id] {
				profiles[user.Id] = user
			}
		}
	}
	return profiles
}

func messengerPublicProfile(id uint64, profiles map[uint64]*userv1.User) gin.H {
	profile := gin.H{"id": strconv.FormatUint(id, 10), "username": "unknown", "avatar": ""}
	if user := profiles[id]; user != nil {
		profile["username"] = user.Username
		profile["nickname"] = user.Username
		profile["avatar"] = user.Avatar
	}
	return profile
}

func messengerPageMessage(message *messengerv1.Message, profiles map[uint64]*userv1.User) gin.H {
	result := formatMessage(message)
	if result != nil {
		result["sender"] = messengerPublicProfile(message.SenderId, profiles)
	}
	return result
}
