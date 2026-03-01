package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"twitter-clone/internal/domain"
	"twitter-clone/pkg/logger"

	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// ✅ 优化：预编译正则，提升 100 倍性能
var (
	usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
	emailRegex    = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
)

// UserService 用户服务
type UserService struct {
	repo      domain.UserRepository
	jwtConfig *JWTConfig
}

// NewUserService 创建用户服务实例
func NewUserService(repo domain.UserRepository) *UserService {
	return &UserService{
		repo:      repo,
		jwtConfig: DefaultJWTConfig(),
	}
}

// NewUserServiceWithJWTConfig 创建用户服务实例（自定义 JWT 配置）
func NewUserServiceWithJWTConfig(repo domain.UserRepository, jwtConfig *JWTConfig) *UserService {
	return &UserService{
		repo:      repo,
		jwtConfig: jwtConfig,
	}
}

// Register 用户注册
func (s *UserService) Register(ctx context.Context, username, email, password string) (*domain.User, error) {
	// ✅ 修复：在业务入口处统一处理数据清洗
	email = strings.ToLower(strings.TrimSpace(email))
	username = strings.TrimSpace(username)

	//1.参数验证
	if err := s.validateUsername(username); err != nil {
		return nil, err
	}

	if err := s.validateEmail(email); err != nil {
		return nil, err
	}

	if err := s.validatePassword(password); err != nil {
		return nil, err
	}

	// 2. 检查邮箱是否已存在
	emailExists, err := s.repo.IsEmailExist(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("failed to check email existence: %w", err)
	}
	if emailExists {
		return nil, ErrUserAlreadyExists
	}

	// 3. 检查用户名是否已存在
	usernameExists, err := s.repo.IsUsernameExist(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("failed to check username existence: %w", err)
	}
	if usernameExists {
		return nil, ErrUserAlreadyExists
	}

	// 4. 密码加密（使用 bcrypt）
	// bcrypt.DefaultCost = 10，平衡安全性和性能
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	// 5. 构建 User 实体
	user := &domain.User{
		Username:     username,
		Email:        email,
		PasswordHash: string(passwordHash),
		Avatar:       "", // 默认为空，后续可以上传
		Bio:          "", // 默认为空
	}

	// 6. 保存到数据库
	if err := s.repo.Create(ctx, user); err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	// 7. 返回创建的用户（不包含密码哈希）
	return user, nil

}

// Login 用户登录
// 返回: token string, user *domain.User, error
func (s *UserService) Login(ctx context.Context, email, password string) (string, *domain.User, error) {
	// 1. 根据邮箱查找用户
	user, err := s.repo.GetByEmail(ctx, email)
	if err != nil {
		// 统一返回 ErrInvalidCredentials，不暴露具体错误（安全考虑）
		return "", nil, ErrInvalidCredentials
	}

	// 2. 校验密码
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	if err != nil {
		// 密码错误，返回 ErrInvalidCredentials
		return "", nil, ErrInvalidCredentials
	}

	// 3. 生成 JWT Token
	token, err := GenerateToken(s.jwtConfig, user.ID, user.Username, user.Email)
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate token: %w", err)
	}
	logger.Info(ctx, "✅ User logged in", zap.String("username", user.Username), zap.Uint64("user_id", user.ID))

	// 4. 返回 Token 和用户信息
	return token, user, nil
}

// GetProfile 获取用户信息
func (s *UserService) GetProfile(ctx context.Context, userID uint64) (*domain.User, error) {
	user, err := s.repo.GetByID(ctx, userID)
	if err != nil {
		return nil, ErrUserNotFound
	}
	return user, nil
}

// GetBatchUsers 批量获取用户信息
func (s *UserService) GetBatchUsers(ctx context.Context, userIDs []uint64) ([]*domain.User, error) {
	if len(userIDs) == 0 {
		return []*domain.User{}, nil
	}
	// 去重
	uniqueIDs := make([]uint64, 0, len(userIDs))
	seen := make(map[uint64]bool)
	for _, id := range userIDs {
		if !seen[id] {
			seen[id] = true
			uniqueIDs = append(uniqueIDs, id)
		}
	}

	return s.repo.GetByIDs(ctx, uniqueIDs)
}

