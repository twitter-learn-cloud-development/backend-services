package project

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxProjectNameRunes     = 80
	maxProjectMembers       = 100
	maxAccessibleProjectIDs = 256
	defaultDirectoryTimeout = 3 * time.Second
)

type ManagerOption func(*Manager)

type Manager struct {
	store            Store
	directory        UserDirectory
	enabled          bool
	directoryTimeout time.Duration
	now              func() time.Time
	newID            func() (string, error)
}

func WithEnabled(enabled bool) ManagerOption {
	return func(manager *Manager) { manager.enabled = enabled }
}

func WithDirectoryTimeout(timeout time.Duration) ManagerOption {
	return func(manager *Manager) {
		if timeout > 0 {
			manager.directoryTimeout = timeout
		}
	}
}

func NewManager(store Store, directory UserDirectory, options ...ManagerOption) *Manager {
	manager := &Manager{
		store: store, directory: directory, directoryTimeout: defaultDirectoryTimeout,
		now: time.Now, newID: newProjectID,
	}
	for _, option := range options {
		if option != nil {
			option(manager)
		}
	}
	return manager
}

func (manager *Manager) CreateProject(ctx context.Context, actorUserID uint64, name string) (*Project, error) {
	if err := manager.available(); err != nil {
		return nil, err
	}
	if actorUserID == 0 {
		return nil, errors.New("Agent project owner is required")
	}
	name, err := normalizeProjectName(name)
	if err != nil {
		return nil, err
	}
	id, err := manager.newID()
	if err != nil {
		return nil, err
	}
	now := manager.now().UTC()
	project := &Project{
		ID: id, Name: name, OwnerID: actorUserID, Revision: 1,
		Members: []Member{{
			UserID: actorUserID, Role: RoleOwner, AddedBy: actorUserID,
			CreatedAt: now, UpdatedAt: now,
		}},
		CreatedAt: now, UpdatedAt: now,
	}
	if err := manager.store.CreateProject(ctx, project); err != nil {
		return nil, err
	}
	return cloneProject(project), nil
}

func (manager *Manager) ListProjects(
	ctx context.Context,
	userID uint64,
	page, pageSize int,
) ([]*Project, int64, error) {
	if err := manager.available(); err != nil {
		return nil, 0, err
	}
	if userID == 0 {
		return nil, 0, ErrAccessDenied
	}
	return manager.store.ListProjectsForUser(ctx, userID, page, pageSize)
}

func (manager *Manager) GetProject(ctx context.Context, userID uint64, projectID string) (*Project, error) {
	project, _, err := manager.authorizedProject(ctx, userID, projectID, PermissionUse)
	return project, err
}

func (manager *Manager) UpsertMember(
	ctx context.Context,
	actorUserID uint64,
	projectID string,
	targetUserID uint64,
	role string,
	expectedRevision int64,
) (*Project, error) {
	project, _, err := manager.authorizedProject(ctx, actorUserID, projectID, PermissionManageMembers)
	if err != nil {
		return nil, err
	}
	if expectedRevision <= 0 || project.Revision != expectedRevision {
		return nil, ErrRevisionConflict
	}
	if targetUserID == 0 {
		return nil, errors.New("Agent project member user is required")
	}
	role = strings.ToLower(strings.TrimSpace(role))
	if role != RoleViewer && role != RoleEditor {
		return nil, errors.New("Agent project member role must be viewer or editor")
	}
	if targetUserID == project.OwnerID {
		return nil, errors.New("Agent project owner role cannot be changed")
	}
	if err := manager.requireExistingUser(ctx, targetUserID); err != nil {
		return nil, err
	}

	now := manager.now().UTC()
	members := append([]Member(nil), project.Members...)
	replaced := false
	for index := range members {
		if members[index].UserID != targetUserID {
			continue
		}
		members[index].Role = role
		members[index].AddedBy = actorUserID
		members[index].UpdatedAt = now
		replaced = true
		break
	}
	if !replaced {
		if len(members) >= maxProjectMembers {
			return nil, fmt.Errorf("Agent project supports at most %d members", maxProjectMembers)
		}
		members = append(members, Member{
			UserID: targetUserID, Role: role, AddedBy: actorUserID,
			CreatedAt: now, UpdatedAt: now,
		})
	}
	sort.Slice(members, func(i, j int) bool { return members[i].UserID < members[j].UserID })
	project.Members = members
	project.UpdatedAt = now
	if err := manager.store.ReplaceProjectMembers(ctx, project, expectedRevision); err != nil {
		return nil, err
	}
	return cloneProject(project), nil
}

