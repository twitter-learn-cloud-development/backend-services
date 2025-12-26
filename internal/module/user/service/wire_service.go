package service

import "github.com/google/wire"

// ProviderServiceSet 是 user service 的 Wire ProviderService Set
var ProviderServiceSet = wire.NewSet(
	NewUserService,
)
