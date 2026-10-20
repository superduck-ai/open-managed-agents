package devinit

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"

	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

func reconcileProviders(ctx context.Context, database *db.DB, vaultSecrets *secrets.Service, providers ModelProvidersSpec, state *State, opts Options) error {
	result := make([]ProviderState, 0, len(providers.Providers)*len(state.Workspaces))
	for _, workspace := range state.Workspaces {
		for _, provider := range providers.Providers {
			record, err := reconcileProvider(ctx, database, vaultSecrets, state.Organization.UUID, workspace, provider, opts)
			if err != nil {
				return fmt.Errorf("workspace %q provider %q: %w", workspace.Ref, provider.Name, err)
			}
			result = append(result, record)
		}
	}
	state.Providers = result
	return nil
}

func reconcileProvider(ctx context.Context, database *db.DB, vaultSecrets *secrets.Service, organizationUUID string, workspace WorkspaceState, spec ProviderSpec, opts Options) (ProviderState, error) {
	existingList, err := database.ListLLMProviders(ctx, organizationUUID, workspace.UUID)
	if err != nil {
		return ProviderState{}, err
	}
	var existing *db.LLMProvider
	for i := range existingList {
		if existingList[i].Name == spec.Name {
			existing = &existingList[i]
			break
		}
	}
	if existing == nil {
		provider, err := createProvider(ctx, database, vaultSecrets, organizationUUID, workspace, spec)
		if err != nil {
			return ProviderState{}, err
		}
		return ProviderState{WorkspaceRef: workspace.Ref, Name: spec.Name, ExternalID: provider.ExternalID}, nil
	}
	return updateProvider(ctx, database, vaultSecrets, *existing, workspace.Ref, spec, opts)
}

func createProvider(ctx context.Context, database *db.DB, vaultSecrets *secrets.Service, organizationUUID string, workspace WorkspaceState, spec ProviderSpec) (db.LLMProvider, error) {
	externalID, err := ids.New("llmprov_")
	if err != nil {
		return db.LLMProvider{}, err
	}
	provider := db.LLMProvider{
		UUID:             uuid.NewV4().String(),
		ExternalID:       externalID,
		OrganizationUUID: organizationUUID,
		WorkspaceUUID:    workspace.UUID,
		Name:             spec.Name,
		APIBackend:       spec.APIBackend,
		BaseURL:          spec.BaseURL,
		ModelIDs:         spec.ModelIDs,
	}
	envelope, err := vaultSecrets.Seal(ctx, llmproviders.SecretBinding(provider), []byte(spec.APIKey))
	if err != nil {
		return db.LLMProvider{}, err
	}
	provider.SecretEnvelope = &envelope
	created, err := database.CreateLLMProvider(ctx, provider)
	if errors.Is(err, db.ErrDuplicate) || isModelConflict(err) {
		return db.LLMProvider{}, fmt.Errorf("model ids are already owned by another provider in this workspace: %w", err)
	}
	return created, err
}

func updateProvider(ctx context.Context, database *db.DB, vaultSecrets *secrets.Service, existing db.LLMProvider, workspaceRef string, spec ProviderSpec, opts Options) (ProviderState, error) {
	changed := existing.BaseURL != spec.BaseURL || existing.APIBackend != spec.APIBackend || !equalStrings(existing.ModelIDs, spec.ModelIDs)
	keyChanged, err := providerKeyChanged(ctx, vaultSecrets, existing, spec.APIKey)
	if err != nil {
		return ProviderState{}, err
	}
	changed = changed || keyChanged
	if !changed {
		return ProviderState{WorkspaceRef: workspaceRef, Name: existing.Name, ExternalID: existing.ExternalID}, nil
	}
	if !opts.Update {
		return ProviderState{}, fmt.Errorf("provider differs from the init file; rerun with --update to apply base_url/backend/model/key changes")
	}
	existing.BaseURL = spec.BaseURL
	existing.APIBackend = spec.APIBackend
	existing.ModelIDs = spec.ModelIDs
	if keyChanged {
		envelope, err := vaultSecrets.Seal(ctx, llmproviders.SecretBinding(existing), []byte(spec.APIKey))
		if err != nil {
			return ProviderState{}, err
		}
		existing.SecretEnvelope = &envelope
	}
	updated, err := database.UpdateLLMProvider(ctx, existing)
	if isModelConflict(err) {
		return ProviderState{}, fmt.Errorf("model ids are already owned by another provider in this workspace: %w", err)
	}
	if err != nil {
		return ProviderState{}, err
	}
	return ProviderState{WorkspaceRef: workspaceRef, Name: updated.Name, ExternalID: updated.ExternalID}, nil
}

func providerKeyChanged(ctx context.Context, vaultSecrets *secrets.Service, existing db.LLMProvider, apiKey string) (bool, error) {
	if existing.SecretEnvelope == nil {
		return true, nil
	}
	plaintext, err := vaultSecrets.Open(ctx, llmproviders.SecretBinding(existing), *existing.SecretEnvelope)
	if err != nil {
		return false, fmt.Errorf("decrypt registered provider key: %w", err)
	}
	return subtle.ConstantTimeCompare(plaintext, []byte(apiKey)) != 1, nil
}

func equalStrings(first, second []string) bool {
	if len(first) != len(second) {
		return false
	}
	for i := range first {
		if first[i] != second[i] {
			return false
		}
	}
	return true
}

func isModelConflict(err error) bool {
	var conflict *db.LLMProviderModelConflictError
	return errors.As(err, &conflict)
}
