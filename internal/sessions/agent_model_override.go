package sessions

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/agents"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

type sessionAgentReference struct {
	Type       string          `json:"type"`
	ID         string          `json:"id"`
	Version    *int            `json:"version"`
	Model      json.RawMessage `json:"model"`
	System     json.RawMessage `json:"system"`
	Tools      json.RawMessage `json:"tools"`
	MCPServers json.RawMessage `json:"mcp_servers"`
	Skills     json.RawMessage `json:"skills"`
}

func (a sessionAgentReference) validateType() error {
	switch a.Type {
	case "", "agent", "agent_with_overrides":
		return nil
	default:
		return errInvalidSessionAgentType
	}
}

func (h *Handler) applySessionModelOverride(r *http.Request, principal auth.Principal, agent db.Agent, raw json.RawMessage) (db.Agent, error) {
	if len(raw) == 0 {
		return agent, nil
	}
	model, err := agents.NormalizeSessionModelOverride(raw, func(modelID string) (string, error) {
		canonicalID, err := llmproviders.CanonicalModelID(
			r.Context(),
			h.db,
			principal.OrganizationUUID,
			principal.WorkspaceUUID,
			modelID,
		)
		switch {
		case errors.Is(err, llmproviders.ErrModelNotConfigured):
			return "", errSessionOverrideModelNotConfigured
		case errors.Is(err, llmproviders.ErrAmbiguousModel):
			return "", err
		case err != nil:
			return "", errSessionOverrideModelsUnavailable
		}
		return canonicalID, nil
	})
	if err != nil {
		return db.Agent{}, err
	}
	agent.Model = model
	return agent, nil
}
