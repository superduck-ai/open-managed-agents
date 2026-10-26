package devinit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
)

func reconcileAPIKeys(ctx context.Context, database *db.DB, spec Spec, state *State, credentials *Credentials, opts Options) error {
	organizationUUID := state.Organization.UUID
	userUUID := state.User.UUID
	byWorkspace := map[string]*WorkspaceState{}
	for i := range state.Workspaces {
		byWorkspace[state.Workspaces[i].Ref] = &state.Workspaces[i]
	}
	for _, workspaceSpec := range spec.Workspaces {
		workspace := byWorkspace[workspaceSpec.Ref]
		if workspace == nil {
			return fmt.Errorf("workspace %q missing from state", workspaceSpec.Ref)
		}
		keys, err := reconcileWorkspaceAPIKeys(ctx, database, organizationUUID, userUUID, *workspace, workspaceSpec, credentials, opts)
		if err != nil {
			return err
		}
		workspace.APIKeys = keys
	}
	return nil
}

func reconcileWorkspaceAPIKeys(ctx context.Context, database *db.DB, organizationUUID, userUUID string, workspace WorkspaceState, spec WorkspaceSpec, credentials *Credentials, opts Options) ([]APIKeyState, error) {
	registered := map[string]APIKeyState{}
	for _, key := range workspace.APIKeys {
		registered[key.Ref] = key
	}
	result := make([]APIKeyState, 0, len(spec.APIKeys))
	for _, keySpec := range spec.APIKeys {
		credential, found := findAPICredential(credentials, workspace.Ref, keySpec.Ref)
		if !found {
			return nil, fmt.Errorf("credentials missing api key %q/%q; the plaintext cannot be recovered from the database hash, rotate it explicitly", workspace.Ref, keySpec.Ref)
		}
		stateKey := registered[keySpec.Ref]
		externalID := stateKey.ExternalID
		if externalID == "" {
			return nil, fmt.Errorf("api key %q identity has not been reserved", keySpec.Ref)
		}
		existing, err := database.GetAdminAPIKey(ctx, organizationUUID, externalID)
		if errors.Is(err, platform.ErrNotFound) {
			if !stateKey.Pending {
				return nil, fmt.Errorf("registered api key %s was deleted; refusing to recreate it", externalID)
			}
			if _, createErr := database.CreateConsoleAPIKeyWithRawKey(ctx, platform.CreateConsoleAPIKeyInput{
				OrgUUID:            organizationUUID,
				WorkspaceUUID:      workspace.UUID,
				WorkspaceDisplayID: workspace.ExternalID,
				Name:               keySpec.Name,
				CreatedByUserUUID:  &userUUID,
			}, externalID, credential.Key); createErr != nil {
				return nil, createErr
			}
			stored, lookupErr := database.GetAdminAPIKey(ctx, organizationUUID, externalID)
			if lookupErr != nil {
				return nil, lookupErr
			}
			result = append(result, APIKeyState{Ref: keySpec.Ref, UUID: stored.UUID, ExternalID: externalID, Name: keySpec.Name})
			continue
		}
		if err != nil {
			return nil, err
		}
		if existing.ExpiresAt != nil && !existing.ExpiresAt.After(time.Now()) {
			return nil, fmt.Errorf("api key %s is expired; refusing to restore it", externalID)
		}
		if existing.Status != "active" {
			return nil, fmt.Errorf("api key %q (%s) status is %q; refusing to silently restore or rotate it", keySpec.Name, externalID, existing.Status)
		}
		if existing.WorkspaceUUID != workspace.UUID {
			return nil, fmt.Errorf("api key %s belongs to another workspace; refusing to take it over", externalID)
		}
		if existing.Name != keySpec.Name {
			if !opts.Update {
				return nil, fmt.Errorf("api key %q name %q differs from init file %q; rerun with --update", keySpec.Ref, existing.Name, keySpec.Name)
			}
			if err := database.UpdateAdminAPIKey(ctx, organizationUUID, externalID, true, keySpec.Name, false, ""); err != nil {
				return nil, err
			}
		}
		result = append(result, APIKeyState{Ref: keySpec.Ref, UUID: existing.UUID, ExternalID: externalID, Name: keySpec.Name})
	}
	return result, nil
}

func findAPICredential(credentials *Credentials, workspaceRef, keyRef string) (APICredential, bool) {
	for _, credential := range credentials.APIKeys {
		if credential.WorkspaceRef == workspaceRef && credential.Ref == keyRef {
			return credential, true
		}
	}
	return APICredential{}, false
}
