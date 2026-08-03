package project

import (
	"context"
	"errors"
	"sort"
	"testing"
)

type memoryProjectStore struct {
	projects map[string]Project
}

func newMemoryProjectStore() *memoryProjectStore {
	return &memoryProjectStore{projects: make(map[string]Project)}
}

func (store *memoryProjectStore) CreateProject(_ context.Context, project *Project) error {
	store.projects[project.ID] = *cloneProject(project)
	return nil
}

func (store *memoryProjectStore) GetProject(_ context.Context, projectID string) (*Project, error) {
	project, ok := store.projects[projectID]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneProject(&project), nil
}

func (store *memoryProjectStore) ListProjectsForUser(
	_ context.Context,
	userID uint64,
	_, _ int,
) ([]*Project, int64, error) {
	projects := make([]*Project, 0)
	for _, project := range store.projects {
		for _, member := range project.Members {
			if member.UserID == userID {
				projects = append(projects, cloneProject(&project))
				break
			}
		}
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].ID < projects[j].ID })
	return projects, int64(len(projects)), nil
}

func (store *memoryProjectStore) ListProjectIDsForUser(
	_ context.Context,
	userID uint64,
	limit int,
) ([]string, error) {
	projects, _, _ := store.ListProjectsForUser(context.Background(), userID, 1, limit)
	ids := make([]string, 0, len(projects))
	for _, project := range projects {
		ids = append(ids, project.ID)
		if len(ids) >= limit {
			break
		}
	}
	return ids, nil
}

func (store *memoryProjectStore) ReplaceProjectMembers(
	_ context.Context,
	project *Project,
	expectedRevision int64,
) error {
	existing, ok := store.projects[project.ID]
	if !ok {
		return ErrNotFound
	}
	if existing.Revision != expectedRevision {
		return ErrRevisionConflict
	}
	project.Revision = expectedRevision + 1
	store.projects[project.ID] = *cloneProject(project)
	return nil
}

type projectDirectoryStub struct {
	users map[uint64]bool
	err   error
}

func (directory *projectDirectoryStub) UserExists(_ context.Context, userID uint64) (bool, error) {
	return directory.users[userID], directory.err
}

func TestProjectMembershipRolesAndRevocation(t *testing.T) {
	store := newMemoryProjectStore()
	manager := NewManager(
		store,
		&projectDirectoryStub{users: map[uint64]bool{2: true}},
		WithEnabled(true),
	)
	manager.newID = func() (string, error) { return "agentproj_0123456789abcdef0123456789abcdef", nil }

	created, err := manager.CreateProject(context.Background(), 1, "Research Team")
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if created.Revision != 1 || len(created.Members) != 1 || created.Members[0].Role != RoleOwner {
		t.Fatalf("created project = %+v", created)
	}
	if err := manager.RequireAccess(context.Background(), 1, created.ID, PermissionManageMembers); err != nil {
		t.Fatalf("owner manage members error = %v", err)
	}

	updated, err := manager.UpsertMember(context.Background(), 1, created.ID, 2, RoleViewer, created.Revision)
	if err != nil {
		t.Fatalf("UpsertMember(viewer) error = %v", err)
	}
	if err := manager.RequireAccess(context.Background(), 2, created.ID, PermissionUse); err != nil {
		t.Fatalf("viewer use error = %v", err)
	}
	if err := manager.RequireAccess(context.Background(), 2, created.ID, PermissionManageConnections); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("viewer manage connections error = %v", err)
	}
	if _, err := manager.UpsertMember(context.Background(), 2, created.ID, 3, RoleViewer, updated.Revision); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("viewer changed membership error = %v", err)
	}

	updated, err = manager.UpsertMember(context.Background(), 1, created.ID, 2, RoleEditor, updated.Revision)
	if err != nil {
		t.Fatalf("UpsertMember(editor) error = %v", err)
	}
	if err := manager.RequireAccess(context.Background(), 2, created.ID, PermissionManageConnections); err != nil {
		t.Fatalf("editor manage connections error = %v", err)
	}

	updated, err = manager.RemoveMember(context.Background(), 1, created.ID, 2, updated.Revision)
	if err != nil {
		t.Fatalf("RemoveMember() error = %v", err)
	}
	if err := manager.RequireAccess(context.Background(), 2, created.ID, PermissionUse); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("removed member use error = %v", err)
	}
	if _, err := manager.RemoveMember(context.Background(), 1, created.ID, 1, updated.Revision); err == nil {
		t.Fatal("owner removal unexpectedly succeeded")
	}
}

func TestProjectMemberValidationAndCASFailClosed(t *testing.T) {
	store := newMemoryProjectStore()
	manager := NewManager(
		store,
		&projectDirectoryStub{users: map[uint64]bool{}},
		WithEnabled(true),
	)
	manager.newID = func() (string, error) { return "agentproj_0123456789abcdef0123456789abcdef", nil }
	created, err := manager.CreateProject(context.Background(), 1, "Team")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.UpsertMember(context.Background(), 1, created.ID, 99, RoleViewer, created.Revision); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("missing user error = %v", err)
	}

	manager.directory = &projectDirectoryStub{users: map[uint64]bool{2: true}}
	updated, err := manager.UpsertMember(context.Background(), 1, created.ID, 2, RoleViewer, created.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.UpsertMember(context.Background(), 1, created.ID, 2, RoleEditor, created.Revision); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale member update error = %v", err)
	}
	if updated.Revision != 2 {
		t.Fatalf("updated revision = %d", updated.Revision)
	}

	manager.directory = &projectDirectoryStub{err: errors.New("directory unavailable")}
	if _, err := manager.UpsertMember(context.Background(), 1, created.ID, 3, RoleViewer, updated.Revision); err == nil {
		t.Fatal("directory outage unexpectedly allowed a member")
	}
}
