package profile

import "testing"

func TestManagementRolesNormalizeAndAuthorize(t *testing.T) {
	roles, err := NormalizeManagementRoles([]string{"approver", "viewer", "APPROVER"})
	if err != nil {
		t.Fatalf("NormalizeManagementRoles() error = %v", err)
	}
	if len(roles) != 2 || roles[0] != "viewer" || roles[1] != "approver" {
		t.Fatalf("roles = %v", roles)
	}
	if !HasManagementRole([]string{"editor"}, ManagementRoleViewer) {
		t.Fatal("editor should inherit viewer access")
	}
	if HasManagementRole([]string{"editor"}, ManagementRoleApprover) {
		t.Fatal("editor unexpectedly inherited approver access")
	}
	if !HasManagementRole([]string{"admin"}, ManagementRoleApprover) {
		t.Fatal("admin should inherit approver access")
	}
}

func TestNormalizeManagementRolesRejectsUnknownRole(t *testing.T) {
	if _, err := NormalizeManagementRoles([]string{"owner"}); err == nil {
		t.Fatal("NormalizeManagementRoles() accepted an unknown role")
	}
}
