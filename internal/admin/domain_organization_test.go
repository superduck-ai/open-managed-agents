package admin

import "testing"

func TestOrganizationRoles(t *testing.T) {
	for _, role := range []string{"", "developer", "billing", "claude_code_user", "super_admin", "workspace_admin"} {
		if validateOrganizationRole(role) == nil {
			t.Fatalf("unsupported role %q accepted", role)
		}
	}
	for _, role := range []string{"user", "admin"} {
		if err := validateOrganizationRole(role); err != nil {
			t.Fatalf("role %q: %v", role, err)
		}
	}
}
