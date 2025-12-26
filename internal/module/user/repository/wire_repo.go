package repository

import "github.com/google/wire"

// ProviderRepoSet 是 user repository 的 Wire ProviderRepo Set
var ProviderRepoSet = wire.NewSet(
	NewUserRepository,
)
