package codesessions

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"github.com/superduck-ai/open-managed-agents/internal/vaults"
	"net/url"
	"strings"
	"time"
)

type OAuthRequest struct {
	Kind          string `json:"kind"`
	Provider      string `json:"provider,omitempty"`
	ProviderName  string `json:"provider_name,omitempty"`
	SecretName    string `json:"secret_name,omitempty"`
	MCPServerName string `json:"mcp_server_name,omitempty"`
	MCPServerURL  string `json:"mcp_server_url,omitempty"`
	ParentCallID  string `json:"parent_call_id"`
	Reason        string `json:"reason"`
	InvocationID  string `json:"invocation_id,omitempty"`
}

func (r OAuthRequest) Validate() error {
	if r.ParentCallID == "" || len(r.ParentCallID) > 256 {
		return errors.New("OAuth request requires a tool call")
	}
	if len(r.InvocationID) > 128 || len(r.ProviderName) > 256 {
		return errors.New("invalid OAuth invocation")
	}
	if r.Reason != "missing" && r.Reason != "reauthorization_required" {
		return errors.New("invalid OAuth request reason")
	}
	switch r.Kind {
	case "env_oauth":
		_, _, providerErr := vaults.ParseEnvOAuthProviderKey(r.Provider)
		_, nameErr := vaults.ParseEnvOAuthSecretName(r.SecretName)
		if providerErr != nil || nameErr != nil || len(r.Provider) > 256 || r.MCPServerName != "" || r.MCPServerURL != "" {
			return errors.New("OAuth environment request requires a provider and variable")
		}
	case "mcp_oauth":
		target, err := url.Parse(r.MCPServerURL)
		if err != nil || len(r.MCPServerURL) > 4096 || len(r.MCPServerName) > 256 || target.Hostname() == "" || target.Scheme != "https" || target.User != nil || target.Fragment != "" || r.MCPServerName == "" || r.Provider != "" || r.SecretName != "" {
			return errors.New("OAuth MCP request requires a configured HTTPS server")
		}
	default:
		return errors.New("invalid OAuth credential kind")
	}
	return nil
}

func (s *Service) handleOAuthRequest(ctx context.Context, codeSessionID string, workerEpoch int64, payload *workerControlRequestPayload, meta EventMetadata) error {
	data, err := jsonv2.Marshal(payload.Request.Input)
	if err != nil {
		return err
	}
	var input OAuthRequest
	if err := jsonv2.Unmarshal(data, &input, jsonv2.RejectUnknownMembers(true)); err != nil {
		return err
	}
	if err := input.Validate(); err != nil {
		return err
	}
	if payload.Request.ToolUseID == "" || !strings.HasPrefix(payload.Request.ToolUseID, input.ParentCallID+":oauth:") {
		return errors.New("OAuth request does not match its tool call")
	}
	request := toolPermissionRequestFromWorkerEvent(payload, meta)
	request.ToolName = "RequestOAuth"
	request.RequestedAt = time.Now().UTC()
	request, raw, err := toolCallPublicPayload(codeSessionID, request, toolIdentity{Kind: "oauth"}, resolvedToolPermissionAllow, request.RequestedAt)
	if err != nil {
		return err
	}
	if err := s.persistToolPermissionRequest(ctx, codeSessionID, workerEpoch, request); err != nil {
		return err
	}
	pending, err := toolRequiresActionPayload(codeSessionID, request, request.RequestedAt)
	if err != nil {
		return err
	}
	return s.publishWorkerPublicPayloads(ctx, codeSessionID, []json.RawMessage{raw, pending})
}
