package platformapi

import (
	"context"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/auth"

	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
)

type bootstrapAccountStore interface {
	GetBootstrapUser(ctx context.Context, userExternalID string) (*UserRecord, error)
	ListBootstrapOrganizationsByEmail(ctx context.Context, email string) ([]UserOrganizationRecord, error)
}

func handleBootstrap(store OrganizationStore) http.HandlerFunc {
	bootstrapStore, _ := store.(bootstrapAccountStore)
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID := ""
		var account *Account
		if session, ok := platformsession.SessionFromContext(r.Context()); ok && bootstrapStore != nil {
			built, selectedOrgUUID, err := buildBootstrapAccount(r.Context(), bootstrapStore, session)
			if err != nil {
				internalError(w, "failed to load bootstrap account")
				return
			}
			account = &built
			orgUUID = selectedOrgUUID
		}
		response := buildBootstrapCompatibilityResponse(account, orgUUID != "", bootstrapGrowthbookHashingAlgorithm(r))
		if cookie, err := r.Cookie("sessionKey"); err == nil && account != nil {
			response.CSRFToken = platformsession.CSRFToken(cookie.Value)
		}
		if principal, ok := auth.PrincipalFromContext(r.Context()); ok {
			response.CurrentUserAccess = buildCurrentUserAccess(principal.WorkspaceAccess)
			if account != nil {
				account.Permissions = principal.WorkspaceAccess.Permissions()
			}
		}
		writeJSON(w, http.StatusOK, response)
	}
}

func buildBootstrapAccount(ctx context.Context, store bootstrapAccountStore, session platformsession.Session) (Account, string, error) {
	user, err := store.GetBootstrapUser(ctx, session.UserUUID)
	if err != nil {
		return Account{}, "", err
	}
	if user == nil {
		return Account{}, "", ErrNotFound
	}
	orgs, err := store.ListBootstrapOrganizationsByEmail(ctx, session.VerifiedEmail)
	if err != nil {
		return Account{}, "", err
	}
	preferredOrgUUID := session.OrganizationUUID
	for _, org := range orgs {
		if org.UUID == session.HomeOrganizationUUID {
			preferredOrgUUID = org.UUID
			break
		}
	}
	user.UUID = session.UserUUID
	user.Email = session.VerifiedEmail
	user.IsVerified = session.VerifiedEmail != ""
	return buildAccount(*user, orgs, preferredOrgUUID)
}
