package codesessions

import (
	"context"
	"encoding/json"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

type dshContextSnapshot struct {
	Config   json.RawMessage                      `json:"config"`
	Model    string                               `json:"model"`
	WorkDir  string                               `json:"cwd"`
	Models   map[string]llmproviders.RuntimeModel `json:"models"`
	External bool                                 `json:"external"`
}

type dshChildContext struct {
	MCPConfig       json.RawMessage   `json:"mcp_config"`
	ToolPermissions map[string]string `json:"runtime_tool_permissions,omitempty"`
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
		candidate, err := l.contextCandidate(ctx, record)
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

func (l dshLifecycle) contextCandidate(ctx context.Context, record db.CodeSession) ([]byte, error) {
	var metadata struct {
		Config json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal(record.Metadata, &metadata); err != nil {
		return nil, err
	}
	models, err := l.handler.service.modelCatalog(ctx, record)
	if err != nil {
		return nil, err
	}
	external, err := l.handler.service.selfHostedCodeSession(ctx, record.ExternalID)
	if err != nil {
		return nil, err
	}
	return json.Marshal(dshContextSnapshot{Config: metadata.Config, Model: record.Model, WorkDir: record.WorkDir, Models: models, External: external})
}

func dshChildContexts(record db.CodeSession, snapshot dshContextSnapshot, identity MCPRuntimeIdentity, tunnel config.TunnelConfig) (map[string]dshChildContext, error) {
	var input struct {
		Subagents *RuntimeSubagentConfig `json:"subagents"`
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
		if !snapshot.External {
			item.ToolPermissions = runtimeToolPolicies(permissions.Tools)
		}
		result[agent.Name] = item
	}
	return result, nil
}
