// internal/module/auth/wire_service.go
//go:build wireinject
// +build wireinject

package auth

import (
	userv1 "twitter-clone/api/user/v1"
	"twitter-clone/internal/module/auth/grpc"

	"github.com/google/wire"
)

var ProviderSet = wire.NewSet(
	ProvideJWTConfig,
	grpc.NewAuthService,
)

func InitAuthServer(cfg *RawConsulConfig, userClient userv1.UserServiceClient) (*grpc.AuthService, error) {
	wire.Build(
		ProviderSet,
	)
	return nil, nil
}
