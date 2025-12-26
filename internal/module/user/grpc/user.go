package grpc

import (
	"context"
	"log"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	userv1 "twitter-clone/api/user/v1"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/module/user/service"
)

// UserServer gRPC 服务器
type UserServer struct {
	userv1.UnimplementedUserServiceServer
	svc *service.UserService
}

// NewUserServer 创建 User gRPC 服务器
func NewUserServer(svc *service.UserService) *UserServer {
	return &UserServer{svc: svc}
}

// Register 用户注册
func (s *UserServer) Register(ctx context.Context, req *userv1.RegisterRequest) (*userv1.RegisterResponse, error) {
	log.Printf("gRPC: Register - username=%s, email=%s", req.Username, req.Email)

	// 调用 Service 层
	user, err := s.svc.Register(ctx, req.Username, req.Email, req.Password)
	if err != nil {
		log.Printf("❌ Register error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to register: %v", err)
	}

	// 转换为 Protobuf 消息
	return &userv1.RegisterResponse{
		User: domainUserToProto(user),
	}, nil
}

// Login 用户登录
func (s *UserServer) Login(ctx context.Context, req *userv1.LoginRequest) (*userv1.LoginResponse, error) {
	log.Printf("gRPC: Login - email=%s", req.Email)

	// 调用 Service 层
	token, user, err := s.svc.Login(ctx, req.Email, req.Password)
	if err != nil {
		log.Printf("❌ Login error: %v", err)
		return nil, status.Errorf(codes.Unauthenticated, "login failed: %v", err)
	}

	return &userv1.LoginResponse{
		Token: token,
		User:  domainUserToProto(user),
	}, nil
}

// GetProfile 获取用户资料
func (s *UserServer) GetProfile(ctx context.Context, req *userv1.GetProfileRequest) (*userv1.GetProfileResponse, error) {
	log.Printf("gRPC: GetProfile - user_id=%d", req.UserId)

	user, err := s.svc.GetProfile(ctx, req.UserId)
	if err != nil {
		log.Printf("❌ GetProfile error: %v", err)
		return nil, status.Errorf(codes.NotFound, "user not found: %v", err)
	}

	return &userv1.GetProfileResponse{
		User: domainUserToProto(user),
	}, nil
}

// UpdateProfile 更新用户资料
func (s *UserServer) UpdateProfile(ctx context.Context, req *userv1.UpdateProfileRequest) (*userv1.UpdateProfileResponse, error) {
	log.Printf("gRPC: UpdateProfile - user_id=%d", req.UserId)

	// 调用 Service 层（只返回 error）
	err := s.svc.UpdateProfile(ctx, req.UserId, req.Avatar, req.Bio)
	if err != nil {
		log.Printf("❌ UpdateProfile error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to update profile: %v", err)
	}

	// 更新成功后，重新获取用户资料
	user, err := s.svc.GetProfile(ctx, req.UserId)
	if err != nil {
		log.Printf("❌ GetProfile after update error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to get updated profile: %v", err)
	}

	return &userv1.UpdateProfileResponse{
		User: domainUserToProto(user),
	}, nil
}

// ChangePassword 修改密码
func (s *UserServer) ChangePassword(ctx context.Context, req *userv1.ChangePasswordRequest) (*userv1.ChangePasswordResponse, error) {
	log.Printf("gRPC: ChangePassword - user_id=%d", req.UserId)

	err := s.svc.ChangePassword(ctx, req.UserId, req.OldPassword, req.NewPassword)
	if err != nil {
		log.Printf("❌ ChangePassword error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to change password: %v", err)
	}

	return &userv1.ChangePasswordResponse{
		Message: "password changed successfully",
	}, nil
}

// domainUserToProto 将 Domain User 转换为 Protobuf User
func domainUserToProto(user *domain.User) *userv1.User {
	return &userv1.User{
		Id:        user.ID,
		Username:  user.Username,
		Email:     user.Email,
		Avatar:    user.Avatar,
		Bio:       user.Bio,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
	}
}