func (manager *Manager) RemoveMember(
	ctx context.Context,
	actorUserID uint64,
	projectID string,
	targetUserID uint64,
	expectedRevision int64,
) (*Project, error) {
	project, _, err := manager.authorizedProject(ctx, actorUserID, projectID, PermissionManageMembers)
	if err != nil {
		return nil, err
	}
	if expectedRevision <= 0 || project.Revision != expectedRevision {
		return nil, ErrRevisionConflict
	}
	if targetUserID == 0 {
		return nil, errors.New("Agent project member user is required")
	}
	if targetUserID == project.OwnerID {
		return nil, errors.New("Agent project owner cannot be removed")
	}
	members := make([]Member, 0, len(project.Members))
	removed := false
	for _, member := range project.Members {
		if member.UserID == targetUserID {
			removed = true
			continue
		}
		members = append(members, member)
	}
	if !removed {
		return nil, ErrMemberNotFound
	}
	project.Members = members
	project.UpdatedAt = manager.now().UTC()
	if err := manager.store.ReplaceProjectMembers(ctx, project, expectedRevision); err != nil {
		return nil, err
	}
	return cloneProject(project), nil
}

func (manager *Manager) RequireAccess(
	ctx context.Context,
	userID uint64,
	projectID string,
	permission string,
) error {
	_, _, err := manager.authorizedProject(ctx, userID, projectID, permission)
	return err
}

func (manager *Manager) ListAccessibleProjectIDs(ctx context.Context, userID uint64) ([]string, error) {
	if err := manager.available(); err != nil {
		return nil, err
	}
	if userID == 0 {
		return nil, ErrAccessDenied
	}
	ids, err := manager.store.ListProjectIDsForUser(ctx, userID, maxAccessibleProjectIDs+1)
	if err != nil {
		return nil, err
	}
	if len(ids) > maxAccessibleProjectIDs {
		return nil, fmt.Errorf("Agent project access exceeds the bounded limit of %d", maxAccessibleProjectIDs)
	}
	return ids, nil
}

func (manager *Manager) authorizedProject(
	ctx context.Context,
	userID uint64,
	projectID string,
	permission string,
) (*Project, Member, error) {
	if err := manager.available(); err != nil {
		return nil, Member{}, err
	}
	projectID = strings.TrimSpace(projectID)
	if userID == 0 || projectID == "" {
		return nil, Member{}, ErrAccessDenied
	}
	project, err := manager.store.GetProject(ctx, projectID)
	if err != nil {
		return nil, Member{}, err
	}
	for _, member := range project.Members {
		if member.UserID != userID {
			continue
		}
		if roleAllows(member.Role, permission) {
			return project, member, nil
		}
		return nil, Member{}, ErrAccessDenied
	}
	return nil, Member{}, ErrAccessDenied
}

func (manager *Manager) requireExistingUser(ctx context.Context, userID uint64) error {
	if manager.directory == nil {
		return errors.New("Agent project user directory is unavailable")
	}
	directoryCtx, cancel := context.WithTimeout(ctx, manager.directoryTimeout)
	defer cancel()
	exists, err := manager.directory.UserExists(directoryCtx, userID)
	if err != nil {
		return fmt.Errorf("validate Agent project member: %w", err)
	}
	if !exists {
		return ErrUserNotFound
	}
	return nil
}

func (manager *Manager) available() error {
	if manager == nil || !manager.enabled {
		return ErrDisabled
	}
	if manager.store == nil {
		return errors.New("Agent project store is unavailable")
	}
	return nil
}

func roleAllows(role, permission string) bool {
	switch permission {
	case PermissionUse:
		return role == RoleOwner || role == RoleEditor || role == RoleViewer
	case PermissionManageConnections:
		return role == RoleOwner || role == RoleEditor
	case PermissionManageMembers:
		return role == RoleOwner
	default:
		return false
	}
}

func normalizeProjectName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > maxProjectNameRunes {
		return "", errors.New("Agent project name must contain 1-80 characters")
	}
	return name, nil
}

func newProjectID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate Agent project identifier: %w", err)
	}
	return "agentproj_" + hex.EncodeToString(buffer), nil
}

func cloneProject(project *Project) *Project {
	if project == nil {
		return nil
	}
	cloned := *project
	cloned.Members = append([]Member(nil), project.Members...)
	return &cloned
}
