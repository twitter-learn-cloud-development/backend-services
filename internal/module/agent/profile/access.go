package profile

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

var ErrInvalidManagementRole = errors.New("invalid Agent Profile management role")

type ManagementRole string

const (
	ManagementRoleViewer   ManagementRole = "viewer"
	ManagementRoleEditor   ManagementRole = "editor"
	ManagementRoleApprover ManagementRole = "approver"
	ManagementRoleAdmin    ManagementRole = "admin"
)

var managementRoleOrder = []ManagementRole{
	ManagementRoleViewer,
	ManagementRoleEditor,
	ManagementRoleApprover,
	ManagementRoleAdmin,
}

func ParseManagementRole(value string) (ManagementRole, error) {
	role := ManagementRole(strings.ToLower(strings.TrimSpace(value)))
	for _, allowed := range managementRoleOrder {
		if role == allowed {
			return role, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrInvalidManagementRole, value)
}

func NormalizeManagementRoles(values []string) ([]string, error) {
	seen := make(map[ManagementRole]struct{}, len(values))
	for _, value := range values {
		role, err := ParseManagementRole(value)
		if err != nil {
			return nil, err
		}
		seen[role] = struct{}{}
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("%w: at least one role is required", ErrInvalidManagementRole)
	}
	roles := make([]string, 0, len(seen))
	for _, role := range managementRoleOrder {
		if _, ok := seen[role]; ok {
			roles = append(roles, string(role))
		}
	}
	return roles, nil
}

func HasManagementRole(values []string, required ManagementRole) bool {
	if required == "" {
		return false
	}
	hasAny := false
	for _, value := range values {
		role, err := ParseManagementRole(value)
		if err != nil {
			continue
		}
		hasAny = true
		if role == ManagementRoleAdmin || role == required {
			return true
		}
	}
	return required == ManagementRoleViewer && hasAny
}

func MergeManagementRoles(groups ...[]string) []string {
	seen := make(map[string]struct{})
	for _, group := range groups {
		for _, value := range group {
			role, err := ParseManagementRole(value)
			if err == nil {
				seen[string(role)] = struct{}{}
			}
		}
	}
	roles := make([]string, 0, len(seen))
	for role := range seen {
		roles = append(roles, role)
	}
	sort.Slice(roles, func(i, j int) bool {
		return managementRoleRank(roles[i]) < managementRoleRank(roles[j])
	})
	return roles
}

func managementRoleRank(role string) int {
	for index, candidate := range managementRoleOrder {
		if role == string(candidate) {
			return index
		}
	}
	return len(managementRoleOrder)
}
