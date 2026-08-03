package remote

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	agentCredential "twitter-clone/internal/module/agent/credential"
	agentModel "twitter-clone/internal/module/agent/model"

	"github.com/mark3labs/mcp-go/mcp"
)

type memoryStore struct {
	connections map[string]Connection
	snapshots   map[string]ToolSchemaSnapshot
}

func newMemoryStore() *memoryStore {
	return &memoryStore{connections: make(map[string]Connection), snapshots: make(map[string]ToolSchemaSnapshot)}
}

func (store *memoryStore) CreateMCPConnection(_ context.Context, connection *Connection) error {
	store.connections[connection.ID] = cloneConnection(*connection)
	return nil
}

func (store *memoryStore) UpdateMCPConnection(_ context.Context, connection *Connection, expectedRevision int64) error {
	existing, ok := store.connections[connection.ID]
	if !ok || existing.UserID != connection.UserID || existing.Revision != expectedRevision || existing.Status != ConnectionStatusActive {
		return ErrRevisionConflict
	}
	connection.Revision = expectedRevision + 1
	store.connections[connection.ID] = cloneConnection(*connection)
	return nil
}

func (store *memoryStore) ListMCPConnections(_ context.Context, userID uint64, _, _ int) ([]*Connection, int64, error) {
	items := make([]*Connection, 0)
	for _, value := range store.connections {
		if value.UserID == userID {
			cloned := cloneConnection(value)
			items = append(items, &cloned)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, int64(len(items)), nil
}

func (store *memoryStore) GetMCPConnection(_ context.Context, id string, userID uint64) (*Connection, error) {
	connection, ok := store.connections[id]
	if !ok || connection.UserID != userID {
		return nil, ErrConnectionNotFound
	}
	cloned := cloneConnection(connection)
	return &cloned, nil
}

func (store *memoryStore) GetMCPConnectionByServerID(_ context.Context, serverID string, userID uint64) (*Connection, error) {
	for _, connection := range store.connections {
		if connection.ServerID == serverID && connection.UserID == userID {
			cloned := cloneConnection(connection)
			return &cloned, nil
		}
	}
	return nil, ErrConnectionNotFound
}

func (store *memoryStore) RevokeMCPConnection(_ context.Context, id string, userID uint64, expectedRevision int64) error {
	connection, ok := store.connections[id]
	if !ok || connection.UserID != userID || connection.Revision != expectedRevision {
		return ErrRevisionConflict
	}
	connection.Status = ConnectionStatusRevoked
	connection.HasSecret = false
	connection.EncryptionKeyID = ""
	connection.SecretNonce = ""
	connection.EncryptedCredential = ""
	connection.Revision++
	store.connections[id] = connection
	return nil
}

func (store *memoryStore) SaveMCPToolSnapshot(_ context.Context, snapshot *ToolSchemaSnapshot) (*ToolSchemaSnapshot, error) {
	for _, existing := range store.snapshots {
		if existing.ConnectionID == snapshot.ConnectionID && existing.SchemaHash == snapshot.SchemaHash {
			cloned := cloneSnapshot(existing)
			return &cloned, nil
		}
	}
	store.snapshots[snapshot.ID] = cloneSnapshot(*snapshot)
	cloned := cloneSnapshot(*snapshot)
	return &cloned, nil
}

func (store *memoryStore) GetMCPToolSnapshot(_ context.Context, id, connectionID string, userID uint64) (*ToolSchemaSnapshot, error) {
	snapshot, ok := store.snapshots[id]
	if !ok || snapshot.ConnectionID != connectionID || snapshot.UserID != userID {
		return nil, ErrSnapshotNotFound
	}
	cloned := cloneSnapshot(snapshot)
	return &cloned, nil
}

func (store *memoryStore) ListMCPExecutionBindings(_ context.Context, userID uint64, limit int) ([]ExecutionBinding, error) {
	bindings := make([]ExecutionBinding, 0)
	for _, connection := range store.connections {
		if connection.UserID != userID || connection.ActiveSnapshotID == "" {
			continue
		}
		snapshot, ok := store.snapshots[connection.ActiveSnapshotID]
		if !ok {
			continue
		}
		bindings = append(bindings, ExecutionBinding{
			Connection: cloneConnection(connection),
			Snapshot:   cloneSnapshot(snapshot),
		})
		if len(bindings) >= limit {
			break
		}
	}
	return bindings, nil
}

type discovererStub struct {
	tools       []mcp.Tool
	err         error
	request     DiscoveryRequest
	callResult  *mcp.CallToolResult
	callErr     error
	callName    string
	callInputs  map[string]interface{}
	invalidated []DiscoveryRequest
}

func (stub *discovererStub) Invalidate(request DiscoveryRequest) {
	stub.invalidated = append(stub.invalidated, request)
}

func (stub *discovererStub) Discover(_ context.Context, request DiscoveryRequest) ([]mcp.Tool, error) {
	stub.request = request
	return append([]mcp.Tool(nil), stub.tools...), stub.err
}

func (stub *discovererStub) Call(
	_ context.Context,
	request DiscoveryRequest,
	toolName string,
	arguments map[string]interface{},
) (*mcp.CallToolResult, error) {
	stub.request = request
	stub.callName = toolName
	stub.callInputs = arguments
	if stub.callResult == nil && stub.callErr == nil {
		stub.callResult = mcp.NewToolResultText("ok")
	}
	return stub.callResult, stub.callErr
}

func TestConnectionLifecycleEncryptsCredentialAndIsolatesTenant(t *testing.T) {
	store := newMemoryStore()
	cipher, err := agentCredential.NewAESGCMCipher("test", map[string][]byte{"test": []byte(strings.Repeat("k", 32))})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(store, cipher, agentModel.NewEndpointPolicy("mcp.example.com"), &discovererStub{}, WithEnabled(true))
	created, err := manager.CreateConnection(context.Background(), 7, ConnectionInput{
		Name: "Research", Transport: TransportStreamableHTTP,
		Endpoint: "https://mcp.example.com/mcp", AuthType: AuthBearer, BearerToken: "top-secret-token",
	})
	if err != nil {
		t.Fatalf("CreateConnection() error = %v", err)
	}
	stored := store.connections[created.ID]
	if !stored.HasSecret || stored.EncryptedCredential == "" || strings.Contains(stored.EncryptedCredential, "top-secret-token") {
		t.Fatalf("stored connection leaked or omitted encrypted credential: %+v", stored)
	}
	if _, err := manager.GetConnection(context.Background(), 8, created.ID); !errors.Is(err, ErrConnectionNotFound) {
		t.Fatalf("cross-tenant GetConnection() error = %v", err)
	}
	updated, err := manager.UpdateConnection(context.Background(), 7, created.ID, created.Revision, ConnectionInput{
		Name: "Research updated", Transport: TransportStreamableHTTP,
		Endpoint: "https://mcp.example.com/mcp", AuthType: AuthBearer,
	})
	if err != nil {
		t.Fatalf("UpdateConnection() error = %v", err)
	}
	if updated.Revision != 2 || updated.CredentialVersion != 1 || updated.EncryptedCredential != stored.EncryptedCredential {
		t.Fatalf("metadata update unexpectedly rotated credential: %+v", updated)
	}
	if err := manager.RevokeConnection(context.Background(), 7, created.ID, updated.Revision); err != nil {
		t.Fatalf("RevokeConnection() error = %v", err)
	}
	revoked := store.connections[created.ID]
	if revoked.Status != ConnectionStatusRevoked || revoked.HasSecret || revoked.EncryptedCredential != "" {
		t.Fatalf("revoked connection retained credential: %+v", revoked)
	}
}

func TestConnectionRejectsStdioAndPrivateEndpoint(t *testing.T) {
	manager := NewManager(newMemoryStore(), nil, agentModel.NewEndpointPolicy(), &discovererStub{}, WithEnabled(true))
	_, err := manager.CreateConnection(context.Background(), 7, ConnectionInput{
		Name: "unsafe", Transport: "stdio", Endpoint: "https://mcp.example.com", AuthType: AuthNone,
	})
	if err == nil || !strings.Contains(err.Error(), "streamable_http or sse") {
		t.Fatalf("stdio CreateConnection() error = %v", err)
	}
	_, err = manager.CreateConnection(context.Background(), 7, ConnectionInput{
		Name: "private", Transport: TransportStreamableHTTP, Endpoint: "http://127.0.0.1:8080/mcp", AuthType: AuthNone,
	})
	if !errors.Is(err, agentModel.ErrEndpointNotAllowed) {
		t.Fatalf("private endpoint CreateConnection() error = %v", err)
	}
	_, err = NewManager(newMemoryStore(), nil, agentModel.NewEndpointPolicy("mcp.example.com"), &discovererStub{}).
		CreateConnection(context.Background(), 7, ConnectionInput{
			Name: "plaintext credential", Transport: TransportSSE,
			Endpoint: "http://mcp.example.com/sse", AuthType: AuthBearer, BearerToken: "secret",
		})
	if err == nil || !strings.Contains(err.Error(), "must use https") {
		t.Fatalf("plaintext bearer endpoint CreateConnection() error = %v", err)
	}
}

func TestConnectionRotationAndRevocationInvalidatePriorSessionIdentity(t *testing.T) {
	store := newMemoryStore()
	cipher, err := agentCredential.NewAESGCMCipher("test", map[string][]byte{"test": []byte(strings.Repeat("k", 32))})
	if err != nil {
		t.Fatal(err)
	}
	discoverer := &discovererStub{}
	manager := NewManager(store, cipher, agentModel.NewEndpointPolicy("mcp.example.com"), discoverer, WithEnabled(true))
	created, err := manager.CreateConnection(context.Background(), 7, ConnectionInput{
		Name: "Research", Transport: TransportStreamableHTTP,
		Endpoint: "https://mcp.example.com/mcp", AuthType: AuthBearer, BearerToken: "token-v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := manager.UpdateConnection(context.Background(), 7, created.ID, created.Revision, ConnectionInput{
		Name: "Research", Transport: TransportStreamableHTTP,
		Endpoint: "https://mcp.example.com/mcp", AuthType: AuthBearer, BearerToken: "token-v2",
	})
	if err != nil {
		t.Fatalf("UpdateConnection() error = %v", err)
	}
	if len(discoverer.invalidated) != 1 || discoverer.invalidated[0].CredentialVersion != 1 {
		t.Fatalf("rotation invalidations = %+v", discoverer.invalidated)
	}
	if err := manager.RevokeConnection(context.Background(), 7, created.ID, updated.Revision); err != nil {
		t.Fatalf("RevokeConnection() error = %v", err)
	}
	if len(discoverer.invalidated) != 2 || discoverer.invalidated[1].CredentialVersion != 2 {
		t.Fatalf("revoke invalidations = %+v", discoverer.invalidated)
	}
}

func TestDiscoveryCreatesImmutableNamespacedSnapshotAndRequiresReview(t *testing.T) {
	store := newMemoryStore()
	discoverer := &discovererStub{tools: []mcp.Tool{
		mcp.NewTool("zeta", mcp.WithDescription("last"), mcp.WithString("query", mcp.Required())),
		mcp.NewTool("alpha", mcp.WithDescription("first"), mcp.WithNumber("limit")),
	}}
	manager := NewManager(store, nil, agentModel.NewEndpointPolicy("mcp.example.com"), discoverer, WithEnabled(true))
	created, err := manager.CreateConnection(context.Background(), 9, ConnectionInput{
		Name: "Public tools", Transport: TransportStreamableHTTP,
		Endpoint: "https://mcp.example.com/mcp", AuthType: AuthNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	discovered, first, err := manager.DiscoverTools(context.Background(), 9, created.ID, created.Revision)
	if err != nil {
		t.Fatalf("DiscoverTools() error = %v", err)
	}
	if discovered.DiscoveryStatus != DiscoveryStatusReviewRequired || discovered.PendingSnapshotID != first.ID || discovered.ActiveSnapshotID != "" {
		t.Fatalf("discovered connection did not fail closed: %+v", discovered)
	}
	if len(first.Tools) != 2 || first.Tools[0].Name != "alpha" || first.Tools[1].Name != "zeta" {
		t.Fatalf("snapshot tools are not stable: %+v", first.Tools)
	}
	for _, tool := range first.Tools {
		if tool.QualifiedName != created.ServerID+"."+tool.Name {
			t.Fatalf("tool is not namespaced: %+v", tool)
		}
	}
	approved, _, err := manager.ApproveSnapshot(context.Background(), 9, created.ID, first.ID, discovered.Revision)
	if err != nil {
		t.Fatalf("ApproveSnapshot() error = %v", err)
	}
	if approved.ActiveSnapshotID != first.ID || approved.PendingSnapshotID != "" || approved.DiscoveryStatus != DiscoveryStatusReady {
		t.Fatalf("approved connection = %+v", approved)
	}

	rediscovered, same, err := manager.DiscoverTools(context.Background(), 9, created.ID, approved.Revision)
	if err != nil {
		t.Fatalf("second DiscoverTools() error = %v", err)
	}
	if same.ID != first.ID || rediscovered.PendingSnapshotID != "" || rediscovered.DiscoveryStatus != DiscoveryStatusReady {
		t.Fatalf("unchanged discovery requested another review: connection=%+v snapshot=%+v", rediscovered, same)
	}

	discoverer.tools[0] = mcp.NewTool("zeta", mcp.WithDescription("changed"), mcp.WithString("query", mcp.Required()))
	changedConnection, changed, err := manager.DiscoverTools(context.Background(), 9, created.ID, rediscovered.Revision)
	if err != nil {
		t.Fatalf("changed DiscoverTools() error = %v", err)
	}
	if changed.ID == first.ID || changed.SchemaHash == first.SchemaHash || changedConnection.ActiveSnapshotID != first.ID || changedConnection.PendingSnapshotID != changed.ID {
		t.Fatalf("changed schema did not preserve approved snapshot: connection=%+v snapshot=%+v", changedConnection, changed)
	}
	if store.snapshots[first.ID].Tools[1].Description != "last" {
		t.Fatalf("first snapshot was mutated: %+v", store.snapshots[first.ID])
	}
}

func TestDiscoveryPassesDecryptedBearerAndHonorsFeatureFlag(t *testing.T) {
	store := newMemoryStore()
	cipher, _ := agentCredential.NewAESGCMCipher("test", map[string][]byte{"test": []byte(strings.Repeat("s", 32))})
	discoverer := &discovererStub{tools: []mcp.Tool{mcp.NewTool("lookup")}}
	manager := NewManager(store, cipher, agentModel.NewEndpointPolicy("mcp.example.com"), discoverer)
	created, err := manager.CreateConnection(context.Background(), 11, ConnectionInput{
		Name: "Private", Transport: TransportSSE, Endpoint: "https://mcp.example.com/sse",
		AuthType: AuthBearer, BearerToken: "tenant-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.DiscoverTools(context.Background(), 11, created.ID, created.Revision); !errors.Is(err, ErrDisabled) {
		t.Fatalf("disabled DiscoverTools() error = %v", err)
	}
	manager.enabled = true
	if _, _, err := manager.DiscoverTools(context.Background(), 11, created.ID, created.Revision); err != nil {
		t.Fatalf("enabled DiscoverTools() error = %v", err)
	}
	if discoverer.request.BearerToken != "tenant-token" {
		t.Fatalf("discoverer bearer token = %q", discoverer.request.BearerToken)
	}
}

func TestToolPolicyIsSnapshotBoundAndReadOnlyFailClosed(t *testing.T) {
	store := newMemoryStore()
	discoverer := &discovererStub{tools: []mcp.Tool{
		declaredReadOnlyTool("lookup"),
		mcp.NewTool("mutate", mcp.WithString("value")),
	}}
	manager := NewManager(
		store,
		nil,
		agentModel.NewEndpointPolicy("mcp.example.com"),
		discoverer,
		WithEnabled(true),
	)
	created, err := manager.CreateConnection(context.Background(), 21, ConnectionInput{
		Name: "governed", Transport: TransportStreamableHTTP,
		Endpoint: "https://mcp.example.com/mcp", AuthType: AuthNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	discovered, snapshot, err := manager.DiscoverTools(context.Background(), 21, created.ID, created.Revision)
	if err != nil {
		t.Fatal(err)
	}
	approved, _, err := manager.ApproveSnapshot(
		context.Background(), 21, created.ID, snapshot.ID, discovered.Revision,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !approved.FirstActivatedAt.IsZero() {
		t.Fatalf("schema approval activated connector early: %s", approved.FirstActivatedAt)
	}
	_, _, views, err := manager.ListTools(context.Background(), 21, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 2 || views[0].Policy.Enabled || views[0].Policy.Category != ToolCategoryRisky {
		t.Fatalf("default tool views = %+v", views)
	}
	if _, _, err := manager.ConfigureTool(context.Background(), 21, created.ID, approved.Revision, ToolPolicyInput{
		SnapshotID: snapshot.ID, ToolName: "mutate", Category: ToolCategoryRead, Enabled: true,
	}); !errors.Is(err, ErrToolRiskBlocked) {
		t.Fatalf("enable undeclared read tool error = %v", err)
	}
	if _, _, err := manager.ConfigureTool(context.Background(), 21, created.ID, approved.Revision, ToolPolicyInput{
		SnapshotID: "stale", ToolName: "lookup", Category: ToolCategoryRead, Enabled: true,
	}); !errors.Is(err, ErrSnapshotMismatch) {
		t.Fatalf("stale snapshot policy error = %v", err)
	}
	configured, configuredTool, err := manager.ConfigureTool(
		context.Background(),
		21,
		created.ID,
		approved.Revision,
		ToolPolicyInput{
			SnapshotID: snapshot.ID, ToolName: "lookup", Category: ToolCategoryRead, Enabled: true,
		},
	)
	if err != nil {
		t.Fatalf("ConfigureTool() error = %v", err)
	}
	if !configuredTool.Policy.Enabled || configuredTool.Policy.QualifiedName != created.ServerID+".lookup" {
		t.Fatalf("configured tool = %+v", configuredTool)
	}
	if configured.FirstActivatedAt.IsZero() {
		t.Fatal("first enabled reviewed tool did not activate connector")
	}
	firstActivatedAt := configured.FirstActivatedAt
	executable, err := manager.ListExecutableTools(context.Background(), 21)
	if err != nil {
		t.Fatalf("ListExecutableTools() error = %v", err)
	}
	if len(executable) != 1 || executable[0].Schema.QualifiedName != created.ServerID+".lookup" {
		t.Fatalf("executable tools = %+v", executable)
	}
	configured, configuredTool, err = manager.ConfigureTool(
		context.Background(),
		21,
		created.ID,
		configured.Revision,
		ToolPolicyInput{
			SnapshotID: snapshot.ID, ToolName: "mutate", Category: ToolCategoryRisky, Enabled: true,
		},
	)
	if err != nil || !configuredTool.Policy.Enabled {
		t.Fatalf("configure risky tool = %+v, %v", configuredTool, err)
	}
	if !configured.FirstActivatedAt.Equal(firstActivatedAt) {
		t.Fatalf("connector activation timestamp changed: first=%s current=%s", firstActivatedAt, configured.FirstActivatedAt)
	}
	governed, err := manager.ListGovernedTools(context.Background(), 21)
	if err != nil {
		t.Fatalf("ListGovernedTools() error = %v", err)
	}
	if len(governed) != 2 || governed[0].Policy.Category != ToolCategoryRead || governed[1].Policy.Category != ToolCategoryRisky {
		t.Fatalf("governed tools = %+v", governed)
	}
	if governed[0].ConnectionName != created.Name || governed[0].HealthStatus != HealthStatusUnknown ||
		governed[0].SnapshotVersion != snapshot.Version || governed[0].SchemaHash != snapshot.SchemaHash {
		t.Fatalf("governed catalog metadata = %+v, snapshot = %+v", governed[0], snapshot)
	}
	executable, err = manager.ListExecutableTools(context.Background(), 21)
	if err != nil || len(executable) != 1 || executable[0].Policy.Category != ToolCategoryRead {
		t.Fatalf("read-only executable tools changed = %+v, %v", executable, err)
	}
	otherTenant, err := manager.ListExecutableTools(context.Background(), 22)
	if err != nil || len(otherTenant) != 0 {
		t.Fatalf("cross-tenant executable tools = %+v, %v", otherTenant, err)
	}

	discoverer.tools[0] = declaredReadOnlyTool("lookup")
	discoverer.tools[0].Description = "changed schema"
	reviewRequired, changedSnapshot, err := manager.DiscoverTools(
		context.Background(), 21, created.ID, configured.Revision,
	)
	if err != nil {
		t.Fatal(err)
	}
	if reviewRequired.DiscoveryStatus != DiscoveryStatusReviewRequired {
		t.Fatalf("changed discovery status = %s", reviewRequired.DiscoveryStatus)
	}
	if tools, err := manager.ListExecutableTools(context.Background(), 21); err != nil || len(tools) != 0 {
		t.Fatalf("tools remained executable during review: %+v, %v", tools, err)
	}
	reapproved, _, err := manager.ApproveSnapshot(
		context.Background(), 21, created.ID, changedSnapshot.ID, reviewRequired.Revision,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(reapproved.ToolPolicies) != 0 {
		t.Fatalf("new snapshot retained old policies: %+v", reapproved.ToolPolicies)
	}
}

func TestCallToolRevalidatesPolicyAndDecryptsCredential(t *testing.T) {
	store := newMemoryStore()
	cipher, err := agentCredential.NewAESGCMCipher(
		"test",
		map[string][]byte{"test": []byte(strings.Repeat("c", 32))},
	)
	if err != nil {
		t.Fatal(err)
	}
	discoverer := &discovererStub{tools: []mcp.Tool{declaredReadOnlyTool("lookup")}}
	manager := NewManager(
		store,
		cipher,
		agentModel.NewEndpointPolicy("mcp.example.com"),
		discoverer,
		WithEnabled(true),
	)
	created, err := manager.CreateConnection(context.Background(), 31, ConnectionInput{
		Name: "private", Transport: TransportStreamableHTTP,
		Endpoint: "https://mcp.example.com/mcp", AuthType: AuthBearer, BearerToken: "runtime-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	discovered, snapshot, err := manager.DiscoverTools(context.Background(), 31, created.ID, created.Revision)
	if err != nil {
		t.Fatal(err)
	}
	approved, _, err := manager.ApproveSnapshot(
		context.Background(), 31, created.ID, snapshot.ID, discovered.Revision,
	)
	if err != nil {
		t.Fatal(err)
	}
	configured, _, err := manager.ConfigureTool(context.Background(), 31, created.ID, approved.Revision, ToolPolicyInput{
		SnapshotID: snapshot.ID, ToolName: "lookup", Category: ToolCategoryRead, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.CallTool(
		context.Background(), 31, created.ServerID+".lookup", map[string]interface{}{"query": "Go"},
	)
	if err != nil || result == nil {
		t.Fatalf("CallTool() result/error = %+v, %v", result, err)
	}
	if discoverer.request.BearerToken != "runtime-secret" || discoverer.callName != "lookup" || discoverer.callInputs["query"] != "Go" {
		t.Fatalf("remote call did not receive governed request: %+v", discoverer)
	}
	if _, err := manager.CallTool(context.Background(), 32, created.ServerID+".lookup", nil); err == nil {
		t.Fatal("cross-tenant CallTool() error = nil")
	}
	_, _, err = manager.ConfigureTool(context.Background(), 31, created.ID, configured.Revision, ToolPolicyInput{
		SnapshotID: snapshot.ID, ToolName: "lookup", Category: ToolCategoryRead, Enabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.CallTool(context.Background(), 31, created.ServerID+".lookup", nil); !errors.Is(err, ErrToolDisabled) {
		t.Fatalf("disabled CallTool() error = %v", err)
	}
}

func TestRiskyToolExecutesOnlyThroughGovernedWorkflowPath(t *testing.T) {
	store := newMemoryStore()
	discoverer := &discovererStub{tools: []mcp.Tool{
		mcp.NewTool("mutate", mcp.WithString("value", mcp.Required())),
	}}
	manager := NewManager(
		store,
		nil,
		agentModel.NewEndpointPolicy("mcp.example.com"),
		discoverer,
		WithEnabled(true),
	)
	created, err := manager.CreateConnection(context.Background(), 41, ConnectionInput{
		Name: "side effects", Transport: TransportStreamableHTTP,
		Endpoint: "https://mcp.example.com/mcp", AuthType: AuthNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	discovered, snapshot, err := manager.DiscoverTools(context.Background(), 41, created.ID, created.Revision)
	if err != nil {
		t.Fatal(err)
	}
	approved, _, err := manager.ApproveSnapshot(context.Background(), 41, created.ID, snapshot.ID, discovered.Revision)
	if err != nil {
		t.Fatal(err)
	}
	configured, view, err := manager.ConfigureTool(context.Background(), 41, created.ID, approved.Revision, ToolPolicyInput{
		SnapshotID: snapshot.ID, ToolName: "mutate", Category: ToolCategoryRisky, Enabled: true,
	})
	if err != nil {
		t.Fatalf("ConfigureTool(risky) error = %v", err)
	}
	qualifiedName := created.ServerID + ".mutate"
	if !view.Policy.Enabled || view.Policy.Category != ToolCategoryRisky || view.Schema.QualifiedName != qualifiedName {
		t.Fatalf("configured risky tool = %+v", view)
	}
	if catalog, err := manager.ListExecutableTools(context.Background(), 41); err != nil || len(catalog) != 0 {
		t.Fatalf("risky tool leaked into unified Agent catalog: %+v, %v", catalog, err)
	}
	if _, err := manager.GetExecutableTool(context.Background(), 41, qualifiedName); !errors.Is(err, ErrToolDisabled) {
		t.Fatalf("GetExecutableTool(risky) error = %v", err)
	}
	if _, err := manager.CallTool(context.Background(), 41, qualifiedName, nil); !errors.Is(err, ErrToolDisabled) {
		t.Fatalf("CallTool(risky) error = %v", err)
	}

	governed, err := manager.GetGovernedTool(context.Background(), 41, qualifiedName)
	if err != nil || governed.Policy.Category != ToolCategoryRisky {
		t.Fatalf("GetGovernedTool() = %+v, %v", governed, err)
	}
	result, err := manager.CallGovernedTool(context.Background(), 41, qualifiedName, map[string]interface{}{"value": "updated"}, "")
	if err != nil || result == nil {
		t.Fatalf("CallGovernedTool() = %+v, %v", result, err)
	}
	if discoverer.callName != "mutate" || discoverer.callInputs["value"] != "updated" {
		t.Fatalf("governed call = name %q inputs %+v", discoverer.callName, discoverer.callInputs)
	}
	if _, _, err := manager.ConfigureTool(context.Background(), 41, created.ID, configured.Revision, ToolPolicyInput{
		SnapshotID: snapshot.ID, ToolName: "mutate", Category: ToolCategoryWrite, Enabled: true,
	}); !errors.Is(err, ErrToolWriteBlocked) {
		t.Fatalf("ConfigureTool(write) error = %v", err)
	}
	disabled, _, err := manager.ConfigureTool(context.Background(), 41, created.ID, configured.Revision, ToolPolicyInput{
		SnapshotID: snapshot.ID, ToolName: "mutate", Category: ToolCategoryRisky, Enabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Revision <= configured.Revision {
		t.Fatalf("disable revision = %d, configured = %d", disabled.Revision, configured.Revision)
	}
	if _, err := manager.CallGovernedTool(context.Background(), 41, qualifiedName, nil, ""); !errors.Is(err, ErrToolDisabled) {
		t.Fatalf("disabled CallGovernedTool() error = %v", err)
	}
}

func TestWriteToolRequiresReviewedIdempotencyContractAndInjectsTrustedKey(t *testing.T) {
	const keyArgument = "idempotency_key"
	store := newMemoryStore()
	discoverer := &discovererStub{tools: []mcp.Tool{
		declaredIdempotentWriteTool("create_record", keyArgument),
	}}
	manager := NewManager(
		store,
		nil,
		agentModel.NewEndpointPolicy("mcp.example.com"),
		discoverer,
		WithEnabled(true),
	)
	created, err := manager.CreateConnection(context.Background(), 51, ConnectionInput{
		Name: "write contract", Transport: TransportStreamableHTTP,
		Endpoint: "https://mcp.example.com/mcp", AuthType: AuthNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	discovered, snapshot, err := manager.DiscoverTools(context.Background(), 51, created.ID, created.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Tools) != 1 || !snapshot.Tools[0].DeclaredIdempotent ||
		snapshot.Tools[0].IdempotencyKeyArgument != keyArgument || !snapshot.Tools[0].SupportsWriteIdempotency() {
		t.Fatalf("discovered write contract = %+v", snapshot.Tools)
	}
	approved, _, err := manager.ApproveSnapshot(
		context.Background(), 51, created.ID, snapshot.ID, discovered.Revision,
	)
	if err != nil {
		t.Fatal(err)
	}
	configured, view, err := manager.ConfigureTool(context.Background(), 51, created.ID, approved.Revision, ToolPolicyInput{
		SnapshotID: snapshot.ID, ToolName: "create_record", Category: ToolCategoryWrite, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !view.Policy.Enabled || view.Policy.Category != ToolCategoryWrite || configured.Revision <= approved.Revision {
		t.Fatalf("configured write tool = connection %+v, view %+v", configured, view)
	}
	qualifiedName := created.ServerID + ".create_record"
	if tools, err := manager.ListExecutableTools(context.Background(), 51); err != nil || len(tools) != 0 {
		t.Fatalf("write tool leaked into unified catalog: %+v, %v", tools, err)
	}
	governed, err := manager.ListGovernedTools(context.Background(), 51)
	if err != nil || len(governed) != 1 || governed[0].Policy.Category != ToolCategoryWrite {
		t.Fatalf("write tool missing from governed catalog: %+v, %v", governed, err)
	}
	if _, err := manager.CallGovernedTool(
		context.Background(), 51, qualifiedName,
		map[string]interface{}{"value": "created", keyArgument: "attacker-controlled"}, "",
	); !errors.Is(err, ErrIdempotencyKeyRequired) {
		t.Fatalf("CallGovernedTool(without key) error = %v", err)
	}
	result, err := manager.CallGovernedTool(
		context.Background(), 51, qualifiedName,
		map[string]interface{}{"value": "created", keyArgument: "attacker-controlled"},
		"run-1:step-1:mcp_server.create_record",
	)
	if err != nil || result == nil {
		t.Fatalf("CallGovernedTool(write) = %+v, %v", result, err)
	}
	expectedKey, err := DeriveRemoteIdempotencyKey("run-1:step-1:mcp_server.create_record")
	if err != nil {
		t.Fatal(err)
	}
	if discoverer.callInputs[keyArgument] != expectedKey || discoverer.callInputs[keyArgument] == "attacker-controlled" {
		t.Fatalf("trusted idempotency key was not injected: %+v", discoverer.callInputs)
	}
	if strings.Contains(expectedKey, "run-1") || strings.Contains(expectedKey, "step-1") {
		t.Fatalf("remote idempotency key leaked execution identity: %q", expectedKey)
	}
}

func TestDiscoveryRejectsMalformedWriteIdempotencyMetadata(t *testing.T) {
	tool := mcp.NewTool("mutate", mcp.WithString("value", mcp.Required()))
	idempotent := true
	tool.Annotations.IdempotentHint = &idempotent
	tool.Meta = mcp.NewMetaFromMap(map[string]any{IdempotencyKeyArgumentMetaField: "missing_key"})
	_, _, err := normalizeTools("mcp_server", []mcp.Tool{tool})
	if err == nil || !strings.Contains(err.Error(), "must exist with type string") {
		t.Fatalf("normalizeTools(malformed idempotency metadata) error = %v", err)
	}
}

func TestGovernedWriteToolFailsClosedWhenPersistedSchemaDoesNotMatchContract(t *testing.T) {
	store := newMemoryStore()
	store.connections["mcpconn_1"] = Connection{
		ID: "mcpconn_1", UserID: 61, ServerID: "mcp_server",
		Status: ConnectionStatusActive, DiscoveryStatus: DiscoveryStatusReady,
		ActiveSnapshotID: "mcpsnap_1",
		ToolPolicies: []ToolPolicy{{
			SnapshotID: "mcpsnap_1", ToolName: "create_record",
			QualifiedName: "mcp_server.create_record", Category: ToolCategoryWrite, Enabled: true,
		}},
		Revision: 1,
	}
	store.snapshots["mcpsnap_1"] = ToolSchemaSnapshot{
		ID: "mcpsnap_1", ConnectionID: "mcpconn_1", UserID: 61, ServerID: "mcp_server",
		Tools: []ToolSchema{{
			Name: "create_record", QualifiedName: "mcp_server.create_record",
			InputSchemaJSON:    `{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}`,
			DeclaredIdempotent: true, IdempotencyKeyArgument: "idempotency_key",
		}},
	}
	caller := &discovererStub{}
	manager := NewManager(store, nil, nil, nil, WithEnabled(true), WithCaller(caller))

	_, err := manager.CallGovernedTool(
		context.Background(), 61, "mcp_server.create_record",
		map[string]interface{}{"value": "created"}, "run-1:step-1:create_record",
	)
	if !errors.Is(err, ErrToolWriteBlocked) {
		t.Fatalf("CallGovernedTool(corrupt persisted contract) error = %v", err)
	}
	if caller.callName != "" {
		t.Fatalf("corrupt persisted contract reached remote caller: %q", caller.callName)
	}
}

func declaredReadOnlyTool(name string) mcp.Tool {
	tool := mcp.NewTool(name, mcp.WithString("query"))
	readOnly := true
	destructive := false
	tool.Annotations.ReadOnlyHint = &readOnly
	tool.Annotations.DestructiveHint = &destructive
	return tool
}

func declaredIdempotentWriteTool(name, keyArgument string) mcp.Tool {
	tool := mcp.NewTool(
		name,
		mcp.WithString("value", mcp.Required()),
		mcp.WithString(keyArgument, mcp.Required()),
	)
	idempotent := true
	tool.Annotations.IdempotentHint = &idempotent
	tool.Meta = mcp.NewMetaFromMap(map[string]any{
		IdempotencyKeyArgumentMetaField: keyArgument,
	})
	return tool
}

func cloneConnection(connection Connection) Connection {
	connection.ToolPolicies = append([]ToolPolicy(nil), connection.ToolPolicies...)
	return connection
}

func cloneSnapshot(snapshot ToolSchemaSnapshot) ToolSchemaSnapshot {
	snapshot.Tools = append([]ToolSchema(nil), snapshot.Tools...)
	return snapshot
}
