package devinit

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"uuid"
)

func prepareResourceIdentities(layout Layout, spec Spec, state *State, credentials *Credentials) error {
	if state.Organization == nil {
		if err := reserveIdentity(spec, state); err != nil {
			return err
		}
	}
	for index := range state.Workspaces {
		workspace := &state.Workspaces[index]
		if workspace.Environment == nil {
			externalID, err := ids.New("env_")
			if err != nil {
				return err
			}
			workspace.Environment = &EnvironmentState{UUID: uuid.NewV4().String(), ExternalID: externalID, Pending: true}
		}
		if err := reserveAPIKeyIdentities(workspace, spec, credentials); err != nil {
			return err
		}
	}
	if err := SaveState(layout, *state); err != nil {
		return err
	}
	return SaveCredentials(layout, *credentials)
}

func reserveIdentity(spec Spec, state *State) error {
	externalID, err := ids.New("user_")
	if err != nil {
		return err
	}
	state.Organization = &OrganizationState{UUID: uuid.NewV4().String(), Name: spec.OrganizationName}
	state.User = &UserState{ExternalID: externalID, Email: spec.UserEmail, Name: spec.UserName}
	state.IdentityPending = true
	for _, workspace := range spec.Workspaces {
		externalID, err := ids.New("wrkspc_")
		if err != nil {
			return err
		}
		state.Workspaces = append(state.Workspaces, WorkspaceState{
			Ref: workspace.Ref, UUID: uuid.NewV4().String(), ExternalID: externalID,
			Name: workspace.Name, IsDefault: workspace.IsDefault,
		})
	}
	return nil
}

func reserveAPIKeyIdentities(workspace *WorkspaceState, spec Spec, credentials *Credentials) error {
	for _, workspaceSpec := range spec.Workspaces {
		if workspaceSpec.Ref != workspace.Ref {
			continue
		}
		for _, key := range workspaceSpec.APIKeys {
			if err := reserveAPIKey(workspace, key, credentials); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("registered workspace %q is missing from the init file", workspace.Ref)
}

func reserveAPIKey(workspace *WorkspaceState, spec APIKeySpec, credentials *Credentials) error {
	for index := range credentials.APIKeys {
		credential := &credentials.APIKeys[index]
		if credential.WorkspaceRef != workspace.Ref || credential.Ref != spec.Ref {
			continue
		}
		for _, key := range workspace.APIKeys {
			if key.Ref == spec.Ref {
				credential.ExternalID = key.ExternalID
				return nil
			}
		}
		externalID := credential.ExternalID
		if externalID == "" {
			var err error
			externalID, err = ids.New("apikey_")
			if err != nil {
				return err
			}
		}
		credential.ExternalID = externalID
		workspace.APIKeys = append(workspace.APIKeys, APIKeyState{Ref: spec.Ref, ExternalID: externalID, Name: spec.Name, Pending: true})
		return nil
	}
	return fmt.Errorf("credentials missing api key %q/%q", workspace.Ref, spec.Ref)
}

func devIdentityInput(spec Spec, state State) db.DevIdentity {
	input := db.DevIdentity{
		OrganizationUUID: state.Organization.UUID, OrganizationName: state.Organization.Name,
		User: db.AdminUser{ExternalID: state.User.ExternalID, Email: state.User.Email, Name: state.User.Name, Role: spec.UserRole},
	}
	for _, workspace := range state.Workspaces {
		input.Workspaces = append(input.Workspaces, db.AdminWorkspace{
			UUID: workspace.UUID, ExternalID: workspace.ExternalID, OrganizationUUID: state.Organization.UUID,
			Name: workspace.Name, IsDefault: workspace.IsDefault, CreatedAt: time.Now().UTC(),
			DataResidency: json.RawMessage(`{}`), Tags: json.RawMessage(`{}`),
		})
	}
	return input
}
