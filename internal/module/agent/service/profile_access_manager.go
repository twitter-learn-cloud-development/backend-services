package service

import (
	"context"
	"errors"
	"fmt"

	"twitter-clone/internal/module/agent/profile"
	"twitter-clone/internal/module/agent/repository"
)

var (
	ErrProfileAccessForbidden        = errors.New("Agent Profile management access is forbidden")
	ErrProfileDynamicRBACUnavailable = errors.New("dynamic Agent Profile RBAC is disabled")
	ErrProfileAdminRoleRequiresRoot  = errors.New("only a break-glass root administrator may change the admin role")
)

type ProfileStaticRoleAssignments struct {
	ViewerUserIDs   []uint64
	EditorUserIDs   []uint64
	ApproverUserIDs []uint64
	AdminUserIDs    []uint64
}

type ProfileManagementAccess struct {
	Roles        []string
	StaticRoles  []string
	DynamicRoles []string
	RootAdmin    bool
}

type ProfileAccessManager struct {
	repository     repository.ProfileRoleBindingRepository
	dynamicEnabled bool
	staticRoles    map[uint64][]string
	rootAdmins     map[uint64]struct{}
}

func NewProfileAccessManager(
	repo repository.ProfileRoleBindingRepository,
	staticAssignments ProfileStaticRoleAssignments,
	dynamicEnabled bool,
) *ProfileAccessManager {
	manager := &ProfileAccessManager{
		repository: repo, dynamicEnabled: dynamicEnabled,
		staticRoles: make(map[uint64][]string), rootAdmins: make(map[uint64]struct{}),
	}
	manager.addStaticRole(staticAssignments.ViewerUserIDs, profile.ManagementRoleViewer)
	manager.addStaticRole(staticAssignments.EditorUserIDs, profile.ManagementRoleEditor)
	manager.addStaticRole(staticAssignments.ApproverUserIDs, profile.ManagementRoleApprover)
	manager.addStaticRole(staticAssignments.AdminUserIDs, profile.ManagementRoleAdmin)
	for _, userID := range staticAssignments.AdminUserIDs {
		if userID != 0 {
			manager.rootAdmins[userID] = struct{}{}
		}
	}
	return manager
}

func (m *ProfileAccessManager) ResolveAccess(ctx context.Context, userID uint64) (ProfileManagementAccess, error) {
	if m == nil || userID == 0 {
		return ProfileManagementAccess{}, ErrProfileAccessForbidden
	}
	staticRoles := append([]string(nil), m.staticRoles[userID]...)
	_, rootAdmin := m.rootAdmins[userID]
	access := ProfileManagementAccess{
		StaticRoles: staticRoles,
		Roles:       append([]string(nil), staticRoles...),
		RootAdmin:   rootAdmin,
	}
	if !m.dynamicEnabled || m.repository == nil {
		return access, nil
	}
	binding, err := m.repository.GetProfileRoleBinding(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrProfileRoleBindingNotFound) || len(staticRoles) > 0 {
			return access, nil
		}
		return ProfileManagementAccess{}, fmt.Errorf("resolve dynamic profile access: %w", err)
	}
	access.DynamicRoles = append([]string(nil), binding.Roles...)
	access.Roles = profile.MergeManagementRoles(access.StaticRoles, access.DynamicRoles)
	return access, nil
}

func (m *ProfileAccessManager) RequireRole(ctx context.Context, userID uint64, required profile.ManagementRole) error {
	access, err := m.ResolveAccess(ctx, userID)
	if err != nil {
		return err
	}
	if !profile.HasManagementRole(access.Roles, required) {
		return ErrProfileAccessForbidden
	}
	return nil
}

func (m *ProfileAccessManager) ListBindings(ctx context.Context, actorUserID uint64, page, pageSize int) ([]*repository.ProfileRoleBindingRecord, int64, error) {
	if err := m.requireDynamicAdmin(ctx, actorUserID); err != nil {
		return nil, 0, err
	}
	return m.repository.ListProfileRoleBindings(ctx, page, pageSize)
}

func (m *ProfileAccessManager) ListAuditEvents(ctx context.Context, actorUserID uint64, page, pageSize int) ([]*repository.ProfileRoleAuditEvent, int64, error) {
	if err := m.requireDynamicAdmin(ctx, actorUserID); err != nil {
		return nil, 0, err
	}
	return m.repository.ListProfileRoleAuditEvents(ctx, page, pageSize)
}

