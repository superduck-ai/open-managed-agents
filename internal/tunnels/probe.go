package tunnels

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
)

const (
	tunnelProbeTimeout        = 30 * time.Second
	tunnelProbeCleanupTimeout = 5 * time.Second
)

type TunnelProbeTool struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

type TunnelProbeResult struct {
	Status          string            `json:"status"`
	Channel         string            `json:"channel"`
	ProtocolVersion string            `json:"protocol_version,omitempty"`
	ServerName      string            `json:"server_name,omitempty"`
	ServerVersion   string            `json:"server_version,omitempty"`
	Tools           []TunnelProbeTool `json:"tools"`
}

type tunnelProbeRPCResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      string `json:"id"`
	Result  struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
		Tools []TunnelProbeTool `json:"tools"`
	} `json:"result"`
	Error json.RawMessage `json:"error"`
}

type ResolvedProbeTarget struct {
	TunnelID string
	Channel  string
}

// ResolveProbeTarget recognizes a Tunnel-owned endpoint and verifies that the
// referenced Tunnel belongs to the authenticated organization/workspace.
func (s *Service) ResolveProbeTarget(
	ctx context.Context,
	scope ConsoleScope,
	rawTarget string,
) (ResolvedProbeTarget, bool, error) {
	target, err := url.Parse(rawTarget)
	if err != nil {
		return ResolvedProbeTarget{}, false, nil
	}
	reference, recognized, err := RecognizeTarget(target, s.cfg)
	if !recognized {
		return ResolvedProbeTarget{}, false, nil
	}
	if err != nil {
		return ResolvedProbeTarget{}, true, invalidRequest(err)
	}
	tunnelID := reference.TunnelID
	if reference.Domain != "" {
		tunnel, lookupErr := s.db.GetMCPTunnelByDomain(
			ctx,
			scope.OrganizationUUID,
			scope.WorkspaceUUID,
			reference.Domain,
		)
		if lookupErr != nil {
			return ResolvedProbeTarget{}, true, mapTunnelLookupError(lookupErr, reference.Domain, "retrieve")
		}
		tunnelID = tunnel.ExternalID
	} else {
		if _, lookupErr := s.Get(ctx, tunnelScope{
			OrganizationUUID: scope.OrganizationUUID,
			WorkspaceUUID:    scope.WorkspaceUUID,
		}, tunnelID); lookupErr != nil {
			return ResolvedProbeTarget{}, true, lookupErr
		}
	}
	return ResolvedProbeTarget{TunnelID: tunnelID, Channel: reference.Channel}, true, nil
}

// ProbeTarget probes a configured Tunnel URL through the in-process Broker path.
// The recognized result distinguishes ordinary remote MCP endpoints from malformed
// or unavailable Tunnel targets so callers never fall back to anonymous HTTP for a
// URL owned by this Tunnel data plane.
func (s *Service) ProbeTarget(
	ctx context.Context,
	scope ConsoleScope,
	rawTarget string,
) (TunnelProbeResult, bool, error) {
	target, recognized, err := s.ResolveProbeTarget(ctx, scope, rawTarget)
	if !recognized {
		return TunnelProbeResult{}, false, nil
	}
	if err != nil {
		return TunnelProbeResult{}, true, err
	}
	result, err := s.Probe(ctx, tunnelScope{
		OrganizationUUID: scope.OrganizationUUID,
		WorkspaceUUID:    scope.WorkspaceUUID,
	}, target.TunnelID, target.Channel)
	return result, true, err
}

