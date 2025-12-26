package delivery

// CreateTweetRequest 发布推文请求
type CreateTweetRequest struct {
	Content   string   `test_data:"content" binding:"required,max=280"`
	MediaURLs []string `test_data:"media_urls" binding:"omitempty,max=4,dive,url"`
}

// TweetResponse 推文响应
type TweetResponse struct {
	ID           uint64   `test_data:"id"`
	UserID       uint64   `test_data:"user_id"`
	Content      string   `test_data:"content"`
	MediaURLs    []string `test_data:"media_urls"`
	Type         int      `test_data:"type"`
	VisibleType  int      `test_data:"visible_type"`
	LikeCount    int      `test_data:"like_count"`
	CommentCount int      `test_data:"comment_count"`
	ShareCount   int      `test_data:"share_count"`
	IsLiked      bool     `test_data:"is_liked"`
	CreatedAt    int64    `test_data:"created_at"`
}

// TimelineResponse 时间线响应
type TimelineResponse struct {
	Tweets     []*TweetResponse `test_data:"tweets"`
	NextCursor uint64           `test_data:"next_cursor"`
	HasMore    bool             `test_data:"has_more"`
}
