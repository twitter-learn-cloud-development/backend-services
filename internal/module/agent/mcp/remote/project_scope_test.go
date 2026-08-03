package remote

import (
	"context"
	"errors"
	"sort"
	"testing"

	agentProject "twitter-clone/internal/module/agent/project"

	"github.com/mark3labs/mcp-go/mcp"
)

type projectMemoryStore struct {
	*memoryStore
}

func (store *projectMemoryStore) ListMCPConnectionsByAccess(
	_ context.Context,
	userID uint64,
	projectIDs []string,
	_, _ int,
) ([]*Connection, int64, error) {
	allowedProjects := stringSet(projectIDs)
	items := make([]*Connection, 0)
	for _, connection := range store.connections {
		if !connectionAccessibleForTest(connection, userID, allowedProjects) {
			continue
		}
		cloned := cloneConnection(connection)
		items = append(items, &cloned)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, int64(len(items)), nil
}

func (store *projectMemoryStore) GetMCPConnectionByID(_ context.Context, id string) (*Connection, error) {
	connection, ok := store.connections[id]
	if !ok {
		return nil, ErrConnectionNotFound
	}
	cloned := cloneConnection(connection)
	return &cloned, nil
}

func (store *projectMemoryStore) GetMCPConnectionByServerIDUnscoped(
	_ context.Context,
	serverID string,
) (*Connection, error) {
	for _, connection := range store.connections {
		if connection.ServerID == serverID {
			cloned := cloneConnection(connection)
			return &cloned, nil
		}
	}
	return nil, ErrConnectionNotFound
}

func (store *projectMemoryStore) ListMCPExecutionBindingsByAccess(
	_ context.Context,
	userID uint64,
	projectIDs []string,
	limit int,
) ([]ExecutionBinding, error) {
	allowedProjects := stringSet(projectIDs)
	bindings := make([]ExecutionBinding, 0)
	for _, connection := range store.connections {
		if !connectionAccessibleForTest(connection, userID, allowedProjects) || connection.ActiveSnapshotID == "" {
			continue
		}
		snapshot, ok := store.snapshots[connection.ActiveSnapshotID]
		if !ok || snapshot.ConnectionID != connection.ID || snapshot.UserID != connection.UserID {
			continue
		}
		bindings = append(bindings, ExecutionBinding{
			Connection: cloneConnection(connection), Snapshot: cloneSnapshot(snapshot),
		})
		if len(bindings) >= limit {
			break
		}
	}
	return bindings, nil
}

func connectionAccessibleForTest(connection Connection, userID uint64, projectIDs map[string]struct{}) bool {
	if normalizedConnectionScope(&connection) == ScopeProject {
		_, ok := projectIDs[connection.ProjectID]
		return ok
	}
	return connection.UserID == userID
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

type projectAccessStub struct {
	roles map[string]map[uint64]string
}

func (access *projectAccessStub) RequireAccess(
	_ context.Context,
	userID uint64,
	projectID string,
	permission string,
) error {
	role := access.roles[projectID][userID]
	switch permission {
	case agentProject.PermissionUse:
		if role == agentProject.RoleOwner || role == agentProject.RoleEditor || role == agentProject.RoleViewer {
			return nil
		}
	case agentProject.PermissionManageConnections:
		if role == agentProject.RoleOwner || role == agentProject.RoleEditor {
			return nil
		}
	}
	return agentProject.ErrAccessDenied
}

func (access *projectAccessStub) ListAccessibleProjectIDs(_ context.Context, userID uint64) ([]string, error) {
	ids := make([]string, 0)
	for projectID, roles := range access.roles {
		if _, ok := roles[userID]; ok {
			ids = append(ids, projectID)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func TestProjectConnectionUsesCurrentMembershipForControlAndExecution(t *testing.T) {
	const projectID = "agentproj_0123456789abcdef0123456789abcdef"
	store := &projectMemoryStore{memoryStore: newMemoryStore()}
	access := &projectAccessStub{roles: map[string]map[uint64]string{
		projectID: {7: agentProject.RoleEditor, 8: agentProject.RoleViewer},
	}}
	readTool := mcp.NewTool("lookup", mcp.WithDescription("read project data"))
	readOnly := true
	destructive := false
	readTool.Annotations.ReadOnlyHint = &readOnly
	readTool.Annotations.DestructiveHint = &destructive
	discoverer := &discovererStub{tools: []mcp.Tool{readTool}}
	manager := NewManager(
		store, nil, nil, discoverer,
		WithEnabled(true), WithProjectScope(true, access),
	)
	manager.newID = func(prefix string) (string, error) {
		if prefix == "mcpconn" {
			return "mcpconn_0123456789abcdef0123456789abcdef", nil
		}
		return "mcpsnap_0123456789abcdef0123456789abcdef", nil
	}

	connection, err := manager.CreateConnection(context.Background(), 7, ConnectionInput{
		Scope: ScopeProject, ProjectID: projectID, Name: "Team Research",
		Transport: TransportStreamableHTTP, Endpoint: "https://mcp.example.com/mcp", AuthType: AuthNone,
	})
	if err != nil {
		t.Fatalf("CreateConnection() error = %v", err)
	}
	if connection.Scope != ScopeProject || connection.ProjectID != projectID || connection.UserID != 7 {
		t.Fatalf("project connection = %+v", connection)
	}

	listed, total, err := manager.ListConnections(context.Background(), 8, 1, 20)
	if err != nil || total != 1 || len(listed) != 1 {
		t.Fatalf("viewer ListConnections() = %d/%d, %v", len(listed), total, err)
	}
	if _, _, err := manager.DiscoverTools(context.Background(), 8, connection.ID, connection.Revision); !errors.Is(err, agentProject.ErrAccessDenied) {
		t.Fatalf("viewer DiscoverTools() error = %v", err)
	}

	connection, snapshot, err := manager.DiscoverTools(context.Background(), 7, connection.ID, connection.Revision)
	if err != nil {
		t.Fatalf("editor DiscoverTools() error = %v", err)
	}
	connection, snapshot, err = manager.ApproveSnapshot(
		context.Background(), 7, connection.ID, snapshot.ID, connection.Revision,
	)
	if err != nil {
		t.Fatalf("editor ApproveSnapshot() error = %v", err)
	}
	connection, _, err = manager.ConfigureTool(context.Background(), 7, connection.ID, connection.Revision, ToolPolicyInput{
		SnapshotID: snapshot.ID, ToolName: "lookup", Category: ToolCategoryRead, Enabled: true,
	})
	if err != nil {
		t.Fatalf("editor ConfigureTool() error = %v", err)
	}

	tools, err := manager.ListExecutableTools(context.Background(), 8)
	if err != nil || len(tools) != 1 {
		t.Fatalf("viewer ListExecutableTools() = %d, %v", len(tools), err)
	}
	if _, err := manager.CallTool(context.Background(), 8, tools[0].Schema.QualifiedName, map[string]interface{}{}); err != nil {
		t.Fatalf("viewer CallTool() error = %v", err)
	}
	if discoverer.callName != "lookup" {
		t.Fatalf("remote call name = %q", discoverer.callName)
	}

	delete(access.roles[projectID], 8)
	if _, err := manager.CallTool(context.Background(), 8, tools[0].Schema.QualifiedName, map[string]interface{}{}); !errors.Is(err, agentProject.ErrAccessDenied) {
		t.Fatalf("revoked member CallTool() error = %v", err)
	}
	listed, total, err = manager.ListConnections(context.Background(), 8, 1, 20)
	if err != nil || total != 0 || len(listed) != 0 {
		t.Fatalf("revoked member ListConnections() = %d/%d, %v", len(listed), total, err)
	}
	_ = connection
}

func TestProjectScopeRemainsDisabledWithoutExplicitFlag(t *testing.T) {
	manager := NewManager(newMemoryStore(), nil, nil, &discovererStub{}, WithEnabled(true))
	_, err := manager.CreateConnection(context.Background(), 7, ConnectionInput{
		Scope: ScopeProject, ProjectID: "agentproj_0123456789abcdef0123456789abcdef",
		Name: "Team", Transport: TransportStreamableHTTP,
		Endpoint: "https://mcp.example.com/mcp", AuthType: AuthNone,
	})
	if !errors.Is(err, ErrProjectScopeDisabled) {
		t.Fatalf("project scope without flag error = %v", err)
	}
}
