package service

import (
	"bytes"
	"context"
	"testing"

	agentCredential "twitter-clone/internal/module/agent/credential"
	agentModel "twitter-clone/internal/module/agent/model"
	"twitter-clone/internal/module/agent/repository"
	agentWebSearch "twitter-clone/internal/module/agent/websearch"
	"twitter-clone/pkg/ai"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestQianfanWebSearchProviderConfigUsesEncryptedTenantRouting(t *testing.T) {
	repo := &providerConfigRepoStub{configs: make(map[primitive.ObjectID]repository.ProviderConfig)}
	cipher, err := agentCredential.NewAESGCMCipher("v1", map[string][]byte{"v1": bytes.Repeat([]byte{11}, 32)})
	if err != nil {
		t.Fatalf("NewAESGCMCipher() error = %v", err)
	}
	factory, err := agentWebSearch.NewProviderFactory(agentWebSearch.ProviderFactoryConfig{
		EndpointPolicy: agentModel.NewEndpointPolicy(),
		MaxConcurrent:  2,
	})
	if err != nil {
		t.Fatalf("NewProviderFactory() error = %v", err)
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
		Name: "Personal Qianfan", Provider: agentWebSearch.QianfanProviderName,
		APIKey: "tenant-qianfan-secret",
	})
	if err != nil {
		t.Fatalf("CreateProviderConfig() error = %v", err)
	}
	if created.Provider != agentWebSearch.QianfanProviderName ||
		created.BaseURL != agentWebSearch.DefaultQianfanBaseURL ||
		!created.HasSecret {
		t.Fatalf("created config = %+v", created)
	}
	id, err := primitive.ObjectIDFromHex(created.ID)
	if err != nil {
		t.Fatalf("ObjectIDFromHex() error = %v", err)
	}
	stored := repo.configs[id]
	if stored.EncryptedAPIKey == "" || stored.EncryptedAPIKey == "tenant-qianfan-secret" {
		t.Fatalf("stored Qianfan secret leaked: %+v", stored)
	}

	resolved, err := service.ResolveWebSearchProvider(
		context.Background(),
		agentWebSearch.AccessSubject{UserID: 7, RunID: "run-qianfan"},
		created.ID,
	)
	if err != nil || resolved.Provider == nil || resolved.Provider.Name() != agentWebSearch.QianfanProviderName {
		t.Fatalf("ResolveWebSearchProvider() result/error = %+v/%v", resolved, err)
	}
	if _, err := service.UpdateProviderConfig(context.Background(), 7, created.ID, ProviderConfigInput{
		Kind:     repository.ProviderConfigKindWebSearch,
		Name:     "Switch to Brave",
		Provider: agentWebSearch.BraveProviderName,
		BaseURL:  agentWebSearch.DefaultBraveBaseURL,
		Revision: created.Revision,
	}); err == nil {
		t.Fatal("UpdateProviderConfig() allowed provider change without a new API key")
	}
	if _, err := service.ResolveWebSearchProvider(
		context.Background(),
		agentWebSearch.AccessSubject{UserID: 8, RunID: "run-other"},
		created.ID,
	); err == nil {
		t.Fatal("ResolveWebSearchProvider() error = nil for another tenant")
	}
}

func TestWebSearchProviderConfigRejectsUnknownProvider(t *testing.T) {
	repo := &providerConfigRepoStub{configs: make(map[primitive.ObjectID]repository.ProviderConfig)}
	cipher, err := agentCredential.NewAESGCMCipher("v1", map[string][]byte{"v1": bytes.Repeat([]byte{12}, 32)})
	if err != nil {
		t.Fatalf("NewAESGCMCipher() error = %v", err)
	}
	factory, err := agentWebSearch.NewProviderFactory(agentWebSearch.ProviderFactoryConfig{
		EndpointPolicy: agentModel.NewEndpointPolicy(),
	})
	if err != nil {
		t.Fatalf("NewProviderFactory() error = %v", err)
	}
	service := NewAgentService(
		"https://api.example.com/v1", "service-key", "service-model", "", repo, &ai.Client{}, nil,
		WithProviderConfigCipher(cipher),
		WithProviderEndpointPolicy(agentModel.NewEndpointPolicy()),
		WithWebSearchProviderFactory(factory),
	)
	defer service.Close()

	if _, err := service.CreateProviderConfig(context.Background(), 7, ProviderConfigInput{
		Kind: repository.ProviderConfigKindWebSearch,
		Name: "Unknown", Provider: "unknown-search",
		BaseURL: "https://example.com/search", APIKey: "secret",
	}); err == nil {
		t.Fatal("CreateProviderConfig() accepted an unsupported search provider")
	}
}
