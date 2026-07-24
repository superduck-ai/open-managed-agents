package platformapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/modelcatalog"

	"github.com/go-chi/chi/v5"
)

type bootstrapAccountStore interface {
	FindBootstrapUserContext(ctx context.Context, preferredOrgUUID string) (userExternalID string, orgUUID string, err error)
	GetBootstrapUser(ctx context.Context, userExternalID string) (*UserRecord, error)
	ListBootstrapUserOrganizations(ctx context.Context, userExternalID string, preferredOrgUUID string) ([]UserOrganizationRecord, error)
}

func handleBootstrap(store OrganizationStore, catalog modelcatalog.Reader) http.HandlerFunc {
	bootstrapStore, _ := store.(bootstrapAccountStore)
	return func(w http.ResponseWriter, r *http.Request) {
		models := loadPlatformModelCatalog(r.Context(), catalog)
		orgUUID := firstNonEmpty(chi.URLParam(r, "orgUuid"), "")
		userExternalID := ""
		if principal, ok := auth.PrincipalFromContext(r.Context()); ok {
			userExternalID = strings.TrimSpace(principal.UserExternalID)
			if orgUUID == "" {
				orgUUID = firstNonEmpty(principal.OrganizationUUID, principal.OrganizationExternalID)
			}
		}

		var account *Account
		if userExternalID != "" && bootstrapStore != nil {
			built, selectedOrgUUID, err := buildBootstrapAccount(r.Context(), bootstrapStore, userExternalID, orgUUID, models)
			if err != nil {
				internalError(w, "failed to load bootstrap account")
				return
			}
			account = &built
			if orgUUID == "" {
				orgUUID = selectedOrgUUID
			}
		}
		writeJSON(w, http.StatusOK, buildBootstrapCompatibilityResponse(account, orgUUID != "", bootstrapGrowthbookHashingAlgorithm(r), models))
	}
}

func buildBootstrapAccount(ctx context.Context, store bootstrapAccountStore, userExternalID string, preferredOrgUUID string, models platformModelCatalog) (Account, string, error) {
	user, err := store.GetBootstrapUser(ctx, userExternalID)
	if err != nil {
		return Account{}, "", err
	}
	if user == nil {
		return Account{}, "", ErrNotFound
	}
	orgs, err := store.ListBootstrapUserOrganizations(ctx, userExternalID, preferredOrgUUID)
	if err != nil {
		return Account{}, "", err
	}
	return buildAccount(*user, orgs, preferredOrgUUID, models)
}
