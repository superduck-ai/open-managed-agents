package codesessions

import (
	"context"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

func (s *Service) modelBackends(ctx context.Context, session db.CodeSession) (map[string]string, error) {
	providers, err := s.db.ListLLMProviders(ctx, session.OrganizationUUID, session.WorkspaceUUID)
	if err != nil {
		return nil, err
	}
	return modelBackendsForProviders(providers)
}

func modelBackendsForProviders(providers []db.LLMProvider) (map[string]string, error) {
	catalog := llmproviders.NewCatalog(providers)
	backends := make(map[string]string)
	for _, provider := range providers {
		if !llmproviders.ValidBackend(provider.APIBackend) {
			return nil, llmproviders.ErrBackendMismatch
		}
		for _, modelID := range provider.ModelIDs {
			backends[llmproviders.QualifiedModelID(provider.Slug, modelID)] = provider.APIBackend
		}
	}
	for _, provider := range providers {
		for _, modelID := range provider.ModelIDs {
			if _, exists := backends[modelID]; exists {
				continue
			}
			selected, err := catalog.Lookup(modelID)
			if err == nil && selected.UpstreamModelID == modelID {
				backends[modelID] = selected.Provider.APIBackend
			}
		}
	}
	return backends, nil
}
