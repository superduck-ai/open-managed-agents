package platformapi

import (
	"context"
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type consoleAPIKeyCreatorStore interface {
	GetPlatformAdminAccount(context.Context, string) (db.PlatformAdminAccount, error)
	GetActivePlatformUserByEmail(context.Context, string, string) (db.AdminUser, error)
}

var _ consoleAPIKeyCreatorStore = (*db.DB)(nil)

func ResolveConsoleAPIKeyCreator(ctx context.Context, store OrganizationStore, principal auth.Principal) (*string, error) {
	if principal.UserUUID != "" {
		return new(principal.UserUUID), nil
	}
	if !principal.PlatformOrganizationAdmin(principal.OrganizationUUID) {
		return nil, nil
	}
	resolver, ok := store.(consoleAPIKeyCreatorStore)
	if !ok {
		return nil, errConsoleAPIKeyCreatorUnavailable
	}
	account, err := resolver.GetPlatformAdminAccount(ctx, principal.AccountUUID)
	if err != nil {
		return nil, err
	}
	member, err := resolver.GetActivePlatformUserByEmail(ctx, principal.OrganizationUUID, account.Email)
	if errors.Is(err, db.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return new(member.UUID), nil
}
