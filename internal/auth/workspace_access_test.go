package auth

import (
	"slices"
	"testing"
)

func TestTwoRolePermissions(t *testing.T) {
	for _, role := range []string{"", "workspace_developer", "workspace_billing", "workspace_restricted_developer", "unknown"} {
		access := WorkspaceAccess{OrganizationRole: "user", Role: role}
		if len(access.Permissions()) != 0 || access.UseResources() || access.ManageOrganization() || access.ManageMembers() {
			t.Fatalf("unsupported role %q grants access", role)
		}
	}
	for _, role := range []string{"workspace_user", "workspace_admin"} {
		access := WorkspaceAccess{OrganizationRole: "user", Role: role}
		if !access.UseResources() {
			t.Fatalf("role %q cannot use workspace resources", role)
		}
		if access.ManageOrganization() || slices.Contains(access.Permissions(), "members:manage") {
			t.Fatalf("workspace role %q grants organization management", role)
		}
		if access.ManageMembers() != (role == "workspace_admin") {
			t.Fatalf("role %q has incorrect member permission", role)
		}
	}
	access := WorkspaceAccess{OrganizationRole: "admin", Role: "workspace_admin"}
	if !access.ManageOrganization() || !slices.Contains(access.Permissions(), "members:manage") {
		t.Fatal("organization administrator cannot manage organization")
	}
}
