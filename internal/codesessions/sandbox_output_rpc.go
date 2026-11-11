package codesessions

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"uuid"
)

type sandboxOutputResult struct {
	ResultType string `json:"resultType"`
	IsError    bool   `json:"isError"`
	Content    []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func callSandboxOutput(ctx context.Context, endpoint, command string) (*sandboxOutputResult, error) {
	id := uuid.NewV4().String()
	payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": map[string]any{"name": "bash", "arguments": map[string]string{"command": command}, "_meta": map[string]any{"io.modelcontextprotocol/protocolVersion": "2026-07-28", "io.modelcontextprotocol/clientCapabilities": map[string]any{}}}})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return nil, fmt.Errorf("invalid sandbox output endpoint")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	request.Header.Set("Mcp-Method", "tools/call")
	request.Header.Set("Mcp-Name", "bash")
	response, _, err := sendSandboxMCPRequest(request)
	if err != nil {
		return nil, fmt.Errorf("sandbox output request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sandbox output returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (128<<10)+1))
	if err != nil || len(body) > 128<<10 {
		return nil, fmt.Errorf("invalid sandbox output response size")
	}
	if strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		body = sandboxOutputEvent(body, id)
	}
	var rpc struct {
		JSONRPC string               `json:"jsonrpc"`
		ID      string               `json:"id"`
		Result  *sandboxOutputResult `json:"result"`
		Error   json.RawMessage      `json:"error"`
	}
	if json.Unmarshal(body, &rpc) != nil || rpc.JSONRPC != "2.0" || rpc.ID != id || rpc.Result == nil || len(rpc.Error) > 0 || rpc.Result.ResultType != "complete" {
		return nil, fmt.Errorf("invalid sandbox output completion")
	}
	return rpc.Result, nil
}

func sandboxOutputEvent(body []byte, id string) []byte {
	var result []byte
	for _, frame := range strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n\n") {
		var lines []string
		for _, line := range strings.Split(frame, "\n") {
			if data, ok := strings.CutPrefix(line, "data:"); ok {
				lines = append(lines, strings.TrimPrefix(data, " "))
			}
		}
		data := []byte(strings.Join(lines, "\n"))
		var event struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(data, &event) == nil && event.ID == id {
			if result != nil {
				return nil
			}
			result = data
		}
	}
	return result
}
