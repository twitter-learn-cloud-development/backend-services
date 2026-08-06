package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"twitter-clone/internal/module/agent/profile"
	"twitter-clone/internal/module/agent/repository"
)

func TestProfileAccessManagerMergesStaticAndDynamicRoles(t *testing.T) {
	store := newProfileRoleStoreFake()
	store.bindings[20] = &repository.ProfileRoleBindingRecord{UserID: 20, Roles: []string{"approver"}, Revision: 1}
	manager := NewProfileAccessManager(store, ProfileStaticRoleAssignments{EditorUserIDs: []uint64{20}}, true)

	access, err := manager.ResolveAccess(context.Background(), 20)
	if err != nil {
		t.Fatalf("ResolveAccess() error = %v", err)
	}
	if !profile.HasManagementRole(access.Roles, profile.ManagementRoleEditor) ||
		!profile.HasManagementRole(access.Roles, profile.ManagementRoleApprover) {
		t.Fatalf("merged roles = %v", access.Roles)
	}
	if len(access.StaticRoles) != 1 || len(access.DynamicRoles) != 1 {
		t.Fatalf("access sources = %+v", access)
	}
}

func TestProfileAccessManagerOnlyRootMayGrantAdmin(t *testing.T) {
	store := newProfileRoleStoreFake()
	store.bindings[2] = &repository.ProfileRoleBindingRecord{UserID: 2, Roles: []string{"admin"}, Revision: 1}
	manager := NewProfileAccessManager(store, ProfileStaticRoleAssignments{AdminUserIDs: []uint64{1}}, true)

	if _, err := manager.UpsertBinding(context.Background(), 2, 3, []string{"admin"}, 0); !errors.Is(err, ErrProfileAdminRoleRequiresRoot) {
		t.Fatalf("dynamic admin grant error = %v", err)
	}
	binding, err := manager.UpsertBinding(context.Background(), 1, 3, []string{"viewer", "admin"}, 0)
	if err != nil {
		t.Fatalf("root UpsertBinding() error = %v", err)
	}
	if binding.Revision != 1 || len(store.audits) != 2 {
		t.Fatalf("binding = %+v, audits = %d", binding, len(store.audits))
	}
}

func TestProfileAccessManagerStaticRootSurvivesRepositoryFailure(t *testing.T) {
	store := newProfileRoleStoreFake()
	store.getErr = errors.New("mongo unavailable")
	manager := NewProfileAccessManager(store, ProfileStaticRoleAssignments{AdminUserIDs: []uint64{1}}, true)

	if err := manager.RequireRole(context.Background(), 1, profile.ManagementRoleAdmin); err != nil {
		t.Fatalf("static root access error = %v", err)
	}
	if err := manager.RequireRole(context.Background(), 9, profile.ManagementRoleViewer); err == nil {
		t.Fatal("dynamic-only user was authorized while repository was unavailable")
	}
}

type profileRoleStoreFake struct {
	mu       sync.Mutex
	bindings map[uint64]*repository.ProfileRoleBindingRecord
	audits   []*repository.ProfileRoleAuditEvent
	getErr   error
}

func newProfileRoleStoreFake() *profileRoleStoreFake {
	return &profileRoleStoreFake{bindings: make(map[uint64]*repository.ProfileRoleBindingRecord)}
}

func (f *profileRoleStoreFake) GetProfileRoleBinding(_ context.Context, userID uint64) (*repository.ProfileRoleBindingRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return nil, f.getErr
	}
	record, ok := f.bindings[userID]
	if !ok {
		return nil, repository.ErrProfileRoleBindingNotFound
	}
	return cloneProfileRoleBinding(record), nil
}

func (f *profileRoleStoreFake) ListProfileRoleBindings(_ context.Context, _, _ int) ([]*repository.ProfileRoleBindingRecord, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]*repository.ProfileRoleBindingRecord, 0, len(f.bindings))
	for _, record := range f.bindings {
		result = append(result, cloneProfileRoleBinding(record))
	}
	return result, int64(len(result)), nil
}

func (f *profileRoleStoreFake) UpsertProfileRoleBinding(_ context.Context, binding *repository.ProfileRoleBindingRecord, expectedRevision int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	current, exists := f.bindings[binding.UserID]
	if expectedRevision == 0 {
		if exists {
			return repository.ErrProfileRoleBindingConflict
		}
		binding.Revision = 1
		binding.CreatedBy = binding.UpdatedBy
		binding.CreatedAt = time.Now()
	} else if !exists {
		return repository.ErrProfileRoleBindingNotFound
	} else if current.Revision != expectedRevision {
		return repository.ErrProfileRoleBindingConflict
	} else {
		binding.Revision = expectedRevision + 1
		binding.CreatedBy = current.CreatedBy
		binding.CreatedAt = current.CreatedAt
	}
	binding.UpdatedAt = time.Now()
	f.bindings[binding.UserID] = cloneProfileRoleBinding(binding)
	return nil
}

func (f *profileRoleStoreFake) DeleteProfileRoleBinding(_ context.Context, userID uint64, expectedRevision int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	current, exists := f.bindings[userID]
	if !exists {
		return repository.ErrProfileRoleBindingNotFound
	}
	if current.Revision != expectedRevision {
		return repository.ErrProfileRoleBindingConflict
	}
	delete(f.bindings, userID)
	return nil
}

func (f *profileRoleStoreFake) AppendProfileRoleAuditEvent(_ context.Context, event *repository.ProfileRoleAuditEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	copyEvent := *event
	copyEvent.Roles = append([]string(nil), event.Roles...)
	f.audits = append(f.audits, &copyEvent)
	return nil
}

func (f *profileRoleStoreFake) ListProfileRoleAuditEvents(_ context.Context, _, _ int) ([]*repository.ProfileRoleAuditEvent, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]*repository.ProfileRoleAuditEvent, 0, len(f.audits))
	for _, event := range f.audits {
		copyEvent := *event
		copyEvent.Roles = append([]string(nil), event.Roles...)
		result = append(result, &copyEvent)
	}
	return result, int64(len(result)), nil
}

func cloneProfileRoleBinding(record *repository.ProfileRoleBindingRecord) *repository.ProfileRoleBindingRecord {
	if record == nil {
		return nil
	}
	copyRecord := *record
	copyRecord.Roles = append([]string(nil), record.Roles...)
	return &copyRecord
}
