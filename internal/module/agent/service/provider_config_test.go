package service

import (
	"bytes"
	"context"
	"testing"
	"time"

	agentCredential "twitter-clone/internal/module/agent/credential"
	agentModel "twitter-clone/internal/module/agent/model"
	"twitter-clone/internal/module/agent/repository"
	agentWebSearch "twitter-clone/internal/module/agent/websearch"
	"twitter-clone/pkg/ai"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type providerConfigRepoStub struct {
	repository.AgentRepository
	configs map[primitive.ObjectID]repository.ProviderConfig
}

func (repo *providerConfigRepoStub) CreateProviderConfig(_ context.Context, config *repository.ProviderConfig) error {
	copy := *config
	copy.CreatedAt = time.Now()
	copy.UpdatedAt = copy.CreatedAt
	*config = copy
	repo.configs[copy.ID] = copy
	return nil
}

func (repo *providerConfigRepoStub) UpdateProviderConfig(_ context.Context, config *repository.ProviderConfig, expectedRevision int64) error {
	current, ok := repo.configs[config.ID]
	if !ok || current.UserID != config.UserID || current.Revision != expectedRevision {
		return repository.ErrProviderConfigConflict
	}
	copy := *config
	copy.Revision = expectedRevision + 1
	copy.UpdatedAt = time.Now()
	*config = copy
	repo.configs[copy.ID] = copy
	return nil
}

func (repo *providerConfigRepoStub) ListProviderConfigs(_ context.Context, userID uint64, _, _ int) ([]*repository.ProviderConfig, int64, error) {
	configs := make([]*repository.ProviderConfig, 0)
	for _, config := range repo.configs {
		if config.UserID == userID {
			copy := config
			configs = append(configs, &copy)
		}
	}
	return configs, int64(len(configs)), nil
}

func (repo *providerConfigRepoStub) GetProviderConfig(_ context.Context, id primitive.ObjectID, userID uint64) (*repository.ProviderConfig, error) {
	config, ok := repo.configs[id]
	if !ok || config.UserID != userID {
		return nil, repository.ErrProviderConfigNotFound
	}
	copy := config
	return &copy, nil
}

func (repo *providerConfigRepoStub) RevokeProviderConfig(_ context.Context, id primitive.ObjectID, userID uint64, expectedRevision int64) error {
	config, ok := repo.configs[id]
	if !ok || config.UserID != userID || config.Revision != expectedRevision {
		return repository.ErrProviderConfigConflict
	}
	config.Status = repository.ProviderConfigStatusRevoked
	config.HasSecret = false
	config.EncryptionKeyID = ""
	config.SecretNonce = ""
	config.EncryptedAPIKey = ""
	config.Revision++
	repo.configs[id] = config
	return nil
}

func TestProviderConfigEncryptedLifecycleAndTenantIsolation(t *testing.T) {
	repo := &providerConfigRepoStub{configs: make(map[primitive.ObjectID]repository.ProviderConfig)}
	cipher, err := agentCredential.NewAESGCMCipher("v1", map[string][]byte{"v1": bytes.Repeat([]byte{7}, 32)})
	if err != nil {
		t.Fatalf("NewAESGCMCipher() error = %v", err)
	}
	service := NewAgentService(
		"https://api.example.com/v1", "service-key", "service-model", "", repo, &ai.Client{}, nil,
		WithProviderConfigCipher(cipher),
		WithProviderEndpointPolicy(agentModel.NewEndpointPolicy()),
	)
	defer service.Close()

	created, err := service.CreateProviderConfig(context.Background(), 7, ProviderConfigInput{
		Name: "Personal OpenAI", Provider: "openai-compatible",
		BaseURL: "https://api.example.com/v1/", Model: "custom-model", APIKey: "tenant-secret-v1",
	})
	if err != nil {
		t.Fatalf("CreateProviderConfig() error = %v", err)
	}
	id, _ := primitive.ObjectIDFromHex(created.ID)
	stored := repo.configs[id]
	if stored.EncryptedAPIKey == "" || stored.EncryptedAPIKey == "tenant-secret-v1" || stored.SecretNonce == "" || !stored.HasSecret {
		t.Fatalf("stored provider config leaked or omitted secret metadata: %+v", stored)
	}

	resolved, err := service.ResolveWorkflowProviderConfig(context.Background(), 7, created.ID)
	if err != nil || resolved.APIKey != "tenant-secret-v1" || resolved.Model != "custom-model" {
		t.Fatalf("ResolveWorkflowProviderConfig() result/error = %+v/%v", resolved, err)
	}
	if _, err := service.ResolveWorkflowProviderConfig(context.Background(), 8, created.ID); err == nil {
		t.Fatal("ResolveWorkflowProviderConfig() error = nil for another tenant")
	}

	updated, err := service.UpdateProviderConfig(context.Background(), 7, created.ID, ProviderConfigInput{
		Name: "Personal OpenAI", Provider: "openai-compatible",
		BaseURL: "https://api.example.com/v1", Model: "custom-model-v2",
		APIKey: "tenant-secret-v2", Revision: created.Revision,
	})
	if err != nil {
		t.Fatalf("UpdateProviderConfig() error = %v", err)
	}
	if updated.CredentialVersion != 2 || updated.Revision != 2 || repo.configs[id].EncryptedAPIKey == stored.EncryptedAPIKey {
		t.Fatalf("updated provider config = %+v", updated)
	}
	resolved, err = service.ResolveWorkflowProviderConfig(context.Background(), 7, created.ID)
	if err != nil || resolved.APIKey != "tenant-secret-v2" {
		t.Fatalf("rotated ResolveWorkflowProviderConfig() result/error = %+v/%v", resolved, err)
	}

	if err := service.RevokeProviderConfig(context.Background(), 7, created.ID, updated.Revision); err != nil {
		t.Fatalf("RevokeProviderConfig() error = %v", err)
	}
	revoked := repo.configs[id]
	if revoked.HasSecret || revoked.EncryptedAPIKey != "" || revoked.Status != repository.ProviderConfigStatusRevoked {
		t.Fatalf("revoked provider config retained secret: %+v", revoked)
	}
	if _, err := service.ResolveWorkflowProviderConfig(context.Background(), 7, created.ID); err == nil {
		t.Fatal("ResolveWorkflowProviderConfig() error = nil after revocation")
	}
}

func TestWebSearchProviderConfigUsesEncryptedTenantRouting(t *testing.T) {
	repo := &providerConfigRepoStub{configs: make(map[primitive.ObjectID]repository.ProviderConfig)}
	cipher, err := agentCredential.NewAESGCMCipher("v1", map[string][]byte{"v1": bytes.Repeat([]byte{9}, 32)})
	if err != nil {
		t.Fatalf("NewAESGCMCipher() error = %v", err)
	}
	factory, err := agentWebSearch.NewBraveProviderFactory(agentWebSearch.BraveProviderFactoryConfig{
		EndpointPolicy: agentModel.NewEndpointPolicy(),
		MaxConcurrent:  2,
	})
	if err != nil {
		t.Fatalf("NewBraveProviderFactory() error = %v", err)
	}
	service := NewAgentService(
		"https://api.example.com/v1", "service-key", "service-model", "", repo, &ai.Client{}, nil,
		WithProviderConfigCipher(cipher),
		WithProviderEndpointPolicy(agentModel.NewEndpointPolicy()),
		WithWebSearchProviderFactory(factory),
	)
	defer service.Close()

	created, err := service.CreateProviderConfig(context.Background(), 7, ProviderConfigInput{
		Kind: repository.ProviderConfigKindWebSearch,
		Name: "Personal Brave", Provider: agentWebSearch.BraveProviderName,
		BaseURL: agentWebSearch.DefaultBraveBaseURL, APIKey: "tenant-brave-secret",
	})
	if err != nil {
		t.Fatalf("CreateProviderConfig() error = %v", err)
	}
	if created.Kind != repository.ProviderConfigKindWebSearch || created.Model != "" || !created.HasSecret {
		t.Fatalf("created config = %+v", created)
	}
	id, _ := primitive.ObjectIDFromHex(created.ID)
	stored := repo.configs[id]
	if stored.EncryptedAPIKey == "" || stored.EncryptedAPIKey == "tenant-brave-secret" {
		t.Fatalf("stored web provider secret leaked: %+v", stored)
	}
	if _, err := service.ResolveWorkflowProviderConfig(context.Background(), 7, created.ID); err == nil {
		t.Fatal("web provider config was accepted as an LLM provider")
	}
	resolved, err := service.ResolveWebSearchProvider(
		context.Background(),
		agentWebSearch.AccessSubject{UserID: 7, RunID: "run-1"},
		created.ID,
	)
	if err != nil || resolved.Provider == nil || resolved.Provider.Name() != agentWebSearch.BraveProviderName || resolved.CacheScope == "" {
		t.Fatalf("ResolveWebSearchProvider() result/error = %+v/%v", resolved, err)
	}
	if _, err := service.ResolveWebSearchProvider(
		context.Background(),
		agentWebSearch.AccessSubject{UserID: 8, RunID: "run-2"},
		created.ID,
	); err == nil {
		t.Fatal("ResolveWebSearchProvider() error = nil for another tenant")
	}
	configs, total, err := service.ListProviderConfigsByKind(
		context.Background(), 7, repository.ProviderConfigKindWebSearch, 1, 20,
	)
	if err != nil || total != 1 || len(configs) != 1 || configs[0].ID != created.ID {
		t.Fatalf("ListProviderConfigsByKind() configs/total/error = %+v/%d/%v", configs, total, err)
	}
	if _, err := service.UpdateProviderConfig(context.Background(), 7, created.ID, ProviderConfigInput{
		Kind: repository.ProviderConfigKindLLM,
		Name: "Changed Kind", Provider: "openai-compatible",
		BaseURL: "https://api.example.com/v1", Model: "model", Revision: created.Revision,
	}); err == nil {
		t.Fatal("UpdateProviderConfig() allowed kind mutation")
	}
	if err := service.RevokeProviderConfig(context.Background(), 7, created.ID, created.Revision); err != nil {
		t.Fatalf("RevokeProviderConfig() error = %v", err)
	}
	if _, err := service.ResolveWebSearchProvider(
		context.Background(),
		agentWebSearch.AccessSubject{UserID: 7, RunID: "run-3"},
		created.ID,
	); err == nil {
		t.Fatal("ResolveWebSearchProvider() error = nil after revocation")
	}
}