func (m *ProfileAccessManager) UpsertBinding(
	ctx context.Context,
	actorUserID, subjectUserID uint64,
	roles []string,
	expectedRevision int64,
) (*repository.ProfileRoleBindingRecord, error) {
	if err := m.requireDynamicAdmin(ctx, actorUserID); err != nil {
		return nil, err
	}
	if subjectUserID == 0 || expectedRevision < 0 {
		return nil, errors.New("subject user and a non-negative expected revision are required")
	}
	normalizedRoles, err := profile.NormalizeManagementRoles(roles)
	if err != nil {
		return nil, err
	}
	current, currentErr := m.repository.GetProfileRoleBinding(ctx, subjectUserID)
	if currentErr != nil && !errors.Is(currentErr, repository.ErrProfileRoleBindingNotFound) {
		return nil, currentErr
	}
	if profile.HasManagementRole(normalizedRoles, profile.ManagementRoleAdmin) ||
		(current != nil && profile.HasManagementRole(current.Roles, profile.ManagementRoleAdmin)) {
		if !m.IsRootAdmin(actorUserID) {
			return nil, ErrProfileAdminRoleRequiresRoot
		}
	}
	operationID, err := newProfileOperationID()
	if err != nil {
		return nil, err
	}
	audit := repository.ProfileRoleAuditEvent{
		OperationID: operationID, Action: repository.ProfileRoleAuditActionUpsert,
		ActorUserID: actorUserID, SubjectUserID: subjectUserID,
		Roles: append([]string(nil), normalizedRoles...), Revision: expectedRevision,
	}
	if err := m.appendRoleAudit(ctx, audit, repository.ProfileAuditOutcomeRequested, ""); err != nil {
		return nil, fmt.Errorf("initial profile role audit failed: %w", err)
	}
	binding := &repository.ProfileRoleBindingRecord{
		UserID: subjectUserID, Roles: append([]string(nil), normalizedRoles...), UpdatedBy: actorUserID,
	}
	if err := m.repository.UpsertProfileRoleBinding(ctx, binding, expectedRevision); err != nil {
		return nil, m.finishFailedRoleMutation(ctx, audit, err)
	}
	audit.Revision = binding.Revision
	if err := m.appendRoleAudit(ctx, audit, repository.ProfileAuditOutcomeSucceeded, ""); err != nil {
		return nil, fmt.Errorf("final profile role audit failed: %w", err)
	}
	return m.repository.GetProfileRoleBinding(ctx, subjectUserID)
}

func (m *ProfileAccessManager) DeleteBinding(ctx context.Context, actorUserID, subjectUserID uint64, expectedRevision int64) error {
	if err := m.requireDynamicAdmin(ctx, actorUserID); err != nil {
		return err
	}
	if subjectUserID == 0 || expectedRevision < 1 {
		return errors.New("subject user and expected revision are required")
	}
	current, err := m.repository.GetProfileRoleBinding(ctx, subjectUserID)
	if err != nil {
		return err
	}
	if profile.HasManagementRole(current.Roles, profile.ManagementRoleAdmin) && !m.IsRootAdmin(actorUserID) {
		return ErrProfileAdminRoleRequiresRoot
	}
	operationID, err := newProfileOperationID()
	if err != nil {
		return err
	}
	audit := repository.ProfileRoleAuditEvent{
		OperationID: operationID, Action: repository.ProfileRoleAuditActionDelete,
		ActorUserID: actorUserID, SubjectUserID: subjectUserID,
		Roles: append([]string(nil), current.Roles...), Revision: expectedRevision,
	}
	if err := m.appendRoleAudit(ctx, audit, repository.ProfileAuditOutcomeRequested, ""); err != nil {
		return fmt.Errorf("initial profile role audit failed: %w", err)
	}
	if err := m.repository.DeleteProfileRoleBinding(ctx, subjectUserID, expectedRevision); err != nil {
		return m.finishFailedRoleMutation(ctx, audit, err)
	}
	return m.appendRoleAudit(ctx, audit, repository.ProfileAuditOutcomeSucceeded, "")
}

func (m *ProfileAccessManager) IsRootAdmin(userID uint64) bool {
	if m == nil || userID == 0 {
		return false
	}
	_, ok := m.rootAdmins[userID]
	return ok
}

func (m *ProfileAccessManager) DynamicEnabled() bool {
	return m != nil && m.dynamicEnabled && m.repository != nil
}

func (m *ProfileAccessManager) addStaticRole(userIDs []uint64, role profile.ManagementRole) {
	for _, userID := range userIDs {
		if userID == 0 {
			continue
		}
		m.staticRoles[userID] = profile.MergeManagementRoles(m.staticRoles[userID], []string{string(role)})
	}
}

func (m *ProfileAccessManager) requireDynamicAdmin(ctx context.Context, actorUserID uint64) error {
	if m == nil || !m.dynamicEnabled || m.repository == nil {
		return ErrProfileDynamicRBACUnavailable
	}
	return m.RequireRole(ctx, actorUserID, profile.ManagementRoleAdmin)
}

func (m *ProfileAccessManager) appendRoleAudit(ctx context.Context, base repository.ProfileRoleAuditEvent, outcome, errorCode string) error {
	base.ID = [12]byte{}
	base.Outcome = outcome
	base.ErrorCode = errorCode
	return m.repository.AppendProfileRoleAuditEvent(ctx, &base)
}

func (m *ProfileAccessManager) finishFailedRoleMutation(ctx context.Context, audit repository.ProfileRoleAuditEvent, mutationErr error) error {
	if auditErr := m.appendRoleAudit(ctx, audit, repository.ProfileAuditOutcomeFailed, profileRoleMutationErrorCode(mutationErr)); auditErr != nil {
		return errors.Join(mutationErr, fmt.Errorf("final profile role audit failed: %w", auditErr))
	}
	return mutationErr
}

func profileRoleMutationErrorCode(err error) string {
	switch {
	case errors.Is(err, repository.ErrProfileRoleBindingNotFound):
		return "not_found"
	case errors.Is(err, repository.ErrProfileRoleBindingConflict):
		return "revision_conflict"
	default:
		return "persistence_failed"
	}
}
