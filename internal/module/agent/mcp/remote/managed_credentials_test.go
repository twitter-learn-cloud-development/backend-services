package remote

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentCredential "twitter-clone/internal/module/agent/credential"
	agentModel "twitter-clone/internal/module/agent/model"
	agentProject "twitter-clone/internal/module/agent/project"

	"github.com/mark3labs/mcp-go/mcp"
)

const managedCredentialTestProjectID = "agentproj_0123456789abcdef0123456789abcdef"

func TestFileManagedCredentialResolverBindsProjectEndpointAndRotatesSecret(t *testing.T) {
	secretDirectory := t.TempDir()
	writeManagedSecret(t, secretDirectory, "research-token", "token-v1\n")
	resolver, err := NewFileManagedCredentialResolver(`[
		{
			"reference":"team.research",
			"project_id":"agentproj_0123456789abcdef0123456789abcdef",
			"endpoint":"https://mcp.example.com/mcp",
			"auth_type":"bearer",
			"secret_key":"research-token",
			"version":1
		}
	]`, secretDirectory, agentModel.NewEndpointPolicy("mcp.example.com"))
	if err != nil {
		t.Fatalf("NewFileManagedCredentialResolver() error = %v", err)
	}
	request := ManagedCredentialRequest{
		Reference: "team.research", ProjectID: managedCredentialTestProjectID,
		Endpoint: "https://mcp.example.com/mcp", AuthType: AuthBearer,
	}
	first, err := resolver.Resolve(context.Background(), request)
	if err != nil {
		t.Fatalf("Resolve(first) error = %v", err)
	}
	if first.BearerToken != "token-v1" || first.Version != 1 || first.Identity == "" {
		t.Fatalf("first managed credential = %+v", first)
	}

	wrongProject := request
	wrongProject.ProjectID = "agentproj_ffffffffffffffffffffffffffffffff"
	if _, err := resolver.Resolve(context.Background(), wrongProject); !errors.Is(err, ErrManagedCredentialBinding) {
		t.Fatalf("Resolve(wrong project) error = %v", err)
	}

	writeManagedSecret(t, secretDirectory, "research-token", "token-v2")
	second, err := resolver.Resolve(context.Background(), request)
	if err != nil {
		t.Fatalf("Resolve(rotated) error = %v", err)
	}
	if second.BearerToken != "token-v2" || second.Identity == first.Identity {
		t.Fatalf("rotated managed credential = %+v", second)
	}
}

func TestFileManagedCredentialResolverRejectsPlaintextAndUnknownFields(t *testing.T) {
	secretDirectory := t.TempDir()
	writeManagedSecret(t, secretDirectory, "research-token", "token")
	_, err := NewFileManagedCredentialResolver(`[
		{
			"reference":"team.research",
			"project_id":"agentproj_0123456789abcdef0123456789abcdef",
			"endpoint":"https://mcp.example.com/mcp",
			"auth_type":"bearer",
			"secret_key":"research-token",
			"version":1,
			"bearer_token":"must-not-be-accepted"
		}
	]`, secretDirectory, agentModel.NewEndpointPolicy("mcp.example.com"))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("plaintext registry error = %v", err)
	}
}

