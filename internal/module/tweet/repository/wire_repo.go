package repository

import "github.com/google/wire"

// ProviderRepoSet 是 tweet repository 的 Wire ProviderRepo Set
var ProviderRepoSet = wire.NewSet(
	NewTweetRepository,
	NewPollRepository,
	NewLikeRepository,    // Assuming these exist based on directory listing
	NewCommentRepository, // Assuming these exist
	NewRetweetRepository, // Assuming these exist
)
