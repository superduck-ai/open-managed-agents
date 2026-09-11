package tunnels

import (
	"context"
	"net/http"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestProbeTargetRecognizesOnlyConfiguredTunnelURLs(t *testing.T) {
	t.Parallel()
	service := &Service{cfg: config.TunnelConfig{
		PublicBaseURL: "https://mcp.example.com",
		DomainSuffix:  "tunnel.example.com",
	}}
	scope := ConsoleScope{OrganizationUUID: "org", WorkspaceUUID: "workspace"}

	if _, recognized, err := service.ProbeTarget(context.Background(), scope, "https://remote.example.com/mcp"); recognized || err != nil {
		t.Fatalf("ordinary MCP target = (recognized=%v, err=%v), want false/nil", recognized, err)
	}
	_, recognized, err := service.ProbeTarget(
		context.Background(),
		scope,
		"https://mcp.example.com/v1/mcp/not-a-tunnel/secondary",
	)
	if !recognized || err == nil {
		t.Fatalf("malformed Tunnel target = (recognized=%v, err=%v), want true/error", recognized, err)
	}
}

func TestDecodeTunnelProbeResponseRejectsInvalidResults(t *testing.T) {
	t.Parallel()
	for _, response := range []TunnelResponse{
		{},
		{ResponseCode: http.StatusOK, JSONResponse: []byte(`not-json`)},
		{ResponseCode: http.StatusOK, JSONResponse: []byte(`{"jsonrpc":"2.0","id":"other","result":{}}`)},
		{ResponseCode: http.StatusOK, JSONResponse: []byte(`{"jsonrpc":"2.0","id":"probe","error":{"code":-32603}}`)},
	} {
		var decoded tunnelProbeRPCResponse
		if err := decodeTunnelProbeResponse(response, "probe", &decoded); err == nil {
			t.Fatalf("accepted invalid probe response: %s", response.JSONResponse)
		}
	}
}

func TestDecodeTunnelProbeResponse(t *testing.T) {
	t.Parallel()
	response := TunnelResponse{JSONResponse: []byte(`{"jsonrpc":"2.0","id":"probe","result":{"protocolVersion":"2025-06-18","serverInfo":{"name":"stub","version":"1"},"tools":[{"name":"echo"}]},"error":null}`)}
	var decoded tunnelProbeRPCResponse
	if err := decodeTunnelProbeResponse(response, "probe", &decoded); err != nil {
		t.Fatalf("decode valid probe response: %v", err)
	}
	if decoded.Result.ServerInfo.Name != "stub" || len(decoded.Result.Tools) != 1 || decoded.Result.Tools[0].Name != "echo" {
		t.Fatalf("decoded probe response = %+v", decoded)
	}
}
