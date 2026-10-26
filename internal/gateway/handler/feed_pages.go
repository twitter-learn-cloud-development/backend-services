package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	tweetv1 "twitter-clone/api/tweet/v1"
	userv1 "twitter-clone/api/user/v1"
)

func (h *TweetHandler) enrichFeedTweets(ctx context.Context, tweets []*tweetv1.Tweet) []gin.H {
	profiles := make(map[uint64]gin.H)
	ids := make([]uint64, 0, len(tweets))
	for _, tweet := range tweets {
		if _, ok := profiles[tweet.UserId]; !ok {
			ids = append(ids, tweet.UserId)
			profiles[tweet.UserId] = gin.H{"id": strconv.FormatUint(tweet.UserId, 10), "username": "unknown", "avatar": ""}
		}
	}
	if len(ids) > 0 && h.userClient != nil {
		response, err := h.userClient.GetBatchUsers(ctx, &userv1.GetBatchUsersRequest{UserIds: ids})
		if err == nil && response != nil {
			for _, user := range response.Users {
				if user != nil {
					if _, ok := profiles[user.Id]; ok {
						profiles[user.Id] = formatUser(user)
					}
				}
			}
		}
	}
	return formatEnrichedTweets(tweets, profiles)
}

func parseFeedPageQuery(c *gin.Context) (uint64, int32, bool) {
	query := c.Request.URL.Query()
	cursorText, limitText := "0", "20"
	if values, ok := query["cursor"]; ok {
		if len(values) != 1 {
			c.JSON(400, gin.H{"error": "invalid cursor"})
			return 0, 0, false
		}
		cursorText = values[0]
	}
	if values, ok := query["limit"]; ok {
		if len(values) != 1 {
			c.JSON(400, gin.H{"error": "invalid limit"})
			return 0, 0, false
		}
		limitText = values[0]
	}
	cursor, err := strconv.ParseUint(cursorText, 10, 64)
	if err != nil || strconv.FormatUint(cursor, 10) != cursorText {
		c.JSON(400, gin.H{"error": "invalid cursor"})
		return 0, 0, false
	}
	limit, err := strconv.ParseInt(limitText, 10, 32)
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid limit"})
		return 0, 0, false
	}
	return cursor, int32(limit), true
}

func writeFeedPageError(c *gin.Context, err error) {
	code := http.StatusInternalServerError
	switch status.Code(err) {
	case codes.InvalidArgument:
		code = 400
	case codes.Unavailable:
		code = 503
	case codes.DeadlineExceeded:
		code = 504
	}
	c.JSON(code, gin.H{"error": "failed to read feed"})
}