func (s *Service) Probe(ctx context.Context, scope tunnelScope, tunnelID, channel string) (TunnelProbeResult, error) {
	if s.broker == nil {
		return TunnelProbeResult{}, unavailable("Tunnel broker is unavailable", errors.New("tunnel broker is not configured"))
	}
	if channel == "" {
		channel = "main"
	}
	if !channelNamePattern.MatchString(channel) {
		return TunnelProbeResult{}, invalidRequest(errors.New("channel is invalid"))
	}
	tunnel, err := s.Get(ctx, scope, tunnelID)
	if err != nil {
		return TunnelProbeResult{}, err
	}
	if tunnel.ArchivedAt != nil {
		return TunnelProbeResult{}, archivedTunnel(tunnelID)
	}
	timeout := s.cfg.RequestTimeout
	if timeout <= 0 || timeout > tunnelProbeTimeout {
		timeout = tunnelProbeTimeout
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	initializeBody := json.RawMessage(`{"jsonrpc":"2.0","id":"oma-probe-initialize","method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"oma-tunnel-probe","version":"1.0.0"}}}`)
	initialize, err := s.executeProbeCommand(probeCtx, tunnel, channel, "", CommandTypeJSONRPC, initializeBody)
	if err != nil {
		return TunnelProbeResult{}, err
	}
	var initializeRPC tunnelProbeRPCResponse
	if err := decodeTunnelProbeResponse(initialize, "oma-probe-initialize", &initializeRPC); err != nil {
		return TunnelProbeResult{}, unavailable("Tunnel MCP initialize failed", err)
	}
	sessionID := initialize.ResponseHeaders.Get("Mcp-Session-Id")
	if sessionID != "" {
		defer s.terminateProbeSession(tunnel, channel, sessionID)
	}

	initializedBody := json.RawMessage(`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`)
	if _, err := s.executeProbeCommand(probeCtx, tunnel, channel, sessionID, CommandTypeJSONRPC, initializedBody); err != nil {
		return TunnelProbeResult{}, err
	}
	toolsBody := json.RawMessage(`{"jsonrpc":"2.0","id":"oma-probe-tools","method":"tools/list","params":{}}`)
	toolsResponse, err := s.executeProbeCommand(probeCtx, tunnel, channel, sessionID, CommandTypeJSONRPC, toolsBody)
	if err != nil {
		return TunnelProbeResult{}, err
	}
	var toolsRPC tunnelProbeRPCResponse
	if err := decodeTunnelProbeResponse(toolsResponse, "oma-probe-tools", &toolsRPC); err != nil {
		return TunnelProbeResult{}, unavailable("Tunnel MCP tools/list failed", err)
	}
	tools := toolsRPC.Result.Tools
	if tools == nil {
		tools = []TunnelProbeTool{}
	}
	return TunnelProbeResult{
		Status:          "ok",
		Channel:         channel,
		ProtocolVersion: initializeRPC.Result.ProtocolVersion,
		ServerName:      initializeRPC.Result.ServerInfo.Name,
		ServerVersion:   initializeRPC.Result.ServerInfo.Version,
		Tools:           tools,
	}, nil
}

func (s *Service) executeProbeCommand(
	ctx context.Context,
	tunnel db.MCPTunnel,
	channel string,
	sessionID string,
	commandType CommandType,
	body json.RawMessage,
) (TunnelResponse, error) {
	requestID, err := ids.New("req_")
	if err != nil {
		return TunnelResponse{}, internalError("Could not generate tunnel probe request ID", err)
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().UTC().Add(tunnelProbeTimeout)
	}
	headers := http.Header{"Accept": []string{"application/json, text/event-stream"}}
	if len(body) > 0 {
		headers.Set("Content-Type", "application/json")
	}
	if sessionID != "" {
		headers.Set("Mcp-Session-Id", sessionID)
	}
	command := queuedCommand{
		RequestID: requestID, CommandType: commandType, Channel: channel,
		CreatedAt: time.Now().UTC(), Headers: headers, JSONRPC: body,
		ExpiresAt: deadline, PayloadSize: int64(len(body)), AffinityKey: sessionID,
	}
	waiter, err := s.broker.subscribeResponse(ctx, tunnel.UUID, requestID, true)
	if err != nil {
		return TunnelResponse{}, ingressQueueError(err)
	}
	defer waiter.Close()
	if err := s.broker.Enqueue(ctx, tunnel.UUID, command); err != nil {
		return TunnelResponse{}, ingressQueueError(err)
	}
	response, err := waiter.Wait(ctx, nil)
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tunnelProbeCleanupTimeout)
		defer cancel()
		_ = s.broker.Cancel(cleanupCtx, tunnel.UUID, requestID)
		return TunnelResponse{}, ingressResponseError(err)
	}
	if response.ResponseCode < 200 || response.ResponseCode >= 300 {
		return TunnelResponse{}, unavailable("Tunnel MCP probe returned an error", errors.New("non-success MCP response"))
	}
	return response, nil
}

func decodeTunnelProbeResponse(response TunnelResponse, expectedID string, destination *tunnelProbeRPCResponse) error {
	if len(response.JSONResponse) == 0 {
		return errors.New("MCP response body is empty")
	}
	if err := json.Unmarshal(response.JSONResponse, destination); err != nil {
		return errors.New("MCP response body is invalid")
	}
	probeError := strings.TrimSpace(string(destination.Error))
	if destination.JSONRPC != "2.0" || destination.ID != expectedID || (probeError != "" && probeError != "null") {
		return errors.New("MCP response does not match the probe request")
	}
	return nil
}

func (s *Service) terminateProbeSession(tunnel db.MCPTunnel, channel, sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _ = s.executeProbeCommand(ctx, tunnel, channel, sessionID, CommandTypeSessionTermination, nil)
}
