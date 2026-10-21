package devinit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func reconcileEnvironments(ctx context.Context, database *db.DB, template string, state *State) error {
	if template == "" {
		template = config.DefaultE2BTemplate
	}
	for index := range state.Workspaces {
		workspace := &state.Workspaces[index]
		registered := workspace.Environment
		if registered == nil {
			return fmt.Errorf("workspace %q environment identity has not been reserved", workspace.Ref)
		}
		existing, err := database.GetEnvironment(ctx, workspace.UUID, registered.ExternalID)
		if errors.Is(err, db.ErrNotFound) && registered.Pending {
			_, err = database.CreateEnvironment(ctx, db.Environment{
				UUID: registered.UUID, ExternalID: registered.ExternalID,
				OrganizationUUID: state.Organization.UUID, WorkspaceUUID: workspace.UUID,
				Name: "default", Provider: "e2b", ResolvedTemplate: template,
				Config:   json.RawMessage(`{"type":"cloud","packages":{"type":"packages","apt":[],"cargo":[],"gem":[],"go":[],"npm":[],"pip":[]},"networking":{"type":"unrestricted"}}`),
				Metadata: json.RawMessage(`{}`), CreatedAt: time.Now().UTC(),
			})
		} else if err == nil && (existing.UUID != registered.UUID || existing.OrganizationUUID != state.Organization.UUID || existing.ArchivedAt != nil) {
			return fmt.Errorf("workspace %q registered environment is archived or its identity differs", workspace.Ref)
		}
		if err != nil {
			return fmt.Errorf("workspace %q initialize default environment: %w", workspace.Ref, err)
		}
		registered.Pending = false
	}
	return nil
}
