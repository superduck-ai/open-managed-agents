package mcpcatalogs

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestProbeClassifiesAuthenticationFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "do not expose this upstream body", http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)

	_, err := (Prober{}).Probe(context.Background(), server.URL)
	var probeErr *ProbeError
	if !errors.As(err, &probeErr) {
		t.Fatalf("Probe error = %v, want ProbeError", err)
	}
	if probeErr.Code != "auth_required" {
		t.Fatalf("ProbeError = %#v, want auth_required", probeErr)
	}
	if probeErr.Message == "do not expose this upstream body" {
		t.Fatal("ProbeError leaked an upstream response body")
	}
}

func TestProbeAllowsPrivateMCPWithoutAddressPolicy(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "weather-service", Version: "1.0.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_forecast",
		Title:       "Get forecast",
		Description: "Returns a weather forecast.",
	}, emptyToolHandler)
	httpServer := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, nil))
	t.Cleanup(httpServer.Close)

	result, err := (Prober{}).Probe(context.Background(), httpServer.URL)
	if err != nil {
		t.Fatalf("Probe returned error: %v", err)
	}
	if len(result.Tools) != 1 {
		t.Fatalf("Probe returned %d tools, want 1", len(result.Tools))
	}
	if got := result.Tools[0]; got.Name != "get_forecast" || got.Title != "Get forecast" || got.Description != "Returns a weather forecast." {
		t.Fatalf("Probe tool = %#v", got)
	}
}

func TestLiveMCPProbe(t *testing.T) {
	endpoint := os.Getenv("TEST_MCP_DISCOVERY_URL")
	if endpoint == "" {
		t.Skip("TEST_MCP_DISCOVERY_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := (Prober{}).Probe(ctx, endpoint)
	if err != nil {
		t.Fatalf("Probe returned error: %v", err)
	}
	if len(result.Tools) == 0 {
		t.Fatal("Probe returned no tools")
	}
}

func emptyToolHandler(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
	return nil, nil, nil
}
