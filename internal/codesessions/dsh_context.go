package codesessions

import (
	"context"
	"encoding/json"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"
)

type dshContextSnapshot struct {
	Config   json.RawMessage                      `json:"config"`
	Model    string                               `json:"model"`
	WorkDir  string                               `json:"cwd"`
	Models   map[string]llmproviders.RuntimeModel `json:"models"`
	External bool                                 `json:"external"`
}

type dshChildContext struct {
	RuntimeSkills       []runtimeSkillConfig `json:"runtime_skills"`
	SkillCatalogVersion int                  `json:"skill_catalog_version,omitempty"`
	MCPConfig           json.RawMessage      `json:"mcp_config"`
	ToolPermissions     map[string]string    `json:"runtime_tool_permissions,omitempty"`
}

func (l dshLifecycle) RegistrationContext(ctx context.Context, scope db.DSHScope, token string) (json.RawMessage, error) {
	h := l.handler
	record, err := h.requireCodeSession(ctx, scope.CodeSessionID)
	if err != nil {
		return nil, err
	}
	raw, err := h.db.GetDSHContextSnapshot(ctx, scope)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		candidate, err := h.service.dshContextCandidate(ctx, record)
		if err != nil {
			return nil, err
		}
		raw, err = h.db.FreezeDSHContext(ctx, scope, candidate)
		if err != nil {
			return nil, err
		}
	}
	return renderDSHContext(record, raw, token, h.cfg)
}

func renderDSHContext(record db.CodeSession, raw []byte, token string, cfg config.Config) (json.RawMessage, error) {
	var err error
	var snapshot dshContextSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, err
	}
	record.WorkDir = snapshot.WorkDir
	record.Model = snapshot.Model
	record.Metadata, err = json.Marshal(struct {
		Config json.RawMessage `json:"config"`
	}{snapshot.Config})
	if err != nil {
		return nil, err
	}
	identity := MCPRuntimeIdentity{Protocol: "dsr1", CodeSessionID: record.ExternalID, SessionIngressToken: token, APIBaseURL: cfg.CodeSession.SandboxAPIBaseURL}
	value, err := sessionContextFromCodeSession(record, identity, cfg)
	if err != nil {
		return nil, err
	}
	if snapshot.External {
		delete(value, "runtime_tool_permissions")
	}
	children, err := dshChildContexts(record, snapshot, identity, cfg.Tunnel)
	if err != nil {
		return nil, err
	}
	value["models"] = snapshot.Models
	value["subagent_contexts"] = children
	return json.Marshal(value)
}

func (s *Service) dshContextCandidate(ctx context.Context, record db.CodeSession) ([]byte, error) {
	var metadata struct {
		Config json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal(record.Metadata, &metadata); err != nil {
		return nil, err
	}
	models, err := s.modelCatalog(ctx, record)
	if err != nil {
		return nil, err
	}
	external, err := s.selfHostedCodeSession(ctx, record.ExternalID)
	if err != nil {
		return nil, err
	}
	return json.Marshal(dshContextSnapshot{Config: metadata.Config, Model: record.Model, WorkDir: record.WorkDir, Models: models, External: external})
}

func dshChildContexts(record db.CodeSession, snapshot dshContextSnapshot, identity MCPRuntimeIdentity, tunnel config.TunnelConfig) (map[string]dshChildContext, error) {
	var input struct {
		Subagents           *RuntimeSubagentConfig                  `json:"subagents"`
		SkillCatalogVersion int                                     `json:"skill_catalog_version"`
		AgentSkills         map[string]skillsapi.RuntimeAgentSkills `json:"agent_skills"`
	}
	if err := json.Unmarshal(snapshot.Config, &input); err != nil {
		return nil, err
	}
	result := make(map[string]dshChildContext)
	if input.Subagents == nil {
		return result, nil
	}
	for _, agent := range input.Subagents.Agents {
		child, err := codeSessionWithToolSnapshot(record, agent.AgentSnapshot)
		if err != nil {
			return nil, err
		}
		var metadata struct {
			Config json.RawMessage `json:"config"`
		}
		if err := json.Unmarshal(child.Metadata, &metadata); err != nil {
			return nil, err
		}
		mcp, err := BuildMCPRuntimeConfig(metadata.Config, identity, tunnel)
		if err != nil {
			return nil, err
		}
		if len(mcp) == 0 {
			mcp = json.RawMessage(`{}`)
		}
		var permissions toolPermissionSnapshot
		if err := json.Unmarshal(metadata.Config, &permissions); err != nil {
			return nil, err
		}
		item := dshChildContext{MCPConfig: mcp}
		if input.SkillCatalogVersion != 0 {
			catalog, ok := input.AgentSkills[agent.Name]
			var authority struct {
				ID      string `json:"id"`
				Version int    `json:"version"`
			}
			if err := json.Unmarshal(agent.AgentSnapshot, &authority); err != nil {
				return nil, err
			}
			if input.SkillCatalogVersion != 1 || !ok || catalog.AgentID != authority.ID || catalog.AgentVersion != authority.Version || catalog.Skills == nil {
				return nil, errRuntimeSkillCatalog
			}
			item.RuntimeSkills = catalog.Skills
			item.SkillCatalogVersion = 1
		}
		if !snapshot.External {
			item.ToolPermissions = runtimeToolPolicies(permissions.Tools)
		}
		result[agent.Name] = item
	}
	return result, nil
}
