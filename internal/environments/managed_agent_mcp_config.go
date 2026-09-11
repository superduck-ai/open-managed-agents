package environments

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

const managedAgentMCPConfigPath = "/tmp/managed-agent-mcp-config.json"

type managedAgentMCPConfigFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Mode    int    `json:"mode"`
}

// These fields belong to the environment-manager startup JSON boundary.
// RawMessage retains unknown configuration and exact JSON numbers.
type managedAgentMCPStartupFields struct {
	MCPConfig      json.RawMessage            `json:"mcp_config"`
	MCPConfigFile  managedAgentMCPConfigFile  `json:"mcp_config_file"`
	ClaudeCodeArgs map[string]json.RawMessage `json:"claude_code_args"`
}

// managedAgentMCPConfigFields encodes the MCP document and its launch fields.
// It only handles JSON serialization; it never issues credentials or starts a process.
func managedAgentMCPConfigFields(document any, args map[string]json.RawMessage) (managedAgentMCPStartupFields, error) {
	content, err := json.Marshal(document)
	if err != nil {
		return managedAgentMCPStartupFields{}, fmt.Errorf("encode managed-agent MCP config: %w", err)
	}
	if args == nil {
		args = make(map[string]json.RawMessage)
	}
	args["mcp-config"], _ = json.Marshal(managedAgentMCPConfigPath)
	return managedAgentMCPStartupFields{
		MCPConfig:      content,
		MCPConfigFile:  managedAgentMCPConfigFile{Path: managedAgentMCPConfigPath, Content: base64.StdEncoding.EncodeToString(content), Mode: 0o600},
		ClaudeCodeArgs: args,
	}, nil
}

// buildManagedAgentRuntimeMCPConfig constructs the client document only after
// the current Code Session identity is available. Source declarations stay intact.
func buildManagedAgentRuntimeMCPConfig(sessionConfig json.RawMessage, codeSessionID, sessionIngressToken string, cfg config.Config) (json.RawMessage, error) {
	document, err := codesessions.BuildMCPRuntimeConfig(sessionConfig, codesessions.MCPRuntimeIdentity{
		CodeSessionID: codeSessionID, SessionIngressToken: sessionIngressToken,
		APIBaseURL: codeSessionSandboxAPIBaseURL(cfg),
	}, cfg.Tunnel)
	if err != nil {
		return nil, err
	}
	if len(document) == 0 {
		return sessionConfig, nil
	}
	var input struct {
		ClaudeCodeArgs map[string]json.RawMessage `json:"claude_code_args"`
	}
	if err := json.Unmarshal(sessionConfig, &input); err != nil {
		return nil, err
	}
	fields, err := managedAgentMCPConfigFields(document, input.ClaudeCodeArgs)
	if err != nil {
		return nil, err
	}
	return mergeManagedAgentMCPFields(sessionConfig, fields)
}

// mergeManagedAgentMCPFields overlays known fields at a JSON boundary, retaining
// unrelated fields as opaque JSON rather than decoding them through float64.
func mergeManagedAgentMCPFields(original json.RawMessage, patch any) (json.RawMessage, error) {
	fields := make(map[string]json.RawMessage)
	if err := json.Unmarshal(original, &fields); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(patch)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return nil, err
	}
	return json.Marshal(fields)
}
