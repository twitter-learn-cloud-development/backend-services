package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	agentCredential "twitter-clone/internal/module/agent/credential"
	"twitter-clone/internal/module/agent/repository"
	agentWebSearch "twitter-clone/internal/module/agent/websearch"
	"twitter-clone/internal/module/agent/workflow/tool"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

var providerNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

type ProviderConfigInput struct {
	Kind     string
	Name     string
	Provider string
	BaseURL  string
	Model    string
	APIKey   string
	Revision int64
}

type ProviderConfigView struct {
	ID                string
	UserID            uint64
	Kind              string
	Name              string
	Provider          string
	BaseURL           string
	Model             string
	Status            string
	HasSecret         bool
	CredentialVersion int64
	Revision          int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (s *AgentService) CreateProviderConfig(ctx context.Context, userID uint64, input ProviderConfigInput) (*ProviderConfigView, error) {
	repo, err := s.providerConfigRepository()
	if err != nil {
		return nil, err
	}
	input, err = s.validateProviderConfigInput(input, true)
	if err != nil {
		return nil, err
	}
	config := &repository.ProviderConfig{
		ID: primitive.NewObjectID(), UserID: userID,
		Kind: input.Kind, Name: input.Name, Provider: input.Provider, BaseURL: input.BaseURL, Model: input.Model,
		Status: repository.ProviderConfigStatusActive, CredentialVersion: 1, Revision: 1,
	}
	if err := s.sealProviderConfig(config, []byte(input.APIKey)); err != nil {
		return nil, err
	}
	if err := repo.CreateProviderConfig(ctx, config); err != nil {
		return nil, err
	}
	return providerConfigView(config), nil
}

func (s *AgentService) UpdateProviderConfig(
	ctx context.Context,
	userID uint64,
	configID string,
	input ProviderConfigInput,
) (*ProviderConfigView, error) {
	repo, err := s.providerConfigRepository()
	if err != nil {
		return nil, err
	}
	id, err := primitive.ObjectIDFromHex(strings.TrimSpace(configID))
	if err != nil {
		return nil, fmt.Errorf("invalid provider_config_id: %w", err)
	}
	existing, err := repo.GetProviderConfig(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	if existing.Status != repository.ProviderConfigStatusActive {
		return nil, errors.New("provider config is revoked")
	}
	requestedKind := strings.ToLower(strings.TrimSpace(input.Kind))
	if requestedKind == "" {
		input.Kind = providerConfigKind(existing)
	}
	input, err = s.validateProviderConfigInput(input, false)
	if err != nil {
		return nil, err
	}
	if input.Kind != providerConfigKind(existing) {
		return nil, errors.New("provider config kind cannot be changed")
	}
	if existing.Provider != input.Provider && strings.TrimSpace(input.APIKey) == "" {
		return nil, errors.New("new API key is required when provider changes")
	}
	plaintext := []byte(input.APIKey)
	if len(plaintext) == 0 && existing.HasSecret {
		plaintext, err = s.openProviderConfig(existing)
		if err != nil {
			return nil, err
		}
	}
	expectedRevision := input.Revision
	if expectedRevision <= 0 {
		expectedRevision = existing.Revision
	}
	providerChanged := existing.Provider != input.Provider || existing.BaseURL != input.BaseURL
	existing.Kind = input.Kind
	existing.Name = input.Name
	existing.Provider = input.Provider
	existing.BaseURL = input.BaseURL
	existing.Model = input.Model
	if input.APIKey != "" || providerChanged {
		existing.CredentialVersion++
	}
	if err := s.sealProviderConfig(existing, plaintext); err != nil {
		return nil, err
	}
	if err := repo.UpdateProviderConfig(ctx, existing, expectedRevision); err != nil {
		return nil, err
	}
	return providerConfigView(existing), nil
}

func (s *AgentService) ListProviderConfigs(ctx context.Context, userID uint64, page, pageSize int) ([]*ProviderConfigView, int64, error) {
	return s.ListProviderConfigsByKind(ctx, userID, "", page, pageSize)
}

func (s *AgentService) ListProviderConfigsByKind(
	ctx context.Context,
	userID uint64,
	kind string,
	page, pageSize int,
) ([]*ProviderConfigView, int64, error) {
	repo, err := s.providerConfigRepository()
	if err != nil {
		return nil, 0, err
	}
	kind, err = normalizeProviderConfigKind(kind, true)
	if err != nil {
		return nil, 0, err
	}
	var configs []*repository.ProviderConfig
	var total int64
	if kindRepo, ok := repo.(repository.ProviderConfigKindRepository); ok {
		configs, total, err = kindRepo.ListProviderConfigsByKind(ctx, userID, kind, page, pageSize)
	} else {
		configs, total, err = repo.ListProviderConfigs(ctx, userID, page, pageSize)
		if err == nil && kind != "" {
			filtered := configs[:0]
			for _, config := range configs {
				if providerConfigKind(config) == kind {
					filtered = append(filtered, config)
				}
			}
			configs = filtered
			total = int64(len(filtered))
		}
	}
	if err != nil {
		return nil, 0, err
	}
	views := make([]*ProviderConfigView, 0, len(configs))
	for _, config := range configs {
		views = append(views, providerConfigView(config))
	}
	return views, total, nil
}

func (s *AgentService) GetProviderConfig(ctx context.Context, userID uint64, configID string) (*ProviderConfigView, error) {
	config, err := s.getProviderConfig(ctx, userID, configID)
	if err != nil {
		return nil, err
	}
	return providerConfigView(config), nil
}

func (s *AgentService) RevokeProviderConfig(ctx context.Context, userID uint64, configID string, revision int64) error {
	repo, err := s.providerConfigRepository()
	if err != nil {
		return err
	}
	config, err := s.getProviderConfig(ctx, userID, configID)
	if err != nil {
		return err
	}
	if config.Status != repository.ProviderConfigStatusActive {
		return errors.New("provider config is already revoked")
	}
	if revision <= 0 {
		revision = config.Revision
	}
	return repo.RevokeProviderConfig(ctx, config.ID, userID, revision)
}

func (s *AgentService) ResolveWebSearchProvider(
	ctx context.Context,
	subject agentWebSearch.AccessSubject,
	configID string,
) (agentWebSearch.ResolvedProvider, error) {
	if subject.UserID == 0 {
		return agentWebSearch.ResolvedProvider{}, agentWebSearch.ErrAccessIdentityRequired
	}
	if s == nil || s.webSearchProviderFactory == nil {
		return agentWebSearch.ResolvedProvider{}, agentWebSearch.ErrUnavailable
	}
	config, err := s.getProviderConfig(ctx, subject.UserID, configID)
	if err != nil {
		return agentWebSearch.ResolvedProvider{}, err
	}
	if config.Status != repository.ProviderConfigStatusActive {
		return agentWebSearch.ResolvedProvider{}, errors.New("web search provider config is revoked")
	}
	if providerConfigKind(config) != repository.ProviderConfigKindWebSearch ||
		!agentWebSearch.IsSupportedProvider(config.Provider) {
		return agentWebSearch.ResolvedProvider{}, errors.New("provider config is not a supported web search configuration")
	}
	if !config.HasSecret {
		return agentWebSearch.ResolvedProvider{}, errors.New("web search provider credential is unavailable")
	}
	plaintext, err := s.openProviderConfig(config)
	if err != nil {
		return agentWebSearch.ResolvedProvider{}, err
	}
	provider, err := s.webSearchProviderFactory.NewFor(config.Provider, config.BaseURL, string(plaintext))
	for index := range plaintext {
		plaintext[index] = 0
	}
	if err != nil {
		return agentWebSearch.ResolvedProvider{}, err
	}
	return agentWebSearch.ResolvedProvider{
		Provider: provider,
		CacheScope: fmt.Sprintf(
			"user:%d:config:%s:revision:%d:credential:%d",
			subject.UserID,
			config.ID.Hex(),
			config.Revision,
			config.CredentialVersion,
		),
	}, nil
}

func (s *AgentService) ResolveWorkflowProviderConfig(
	ctx context.Context,
	userID uint64,
	configID string,
) (tool.ResolvedProviderConfig, error) {
	config, err := s.getProviderConfig(ctx, userID, configID)
	if err != nil {
		return tool.ResolvedProviderConfig{}, err
	}
	if config.Status != repository.ProviderConfigStatusActive {
		return tool.ResolvedProviderConfig{}, errors.New("provider config is revoked")
	}
	if providerConfigKind(config) != repository.ProviderConfigKindLLM {
		return tool.ResolvedProviderConfig{}, errors.New("provider config is not an LLM configuration")
	}
	apiKey := ""
	if config.HasSecret {
		plaintext, err := s.openProviderConfig(config)
		if err != nil {
			return tool.ResolvedProviderConfig{}, err
		}
		apiKey = string(plaintext)
	}
	return tool.ResolvedProviderConfig{
		Provider: config.Provider, BaseURL: config.BaseURL, Model: config.Model, APIKey: apiKey,
	}, nil
}

func (s *AgentService) providerConfigRepository() (repository.ProviderConfigRepository, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("agent repository is not initialized")
	}
	repo, ok := s.repo.(repository.ProviderConfigRepository)
	if !ok {
		return nil, errors.New("provider config repository is not available")
	}
	return repo, nil
}

func (s *AgentService) getProviderConfig(ctx context.Context, userID uint64, configID string) (*repository.ProviderConfig, error) {
	repo, err := s.providerConfigRepository()
	if err != nil {
		return nil, err
	}
	id, err := primitive.ObjectIDFromHex(strings.TrimSpace(configID))
	if err != nil {
		return nil, fmt.Errorf("invalid provider_config_id: %w", err)
	}
	return repo.GetProviderConfig(ctx, id, userID)
}

func (s *AgentService) validateProviderConfigInput(input ProviderConfigInput, creating bool) (ProviderConfigInput, error) {
	var err error
	input.Kind, err = normalizeProviderConfigKind(input.Kind, false)
	if err != nil {
		return input, err
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Provider = strings.ToLower(strings.TrimSpace(input.Provider))
	input.BaseURL = strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
	input.Model = strings.TrimSpace(input.Model)
	if input.Name == "" || len([]rune(input.Name)) > 80 {
		return input, errors.New("provider config name must contain 1-80 characters")
	}
	if !providerNamePattern.MatchString(input.Provider) {
		return input, errors.New("provider must match [a-z0-9._-] and contain at most 64 characters")
	}
	if len(input.APIKey) > 8192 {
		return input, errors.New("provider API key is too long")
	}
	switch input.Kind {
	case repository.ProviderConfigKindWebSearch:
		defaultBaseURL, supported := agentWebSearch.DefaultProviderBaseURL(input.Provider)
		if !supported {
			return input, errors.New("web search provider must be brave or qianfan")
		}
		if input.Model != "" {
			return input, errors.New("web search provider config must not define a model")
		}
		if input.BaseURL == "" {
			input.BaseURL = defaultBaseURL
		}
		if creating && input.APIKey == "" {
			return input, errors.New("web search provider API key is required")
		}
		if s.webSearchProviderFactory == nil {
			return input, errors.New("web search provider configuration is disabled")
		}
		if err := s.webSearchProviderFactory.ValidateEndpointFor(input.Provider, input.BaseURL); err != nil {
			return input, fmt.Errorf("validate web search base_url: %w", err)
		}
	case repository.ProviderConfigKindLLM:
		if input.Model == "" || len(input.Model) > 200 {
			return input, errors.New("provider model must contain 1-200 characters")
		}
		if creating && input.APIKey == "" && !isLocalProvider(input.Provider) {
			return input, errors.New("provider API key is required for non-local providers")
		}
		if s.providerEndpointPolicy == nil {
			return input, errors.New("provider endpoint policy is not configured")
		}
		if err := s.providerEndpointPolicy.Validate(input.BaseURL, input.Provider); err != nil {
			return input, fmt.Errorf("validate provider base_url: %w", err)
		}
	}
	return input, nil
}

func (s *AgentService) sealProviderConfig(config *repository.ProviderConfig, plaintext []byte) error {
	if len(plaintext) == 0 {
		if !isLocalProvider(config.Provider) {
			return errors.New("provider API key is required for non-local providers")
		}
		config.HasSecret = false
		config.EncryptionKeyID = ""
		config.SecretNonce = ""
		config.EncryptedAPIKey = ""
		return nil
	}
	if s.providerConfigCipher == nil {
		return agentCredential.ErrSecretCipherUnavailable
	}
	secret, err := s.providerConfigCipher.Encrypt(plaintext, providerConfigAAD(config))
	if err != nil {
		return fmt.Errorf("encrypt provider API key: %w", err)
	}
	config.HasSecret = true
	config.EncryptionKeyID = secret.KeyID
	config.SecretNonce = secret.Nonce
	config.EncryptedAPIKey = secret.Ciphertext
	return nil
}

func (s *AgentService) openProviderConfig(config *repository.ProviderConfig) ([]byte, error) {
	if s.providerConfigCipher == nil {
		return nil, agentCredential.ErrSecretCipherUnavailable
	}
	plaintext, err := s.providerConfigCipher.Decrypt(agentCredential.EncryptedSecret{
		KeyID: config.EncryptionKeyID, Nonce: config.SecretNonce, Ciphertext: config.EncryptedAPIKey,
	}, providerConfigAAD(config))
	if err != nil {
		return nil, fmt.Errorf("decrypt provider API key: %w", err)
	}
	return plaintext, nil
}

func providerConfigAAD(config *repository.ProviderConfig) []byte {
	return []byte(fmt.Sprintf("%d:%s:%s", config.UserID, config.ID.Hex(), config.Provider))
}

func providerConfigView(config *repository.ProviderConfig) *ProviderConfigView {
	return &ProviderConfigView{
		ID: config.ID.Hex(), UserID: config.UserID, Kind: providerConfigKind(config), Name: config.Name, Provider: config.Provider,
		BaseURL: config.BaseURL, Model: config.Model, Status: config.Status,
		HasSecret: config.HasSecret, CredentialVersion: config.CredentialVersion,
		Revision: config.Revision, CreatedAt: config.CreatedAt, UpdatedAt: config.UpdatedAt,
	}
}

func providerConfigKind(config *repository.ProviderConfig) string {
	if config == nil || strings.TrimSpace(config.Kind) == "" {
		return repository.ProviderConfigKindLLM
	}
	return strings.ToLower(strings.TrimSpace(config.Kind))
}

func normalizeProviderConfigKind(kind string, allowEmpty bool) (string, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		if allowEmpty {
			return "", nil
		}
		return repository.ProviderConfigKindLLM, nil
	}
	switch kind {
	case repository.ProviderConfigKindLLM, repository.ProviderConfigKindWebSearch:
		return kind, nil
	default:
		return "", errors.New("provider config kind must be llm or web_search")
	}
}

func isLocalProvider(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "local", "lmstudio", "lm-studio":
		return true
	default:
		return false
	}
}
