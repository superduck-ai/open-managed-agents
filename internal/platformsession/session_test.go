package platformsession

import "testing"

func TestPrincipalDoesNotInheritLegacyAPIKey(t *testing.T) {
	session := Session{UserUUID: "user", OrganizationUUID: "org", WorkspaceUUID: "workspace",
		APIKeyUUID: "legacy-key", APIKeyExternalID: "api_key_legacy"}
	principal := session.Principal()
	if principal.APIKeyUUID != "" || principal.APIKeyExternalID != "" {
		t.Fatal("platform credentials inherited an API key")
	}
	if principal.UserUUID != session.UserUUID || principal.WorkspaceUUID != session.WorkspaceUUID {
		t.Fatal("platform identity was lost")
	}
}
