package platformapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestLLMProviderManagementPolicy(t *testing.T) {
	for _, tc := range []struct {
		name    string
		actor   consoleLLMProviderActor
		creator string
		allowed bool
	}{
		{"other creator", consoleLLMProviderActor{"org", "other", false}, "owner", false},
		{"unknown creator", consoleLLMProviderActor{"org", "owner", false}, "", false},
		{"missing actor", consoleLLMProviderActor{"org", "", false}, "", false},
		{"foreign organization admin", consoleLLMProviderActor{"other", "owner", true}, "owner", false},
		{"owner", consoleLLMProviderActor{"org", "owner", false}, "owner", true},
		{"admin", consoleLLMProviderActor{"org", "other", true}, "owner", true},
		{"admin legacy", consoleLLMProviderActor{"org", "other", true}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := db.LLMProvider{OrganizationUUID: "org", CreatedByAccountUUID: tc.creator, CreatedByName: "Creator"}
			response := formatLLMProvider(provider, tc.actor)
			if response.CanManage != tc.allowed || tc.actor.canManage(provider) != tc.allowed {
				t.Fatalf("can_manage = %v, want %v", response.CanManage, tc.allowed)
			}
			if (response.CreatedBy == nil) != (tc.creator == "") {
				t.Fatalf("creator = %#v", response.CreatedBy)
			}
			if response.CreatedBy != nil && (response.CreatedBy.AccountUUID != tc.creator || response.CreatedBy.Name != "Creator") {
				t.Fatalf("creator = %#v", response.CreatedBy)
			}
		})
	}
}

func TestLLMProviderActorUsesAccountAndOrganizationRole(t *testing.T) {
	for _, role := range []string{"user", "admin"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req = req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{AccountUUID: "account", UserUUID: "membership", UserExternalID: "member", OrganizationUUID: "org", WorkspaceAccess: auth.WorkspaceAccess{Role: "workspace_admin"}}))
		actor, err := resolveLLMProviderActor(req, memberActorFixture{role}, "org")
		if err != nil {
			t.Fatal(err)
		}
		if actor.accountUUID != "account" || actor.organizationAdmin != (role == "admin") {
			t.Fatalf("actor = %#v", actor)
		}
	}
}

func TestUnauthorizedSavedPreviewStopsBeforeDecrypt(t *testing.T) {
	response := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	syncProviderFromUpstream(response, req, nil, nil, nil, db.LLMProvider{OrganizationUUID: "org", CreatedByAccountUUID: "other"}, consoleLLMProviderActor{"org", "actor", false}, false)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 before decrypt", response.Code)
	}
}

func TestLLMProviderCreationRequiresStableAccount(t *testing.T) {
	for _, tc := range []struct {
		name    string
		actor   consoleLLMProviderActor
		allowed bool
	}{
		{"missing account", consoleLLMProviderActor{"org", "", false}, false},
		{"admin missing account", consoleLLMProviderActor{"org", "", true}, false},
		{"member account", consoleLLMProviderActor{"org", "account", false}, true},
		{"admin account", consoleLLMProviderActor{"org", "account", true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			if allowed := authorizeLLMProviderCreation(response, tc.actor); allowed != tc.allowed {
				t.Fatalf("creation allowed = %v, want %v", allowed, tc.allowed)
			}
			if !tc.allowed && response.Code != http.StatusForbidden {
				t.Fatalf("missing account status = %d", response.Code)
			}
		})
	}
}
