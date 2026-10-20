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
	backends := make(map[string]string)
	for _, provider := range providers {
		if !llmproviders.ValidBackend(provider.APIBackend) {
			return nil, llmproviders.ErrBackendMismatch
		}
		for _, model := range provider.ModelIDs {
			if _, exists := backends[model]; exists {
				return nil, llmproviders.ErrAmbiguousModel
			}
			backends[model] = provider.APIBackend
		}
	}
	return backends, nil
}
