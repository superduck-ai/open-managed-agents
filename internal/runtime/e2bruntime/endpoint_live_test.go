package e2bruntime

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestLiveLocalSandboxServiceEndpoint(t *testing.T) {
	sandboxID := os.Getenv("OMA_LIVE_ENDPOINT_SANDBOX_ID")
	if sandboxID == "" {
		t.Skip("requires an owned local E2B sandbox with MCP on port 3001")
	}
	provider := NewProvider(config.E2BConfig{
		APIURL: os.Getenv("OMA_LIVE_ENDPOINT_API_URL"), APIKey: os.Getenv("OMA_LIVE_ENDPOINT_API_KEY"),
		LocalPortLookup: true, LocalServiceHost: "127.0.0.1", RequestTimeout: 10 * time.Second,
	})
	endpoint, err := provider.ServiceEndpoint(t.Context(), sandboxID, 3001, "/mcp")
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, endpoint, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"oma-endpoint-test","version":"1"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal("resolved MCP endpoint is unreachable")
	}
	defer response.Body.Close()
	var payload struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("MCP initialize status=%d", response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil || payload.Result.ProtocolVersion == "" {
		t.Fatal("MCP initialize did not return a protocol version")
	}
}