func TestManagedProjectConnectionPersistsReferenceWithoutSecretAndSupportsMigration(t *testing.T) {
	store := &projectMemoryStore{memoryStore: newMemoryStore()}
	access := &projectAccessStub{roles: map[string]map[uint64]string{
		managedCredentialTestProjectID: {7: agentProject.RoleOwner},
	}}
	resolver := &managedCredentialResolverStub{resolved: ResolvedManagedCredential{
		BearerToken: "managed-token", Version: 4, Identity: "managed-identity",
	}}
	cipher, err := agentCredential.NewAESGCMCipher("test", map[string][]byte{
		"test": []byte(strings.Repeat("k", 32)),
	})
	if err != nil {
		t.Fatal(err)
	}
	discoverer := &discovererStub{tools: []mcp.Tool{mcp.NewTool("lookup")}}
	manager := NewManager(
		store, cipher, agentModel.NewEndpointPolicy("mcp.example.com"), discoverer,
		WithEnabled(true), WithProjectScope(true, access), WithManagedCredentials(true, resolver),
	)
	created, err := manager.CreateConnection(context.Background(), 7, ConnectionInput{
		Scope: ScopeProject, ProjectID: managedCredentialTestProjectID, Name: "Managed research",
		Transport: TransportStreamableHTTP, Endpoint: "https://mcp.example.com/mcp",
		AuthType: AuthBearer, CredentialSource: CredentialSourceManaged,
		ManagedCredentialRef: "team.research",
	})
	if err != nil {
		t.Fatalf("CreateConnection() error = %v", err)
	}
	stored := store.connections[created.ID]
	if stored.CredentialSource != CredentialSourceManaged || stored.ManagedCredentialRef != "team.research" ||
		stored.ManagedCredentialVersion != 4 || !stored.HasSecret || stored.EncryptedCredential != "" || stored.SecretNonce != "" {
		t.Fatalf("stored managed connection = %+v", stored)
	}
	if strings.Contains(stored.EncryptedCredential, "managed-token") {
		t.Fatal("managed credential leaked into connection storage")
	}
	if _, _, err := manager.DiscoverTools(context.Background(), 7, created.ID, created.Revision); err != nil {
		t.Fatalf("DiscoverTools() error = %v", err)
	}
	if discoverer.request.BearerToken != "managed-token" || discoverer.request.CredentialIdentity == "" {
		t.Fatalf("managed discovery request = %+v", discoverer.request)
	}

	current := store.connections[created.ID]
	resolver.resolved = ResolvedManagedCredential{
		BearerToken: "managed-token-v2", Version: 5, Identity: "managed-identity-v2",
	}
	if _, _, err := manager.DiscoverTools(
		context.Background(), 7, created.ID, current.Revision,
	); !errors.Is(err, ErrManagedCredentialBinding) {
		t.Fatalf("DiscoverTools(registry version drift) error = %v", err)
	}
	updatedManaged, err := manager.UpdateConnection(context.Background(), 7, created.ID, current.Revision, ConnectionInput{
		Scope: ScopeProject, ProjectID: managedCredentialTestProjectID, Name: "Managed research v2",
		Transport: TransportStreamableHTTP, Endpoint: "https://mcp.example.com/mcp",
		AuthType: AuthBearer, CredentialSource: " managed ",
	})
	if err != nil {
		t.Fatalf("UpdateConnection(adopt managed version) error = %v", err)
	}
	if updatedManaged.ManagedCredentialVersion != 5 || updatedManaged.ManagedCredentialRef != "team.research" ||
		updatedManaged.PendingSnapshotID != "" || updatedManaged.ActiveSnapshotID != "" {
		t.Fatalf("updated managed credential binding = %+v", updatedManaged)
	}

	current = store.connections[created.ID]
	updated, err := manager.UpdateConnection(context.Background(), 7, created.ID, current.Revision, ConnectionInput{
		Scope: ScopeProject, ProjectID: managedCredentialTestProjectID, Name: "User-owned research",
		Transport: TransportStreamableHTTP, Endpoint: "https://mcp.example.com/mcp",
		AuthType: AuthBearer, CredentialSource: CredentialSourceUser, BearerToken: "user-token",
	})
	if err != nil {
		t.Fatalf("UpdateConnection(to user) error = %v", err)
	}
	if updated.CredentialSource != CredentialSourceUser || updated.ManagedCredentialRef != "" ||
		updated.ManagedCredentialVersion != 0 || updated.EncryptedCredential == "" {
		t.Fatalf("user credential migration = %+v", updated)
	}
}

func TestManagedCredentialRequiresProjectScopeAndFeatureFlag(t *testing.T) {
	resolver := &managedCredentialResolverStub{resolved: ResolvedManagedCredential{
		BearerToken: "managed-token", Version: 1, Identity: "identity",
	}}
	manager := NewManager(newMemoryStore(), nil, agentModel.NewEndpointPolicy("mcp.example.com"), &discovererStub{}, WithEnabled(true))
	_, err := manager.CreateConnection(context.Background(), 7, ConnectionInput{
		Name: "Personal managed", Transport: TransportStreamableHTTP,
		Endpoint: "https://mcp.example.com/mcp", AuthType: AuthBearer,
		CredentialSource: CredentialSourceManaged, ManagedCredentialRef: "team.research",
	})
	if !errors.Is(err, ErrManagedCredentialsDisabled) {
		t.Fatalf("managed credential without feature error = %v", err)
	}

	manager = NewManager(
		newMemoryStore(), nil, agentModel.NewEndpointPolicy("mcp.example.com"), &discovererStub{},
		WithEnabled(true), WithManagedCredentials(true, resolver),
	)
	_, err = manager.CreateConnection(context.Background(), 7, ConnectionInput{
		Name: "Personal managed", Transport: TransportStreamableHTTP,
		Endpoint: "https://mcp.example.com/mcp", AuthType: AuthBearer,
		CredentialSource: CredentialSourceManaged, ManagedCredentialRef: "team.research",
	})
	if err == nil || !strings.Contains(err.Error(), "require project scope") {
		t.Fatalf("personal managed credential error = %v", err)
	}
}

type managedCredentialResolverStub struct {
	request  ManagedCredentialRequest
	resolved ResolvedManagedCredential
	err      error
}

func (resolver *managedCredentialResolverStub) Resolve(
	_ context.Context,
	request ManagedCredentialRequest,
) (ResolvedManagedCredential, error) {
	resolver.request = request
	return resolver.resolved, resolver.err
}

func writeManagedSecret(t *testing.T, directory, key, value string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, key), []byte(value), 0o600); err != nil {
		t.Fatalf("write managed credential secret: %v", err)
	}
}
