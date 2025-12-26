package service

import "github.com/google/wire"

// ProviderServiceSet 是 tweet service 的 Wire ProviderService Set
var ProviderServiceSet = wire.NewSet(
	NewTweetService,
)