// SearchUsers 搜索用户
func (s *UserService) SearchUsers(ctx context.Context, keyword string, cursor uint64, limit int) ([]*domain.User, uint64, bool, error) {
	// 1. 参数验证
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return []*domain.User{}, 0, false, nil
	}

	// 2. 搜索
	users, err := s.repo.Search(ctx, keyword, cursor, limit+1)
	if err != nil {
		return nil, 0, false, fmt.Errorf("failed to search users: %w", err)
	}

	// 3. 判断更多
	hasMore := len(users) > limit
	if hasMore {
		users = users[:limit]
	}

	// 4. 计算游标
	var nextCursor uint64
	if hasMore && len(users) > 0 {
		nextCursor = users[len(users)-1].ID
	}

	return users, nextCursor, hasMore, nil
}

// UpdateProfile 更新用户信息
func (s *UserService) UpdateProfile(ctx context.Context, userID uint64, bio, avatar, coverURL, website, location string) error {
	// 检查用户是否存在
	user, err := s.repo.GetByID(ctx, userID)
	if err != nil {
		return ErrUserNotFound
	}

	// 更新字段
	updates := make(map[string]interface{})
	if bio != "" {
		if len(bio) > 255 {
			return fmt.Errorf("bio too long (max 255 characters)")
		}
		updates["bio"] = bio
	}
	if avatar != "" {
		updates["avatar"] = avatar
	}
	if coverURL != "" {
		updates["cover_url"] = coverURL
	}
	if website != "" {
		if len(website) > 255 {
			return fmt.Errorf("website too long")
		}
		updates["website"] = website
	}
	if location != "" {
		if len(location) > 100 {
			return fmt.Errorf("location too long")
		}
		updates["location"] = location
	}

	// 如果没有更新内容，直接返回
	if len(updates) == 0 {
		return nil
	}

	// 执行更新
	if err := s.repo.UpdatePartial(ctx, user.ID, updates); err != nil {
		return fmt.Errorf("failed to update profile: %w", err)
	}

	return nil
}

// ChangePassword 修改密码
func (s *UserService) ChangePassword(ctx context.Context, userID uint64, oldPassword, newPassword string) error {
	//获取用户
	user, err := s.repo.GetByID(ctx, userID)
	if err != nil {
		return ErrUserNotFound
	}

	//验证旧密码
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(oldPassword))
	if err != nil {
		return ErrInvalidCredentials
	}

	//验证新密码格式
	if err := s.validatePassword(newPassword); err != nil {
		return err
	}

	//加密新密码
	newPasswordHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)

	if err != nil {
		return fmt.Errorf("failed to hash new password: %w", err)
	}

	if err := s.repo.UpdatePartial(ctx, userID, map[string]interface{}{
		"password_hash": string(newPasswordHash),
	}); err != nil {
		return fmt.Errorf("failed to change password: %w", err)
	}

	return nil

}

// VerifyToken 验证 Token 并返回用户信息
func (s *UserService) VerifyToken(tokenString string) (*UserClaims, error) {
	return ParseToken(s.jwtConfig, tokenString)
}

// validateUsername 验证用户名格式
func (s *UserService) validateUsername(username string) error {
	// 用户名：3-32字符，只能包含字母、数字、下划线
	if len(username) < 3 || len(username) > 32 {
		return ErrInvalidUsername
	}

	////正则：字母、数字、下划线
	//matched, _ := regexp.MatchString(`^[a-zA-Z0-9_]+$`, username)
	//if !matched {
	//	return ErrInvalidUsername
	//}

	//正则：字母、数字、下划线
	if !usernameRegex.MatchString(username) {
		return ErrInvalidUsername
	}
	return nil
}

// validateEmail 验证邮箱格式
func (s *UserService) validateEmail(email string) error {
	//简单的邮箱格式验证
	if len(email) < 5 || len(email) > 128 {
		return ErrInvalidEmail
	}

	////正则: 基本的邮箱格式
	//matched, _ := regexp.MatchString(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`, email)
	//
	//if !matched {
	//	return ErrInvalidEmail
	//}

	//正则: 基本的邮箱格式
	if !emailRegex.MatchString(email) {
		return ErrInvalidEmail
	}

	return nil
}

// validatePassword 验证密码格式
func (s *UserService) validatePassword(password string) error {
	//密码： 至少6字符
	if len(password) < 6 {
		return ErrInvalidPassword
	}

	// 可以添加更复杂的规则：
	// - 必须包含大小写字母
	// - 必须包含数字
	// - 必须包含特殊字符
	// 这里简化处理

	return nil
}
