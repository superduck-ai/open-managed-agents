package devinit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/superduck-ai/yourbatis"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func reconcileEnvironments(ctx context.Context, database *db.DB, template string, state *State, opts Options) error {
	template = localSandboxTemplate(template)
	for index := range state.Workspaces {
		workspace := &state.Workspaces[index]
		registered := workspace.Environment
		if registered == nil {
			return fmt.Errorf("workspace %q environment identity has not been reserved", workspace.Ref)
		}
		existing, err := database.GetEnvironment(ctx, workspace.UUID, registered.ExternalID)
		if errors.Is(err, db.ErrNotFound) && registered.Pending {
			existing, err = database.CreateEnvironment(ctx, db.Environment{
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
		if registered.Template == "" && existing.Provider == "e2b" && (isDefaultSandboxTemplate(existing.ResolvedTemplate) || existing.ResolvedTemplate == template) {
			registered.Template = existing.ResolvedTemplate
		}
		if opts.Update {
			if err := updateRegisteredEnvironmentTemplate(ctx, database, workspace.UUID, registered, template); err != nil {
				return err
			}
		}
		registered.Pending = false
	}
	return nil
}

func updateRegisteredEnvironmentTemplate(ctx context.Context, database *db.DB, workspaceUUID string, registered *EnvironmentState, template string) error {
	return database.EnvironmentTransaction(ctx, func(tx *yourbatis.Tx) error {
		environment, err := database.LockEnvironmentTx(ctx, tx, workspaceUUID, registered.ExternalID)
		if err != nil {
			return err
		}
		if environment.Provider != "e2b" || (!isDefaultSandboxTemplate(environment.ResolvedTemplate) && environment.ResolvedTemplate != registered.Template) || environment.BuildJobID != nil {
			return nil
		}
		environment.ResolvedTemplate = template
		_, _, err = database.UpdateEnvironmentTx(ctx, tx, environment)
		if err == nil {
			registered.Template = template
		}
		return err
	})
}
