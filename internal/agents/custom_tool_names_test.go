package agents

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCustomToolsRejectReservedMCPPrefix(t *testing.T) {
	for _, name := range []string{"mcp__", "mcp__reserved", "mcp__peer__read"} {
		tools, _ := json.Marshal([]map[string]any{{"type": "custom", "name": name, "description": "owned validation test", "input_schema": map[string]any{"type": "object"}}})
		if _, err := normalizeTools(tools, json.RawMessage(`[]`)); err == nil || !strings.Contains(err.Error(), "mcp__") {
			t.Fatalf("reserved custom tool %q error = %v", name, err)
		}
	}
}

func TestCustomToolsPermitOrdinaryNamesContainingMCPLetters(t *testing.T) {
	for _, name := range []string{"mcp_weather", "weather_mcp__", "get_weather"} {
		tools, _ := json.Marshal([]map[string]any{{"type": "custom", "name": name, "description": "owned validation test", "input_schema": map[string]any{"type": "object"}}})
		if _, err := normalizeTools(tools, json.RawMessage(`[]`)); err != nil {
			t.Fatalf("ordinary custom tool %q error = %v", name, err)
		}
	}
}
