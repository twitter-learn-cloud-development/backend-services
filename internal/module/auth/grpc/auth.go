package grpc

import (
	authV1 "twitter-clone/api/auth/v1"
	userv1 "twitter-clone/api/user/v1"
	"twitter-clone/internal/module/auth/service"
	"twitter-clone/pkg/logger"

	"context"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type AuthService struct {
	authV1.UnimplementedAuthServiceServer
	userClient userv1.UserServiceClient
	jwtConfig  *service.JWTConfig
}

func NewAuthService(userClient userv1.UserServiceClient, cfg *service.JWTConfig) (*AuthService, error) {
	go func() {
		gin.SetMode(gin.ReleaseMode)
		r := gin.Default()

		r.GET("/.well-known/jwks.json", func(c *gin.Context) {
			// 💡 看到了吗？！这里直接白嫖入参里的 cfg，完全不需要任何多余的转换！
			jwks := service.GetPublicJWKS(cfg.PrivateKey, cfg.Kid)
			c.JSON(200, jwks)
		})

		// 启动监听 8081
		if err := r.Run(":8081"); err != nil {
			// 这里可以通过日志记录
			logger.Error(context.Background(), "JWKS server error", zap.Error(err))
		}
	}()

	// 返回组装好的成品 gRPC 服务
	return &AuthService{
		userClient: userClient,
		jwtConfig:  cfg,
	}, nil
}

func (a *AuthService) Login(ctx context.Context, req *authV1.LoginRequest) (*authV1.LoginResponse, error) {
	logger.Info(ctx, "gRPC: Auth Login start", zap.String("email", req.Email))

	//远程调用user服务
	userResponse, err := a.userClient.Login(ctx, &userv1.LoginRequest{
		Email:    req.Email,
		Password: req.Password,
	})
	logger.Info(ctx, "gRPC: Auth Login finished", zap.Error(err))
	if err != nil {
		return nil, err
	}

	token, err := service.GenerateToken(a.jwtConfig, a.jwtConfig.PrivateKey, a.jwtConfig.Kid, userResponse.User.Id, userResponse.User.Username, userResponse.User.Email)
	if err != nil {
		return nil, err
	}

	return &authV1.LoginResponse{
		Token: token,
		User: &authV1.User{
			Id:        userResponse.User.Id,
			Username:  userResponse.User.Username,
			Email:     userResponse.User.Email,
			Avatar:    userResponse.User.Avatar,
			Bio:       userResponse.User.Bio,
			CreatedAt: userResponse.User.CreatedAt,
			UpdatedAt: userResponse.User.UpdatedAt,
			CoverUrl:  userResponse.User.CoverUrl,
			Website:   userResponse.User.Website,
			Location:  userResponse.User.Location,
		},
	}, nil
}

func (a *AuthService) Register(ctx context.Context, req *authV1.RegisterRequest) (*authV1.RegisterResponse, error) {
	//远程调用user服务
	userResponse, err := a.userClient.Register(ctx, &userv1.RegisterRequest{
		Username: req.Username,
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		return nil, err
	}
	return &authV1.RegisterResponse{
		User: &authV1.User{
			Id:        userResponse.User.Id,
			Username:  userResponse.User.Username,
			Email:     userResponse.User.Email,
			Avatar:    userResponse.User.Avatar,
			Bio:       userResponse.User.Bio,
			CreatedAt: userResponse.User.CreatedAt,
			UpdatedAt: userResponse.User.UpdatedAt,
			CoverUrl:  userResponse.User.CoverUrl,
			Website:   userResponse.User.Website,
			Location:  userResponse.User.Location,
		},
	}, nil
}

func (a *AuthService) ChangePassword(ctx context.Context, req *authV1.ChangePasswordRequest) (*authV1.ChangePasswordResponse, error) {
	//远程调用user服务
	userResponse, err := a.userClient.ChangePassword(ctx, &userv1.ChangePasswordRequest{
		UserId:      req.UserId,
		OldPassword: req.OldPassword,
		NewPassword: req.NewPassword,
	})
	if err != nil {
		return nil, err
	}
	return &authV1.ChangePasswordResponse{
		Message: userResponse.Message,
	}, nil
}

//TODO: logout,待user远程服务拥有此服务后进行开发
//func (a *AuthService) Logout(ctx context.Context, req *authV1.LogoutRequest) (*authV1.LogoutResponse, error) {}
