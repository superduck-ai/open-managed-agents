package memory

import (
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"testing"
)

func TestPlatformMemoryActorUsesStableAccount(t *testing.T) {
	principal := auth.Principal{AccountUUID: "account", AccountExternalID: "user_account", OrganizationUUID: "target", CredentialType: auth.CredentialTypePlatformSession, OrganizationAccessSource: auth.OrganizationAccessPlatformAdmin, WorkspaceAccess: auth.WorkspaceAccess{OrganizationRole: "admin", Role: "workspace_admin", Source: "platform_admin"}}
	actor := principalActor(principal)
	if actor.Type != "user_actor" || actor.UserID != "user_account" {
		t.Fatalf("actor=%+v", actor)
	}
	response := responseFromActor(actor)
	if response.Type != "user_actor" {
		t.Fatalf("response=%+v", response)
	}
}
