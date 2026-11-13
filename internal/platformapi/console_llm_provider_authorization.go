package platformapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type consoleLLMProviderActor struct {
	organizationUUID  string
	accountUUID       string
	organizationAdmin bool
}

func resolveLLMProviderActor(r *http.Request, store consoleMemberActorStore, organizationUUID string) (consoleLLMProviderActor, error) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	actor := consoleLLMProviderActor{organizationUUID: organizationUUID, accountUUID: principal.AccountUUID}
	if principal.PlatformOrganizationAdmin(organizationUUID) {
		actor.organizationAdmin = true
		return actor, nil
	}
	member, err := store.GetAdminUser(r.Context(), organizationUUID, principal.UserExternalID)
	if err != nil {
		return consoleLLMProviderActor{}, err
	}
	actor.organizationAdmin = member.Role == "admin"
	return actor, nil
}

func (a consoleLLMProviderActor) canManage(provider db.LLMProvider) bool {
	return a.organizationUUID != "" && a.organizationUUID == provider.OrganizationUUID && (a.organizationAdmin || (a.accountUUID != "" && a.accountUUID == provider.CreatedByAccountUUID))
}

func authorizeLLMProviderManagement(w http.ResponseWriter, actor consoleLLMProviderActor, provider db.LLMProvider) bool {
	if actor.canManage(provider) {
		return true
	}
	writeLLMProviderManagementDenied(w)
	return false
}

func authorizeLLMProviderCreation(w http.ResponseWriter, actor consoleLLMProviderActor) bool {
	if actor.organizationUUID != "" && actor.accountUUID != "" {
		return true
	}
	writeLLMProviderCreatorRequired(w)
	return false
}

func llmProviderScope(w http.ResponseWriter, r *http.Request, database *db.DB) (string, consoleLLMProviderActor, bool) {
	orgUUID, ok := visibleOrgUUID(w, r)
	if !ok {
		return "", consoleLLMProviderActor{}, false
	}
	if chi.URLParam(r, "workspaceId") != "" {
		if _, ok := consoleWorkspaceScopeFromRequest(w, r, database, orgUUID); !ok {
			return "", consoleLLMProviderActor{}, false
		}
	}
	actor, err := resolveLLMProviderActor(r, database, orgUUID)
	if err != nil {
		writeConsoleMemberError(w, err)
		return "", consoleLLMProviderActor{}, false
	}
	return orgUUID, actor, true
}
