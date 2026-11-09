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

type PlatformAccessResolver func(context.Context, platformsession.Session) (PlatformAccess, error)

func handleBootstrap(store OrganizationStore, resolveAccess PlatformAccessResolver) http.HandlerFunc {
	bootstrapStore, _ := store.(bootstrapAccountStore)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		orgUUID := ""
		var account *Account
		platformAccess := PlatformAccess{Permissions: []string{}}
		if session, ok := platformsession.SessionFromContext(r.Context()); ok && bootstrapStore != nil {
			built, selectedOrgUUID, err := buildBootstrapAccount(r.Context(), bootstrapStore, session)
			if err != nil {
				internalError(w, "failed to load bootstrap account")
				return
			}
			account = &built
			orgUUID = selectedOrgUUID
			if resolveAccess != nil {
				platformAccess, err = resolveAccess(r.Context(), session)
				if err != nil {
					platformAccessUnavailable(w)
					return
				}
			}
		}
		var organizationAccess *OrganizationAccess
		if principal, ok := auth.PrincipalFromContext(r.Context()); ok && principal.OrganizationUUID != "" {
			getter, available := store.(platformOrganizationGetter)
			if !available {
				internalError(w, "failed to load current organization")
				return
			}
			organization, err := getter.GetPlatformOrganization(r.Context(), principal.OrganizationUUID)
			if err != nil || organization == nil {
				internalError(w, "failed to load current organization")
				return
			}
			orgUUID = organization.UUID
			organizationAccess = &OrganizationAccess{Source: principal.OrganizationAccessSource, Organization: OrganizationAccessOrganization{UUID: organization.UUID, Name: organization.Name}, OrganizationRole: principal.WorkspaceAccess.OrganizationRole, WorkspaceRole: principal.WorkspaceAccess.Role}
		}
		response := buildBootstrapCompatibilityResponse(account, orgUUID != "", bootstrapGrowthbookHashingAlgorithm(r))
		response.PlatformAccess = platformAccess
		response.OrganizationAccess = organizationAccess
		if principal, ok := auth.PrincipalFromContext(r.Context()); ok {
			response.CurrentUserAccess = buildCurrentUserAccess(principal.WorkspaceAccess)
			if account != nil {
				account.Permissions = principal.WorkspaceAccess.Permissions()
			}
		}
		if account != nil {
			if sessionKey := auth.ExtractPlatformSessionKey(r); sessionKey != "" {
				response.CSRFToken = auth.PlatformCSRFToken(sessionKey)
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
