package platformapi

import (
	"context"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type consoleMemberActorStore interface {
	GetAdminUser(context.Context, string, string) (db.AdminUser, error)
}

func requireConsoleOrganizationAdmin(store OrganizationStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			orgUUID, ok := visibleOrgUUID(w, r)
			if !ok {
				return
			}
			actorStore, ok := store.(consoleMemberActorStore)
			if !ok {
				internalError(w, "failed to authorize organization member operation")
				return
			}
			principal, _ := auth.PrincipalFromContext(r.Context())
			actor, err := actorStore.GetAdminUser(r.Context(), orgUUID, principal.UserExternalID)
			if err != nil {
				writeConsoleMemberError(w, err)
				return
			}
			if err := validateConsoleOrganizationAdminRole(actor.Role); err != nil {
				writeConsoleMemberError(w, err)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
