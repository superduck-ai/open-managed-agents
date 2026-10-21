package agents

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMCPNamesEnforceCharacterLengthAndExactReferences(t *testing.T) {
	for _, name := range []string{"", strings.Repeat("测", 256), strings.Repeat("🧪", 256)} {
		servers, _ := json.Marshal([]map[string]string{{"name": name, "type": "url", "url": "https://example.com/mcp"}})
		if _, err := normalizeMCPServers(servers); err == nil {
			t.Fatal("accepted invalid MCP name length")
		}
		tools, _ := json.Marshal([]map[string]string{{"type": "mcp_toolset", "mcp_server_name": name}})
		if _, err := normalizeTools(tools, servers); err == nil {
			t.Fatal("accepted invalid MCP reference length")
		}
	}
	servers := json.RawMessage(`[{"name":"Peer-测试","type":"url","url":"https://example.com/mcp"}]`)
	for _, name := range []string{"peer-测试", "Peer-測試"} {
		tools, _ := json.Marshal([]map[string]string{{"type": "mcp_toolset", "mcp_server_name": name}})
		if _, err := normalizeTools(tools, servers); err == nil {
			t.Fatal("accepted nonmatching Unicode reference")
		}
	}
	for _, name := range []string{"Peer-测试", "peer__name", "unsafe name", strings.Repeat("测", 255), strings.Repeat("🧪", 255)} {
		servers, _ := json.Marshal([]map[string]string{{"name": name, "type": "url", "url": "https://example.com/mcp"}})
		normalized, err := normalizeMCPServers(servers)
		if err != nil {
			t.Fatal(err)
		}
		var decoded []struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(normalized, &decoded); err != nil || decoded[0].Name != name {
			t.Fatalf("name was changed: %s, error: %v", normalized, err)
		}
		tools, _ := json.Marshal([]map[string]string{{"type": "mcp_toolset", "mcp_server_name": name}})
		if _, err := normalizeTools(tools, normalized); err != nil {
			t.Fatal(err)
		}
	}
}
