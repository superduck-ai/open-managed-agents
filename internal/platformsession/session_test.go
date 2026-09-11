package platformsession

import (
	"encoding/json"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
)

func TestStableSessionIdentity(t *testing.T) {
	if _, ok := SessionFromContext(t.Context()); ok {
		t.Fatal("空上下文不应包含会话")
	}
	session := Session{UserUUID: "original-user", UserExternalID: "user_original",
		OrganizationUUID: "source-org", VerifiedEmail: "verified@example.com", HomeOrganizationUUID: "home-org"}
	raw, err := json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	var restored Session
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	got, ok := SessionFromContext(WithSession(t.Context(), restored))
	if !ok || got != session {
		t.Fatalf("会话身份未完整保留: %+v", got)
	}
	if CSRFToken("key") != auth.HashSecret("csrf:key") || CSRFToken("key") == CSRFToken("other") {
		t.Fatal("CSRF 派生值错误")
	}
}

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
