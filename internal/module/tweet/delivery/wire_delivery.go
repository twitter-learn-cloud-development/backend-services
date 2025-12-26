package delivery

import "github.com/google/wire"

// ProviderHandlerSet 是 tweet delivery 的 Wire ProviderHandler Set
var ProviderHandlerSet = wire.NewSet(
	NewTweetHandler,
)
