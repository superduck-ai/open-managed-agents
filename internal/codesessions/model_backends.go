package codesessions

import (
	"context"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

func (s *Service) modelBackends(ctx context.Context, session db.CodeSession) (map[string]string, map[string]int, map[string]db.ModelOptions, error) {
	catalog, err := llmproviders.LoadCatalog(ctx, s.db, session.OrganizationUUID, session.WorkspaceUUID)
	if err != nil {
		return nil, nil, nil, err
	}
	backends, err := catalog.Backends()
	return backends, catalog.ContextWindows(), catalog.ModelOptions(), err
}
