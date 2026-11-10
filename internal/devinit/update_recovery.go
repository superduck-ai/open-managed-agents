package devinit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
	"uuid"
)

func recoverMissingOrganization(ctx context.Context, database *db.DB, prepared *preparedRun, opts Options) error {
	if !opts.Update || prepared.state.Organization == nil || prepared.state.IdentityPending {
		return nil
	}
	organizationUUID, err := uuid.Parse(prepared.state.Organization.UUID)
	if err != nil {
		return err
	}
	_, err = database.GetAdminOrganization(ctx, organizationUUID)
	if !errors.Is(err, platform.ErrNotFound) {
		return err
	}
	candidate, err := recoveryOrganization(ctx, database, prepared.spec)
	if err != nil {
		return err
	}
	var restore func(*State) error
	if candidate != nil {
		restore = func(state *State) error {
			return adoptRecoveryOrganization(ctx, database, state, *candidate)
		}
	}
	if err := resetResourceRegistration(prepared, restore); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Registered organization is missing; --update is recovering resource identities and rotating local API keys. Previous registration is saved in update-recovery.json.")
	return nil
}

func resetResourceRegistration(prepared *preparedRun, restore func(*State) error) error {
	backup := struct {
		State       State       `json:"state"`
		Credentials Credentials `json:"credentials"`
	}{prepared.state, prepared.credentials}
	if err := writePrivateFile(filepath.Join(prepared.layout.SecretsDir, "update-recovery.json"), 0o600, backup); err != nil {
		return err
	}
	state := State{EnvID: prepared.state.EnvID, Name: prepared.state.Name, Phase: phaseMigrated, Runtime: prepared.state.Runtime}
	if err := reserveIdentity(prepared.spec, &state); err != nil {
		return err
	}
	if restore != nil {
		if err := restore(&state); err != nil {
			return err
		}
	}
	credentials := prepared.credentials
	credentials.APIKeys = append([]APICredential(nil), credentials.APIKeys...)
	for i := range credentials.APIKeys {
		key, err := NewAPIKeyValue()
		if err != nil {
			return err
		}
		credentials.APIKeys[i].ExternalID = ""
		credentials.APIKeys[i].Key = key
	}
	if err := prepareResourceIdentities(prepared.layout, prepared.spec, &state, &credentials); err != nil {
		return err
	}
	prepared.state, prepared.credentials = state, credentials
	prepared.identitiesPrepared = true
	return nil
}

func recoveryOrganization(ctx context.Context, database *db.DB, spec Spec) (*platform.UserOrganizationRecord, error) {
	organizations, err := database.ListBootstrapOrganizationsByEmail(ctx, spec.UserEmail)
	if err != nil {
		return nil, err
	}
	return selectRecoveryOrganization(organizations, spec.OrganizationName)
}

func selectRecoveryOrganization(organizations []platform.UserOrganizationRecord, name string) (*platform.UserOrganizationRecord, error) {
	var candidate *platform.UserOrganizationRecord
	for _, organization := range organizations {
		if organization.Name != name {
			continue
		}
		if candidate != nil {
			return nil, fmt.Errorf("multiple organizations named %q contain the configured user; refusing ambiguous recovery", name)
		}
		if organization.Role != "admin" {
			return nil, fmt.Errorf("configured user is not an admin of recovery organization %s", organization.UUID)
		}
		candidate = new(organization)
	}
	return candidate, nil
}

func adoptRecoveryOrganization(ctx context.Context, database *db.DB, state *State, organization platform.UserOrganizationRecord) error {
	user, err := database.GetAdminUser(ctx, organization.UUID, organization.UserExternalID)
	if err != nil {
		return err
	}
	workspaces, err := database.ListConsoleWorkspaces(ctx, organization.UUID, true)
	if err != nil {
		return err
	}
	if user.Name != state.User.Name {
		return fmt.Errorf("recovery user name differs from configured user")
	}
	state.Organization = &OrganizationState{UUID: organization.UUID, Name: organization.Name}
	state.User = &UserState{UUID: user.UUID, ExternalID: user.ExternalID, Email: user.Email, Name: user.Name}
	for i := range state.Workspaces {
		workspace := &state.Workspaces[i]
		found := false
		for _, existing := range workspaces {
			if existing.IsDefault && workspace.IsDefault && existing.Name != workspace.Name {
				return fmt.Errorf("recovery organization has a different default workspace")
			}
			if existing.Name != workspace.Name {
				continue
			}
			if found || existing.ArchivedAt != nil || existing.IsDefault != workspace.IsDefault {
				return fmt.Errorf("workspace %q is ambiguous or has a different default flag", workspace.Name)
			}
			workspace.UUID, workspace.ExternalID = existing.UUID, existing.ExternalID
			found = true
		}
	}
	return nil
}
