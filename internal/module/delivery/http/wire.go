package http

import "github.com/google/wire"

// ProviderHandleSet 是 user http handler 的 Wire ProviderHandle Set
var ProviderHandleSet = wire.NewSet(
	NewUserHandler,
)
