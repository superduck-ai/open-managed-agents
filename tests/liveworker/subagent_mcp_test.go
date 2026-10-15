package liveworker

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"net/http"
	"sync/atomic"
	"testing"
)

func subagentMCPFixture(t *testing.T, calls *atomic.Int32) string {
	t.Helper()
	return serveRealWorkerFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := jsonv2.UnmarshalRead(http.MaxBytesReader(w, r.Body, 1<<20), &request); err != nil {
			http.Error(w, "invalid RPC", 400)
			return
		}
		if len(request.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "child-proof", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "verify", "description": "Verify the child MCP runtime", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}}}}
		case "tools/call":
			calls.Add(1)
			result = map[string]any{"content": []any{map[string]string{"type": "text", "text": "child MCP verified"}}}
		default:
			result = map[string]any{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = jsonv2.MarshalWrite(w, struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Result  any             `json:"result"`
		}{JSONRPC: "2.0", ID: request.ID, Result: result})
	}))
}
