package service

import "github.com/google/wire"

// ProviderServiceSet 是 follow service 的 Wire ProviderService Set
var ProviderServiceSet = wire.NewSet(
	NewFollowService,
)
