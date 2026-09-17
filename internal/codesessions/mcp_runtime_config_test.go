package codesessions

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const runtimeTestTunnelURL = "https://oma.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef"

func TestMCPRuntimeConfigRejectsInvalidDeclarations(t *testing.T) {
	for _, source := range []string{
		`{"mcp_servers":[{"name":"bad","type":"url","url":"https://oma.example/v1/mcp/invalid/sse"}]}`,
		`{"mcp_servers":[{"name":"bad","type":"url","url":"https://oma.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef?x=1"}]}`,
		`{"mcp_servers":[{"name":"bad","url":"https://oma.example/v1/mcp/%zz"}]}`,
		`{"mcp_servers":[{"url":"https://docs.example/mcp"}]}`,
		`{"mcp_servers":[{"name":" bad ","url":"https://docs.example/mcp"}]}`,
		`{"mcp_servers":[{"name":"bad"}]}`,
		`{"mcp_servers":[{"name":"same","url":"https://docs.example/one"},{"name":"same","url":"https://docs.example/two"}]}`,
		`{"mcp_config":{"mcpServers":{"bad":null}}}`,
	} {
		if _, err := BuildMCPRuntimeConfig(json.RawMessage(source), runtimeMCPTestIdentity(), runtimeMCPTestConfig()); err == nil {
			t.Fatalf("invalid declaration accepted: %s", source)
		}
	}
}

func TestMCPRuntimeConfigRequiresIdentityOnlyForTunnel(t *testing.T) {
	source := runtimeMCPTestSource("sse")
	for _, test := range []struct {
		identity MCPRuntimeIdentity
		want     error
	}{
		{MCPRuntimeIdentity{}, ErrMCPGatewayMissing},
		{MCPRuntimeIdentity{APIBaseURL: "https://gateway.example"}, ErrMCPRuntimeIdentityMissing},
		{MCPRuntimeIdentity{APIBaseURL: "https://gateway.example", CodeSessionID: "cse_test"}, ErrMCPRuntimeIdentityMissing},
	} {
		if _, err := BuildMCPRuntimeConfig(source, test.identity, runtimeMCPTestConfig()); !errors.Is(err, test.want) {
			t.Fatalf("error=%v want=%v", err, test.want)
		}
	}
	ordinary := json.RawMessage(`{"mcp_servers":[{"name":"remote","type":"url","url":"https://docs.example/sse"}]}`)
	if _, err := BuildMCPRuntimeConfig(ordinary, MCPRuntimeIdentity{}, runtimeMCPTestConfig()); err != nil {
		t.Fatal(err)
	}
}

func TestMCPRuntimeConfigSeparatesTunnelAndRemoteTransports(t *testing.T) {
	for _, channel := range []string{"main", "sse", "stdio"} {
		t.Run(channel, func(t *testing.T) {
			source := runtimeMCPTestSource(channel)
			original := append(json.RawMessage(nil), source...)
			raw, err := BuildMCPRuntimeConfig(source, runtimeMCPTestIdentity(), runtimeMCPTestConfig())
			if err != nil {
				t.Fatal(err)
			}
			servers := decodeRuntimeMCPTestDocument(t, raw)
			tunnel := servers["local"]
			if tunnel.Type != "http" || tunnel.URL != "https://gateway.example/v2/ccr-sessions/cse_test/mcp/local" || tunnel.Headers["Authorization"] != "Bearer current-token" {
				t.Fatalf("Tunnel config=%+v", tunnel)
			}
			remote := servers["remote"]
			if remote.Type != "sse" || remote.URL != "https://docs.example/sse" || len(remote.Headers) != 0 {
				t.Fatalf("remote config=%+v", remote)
			}
			if !bytes.Equal(source, original) || bytes.Contains(source, []byte("current-token")) {
				t.Fatal("source modified")
			}
		})
	}
}

func TestMCPRuntimeConfigClassifiesBeforeBuilding(t *testing.T) {
	for _, test := range []struct {
		url    string
		tunnel bool
	}{
		{runtimeTestTunnelURL + "/sse", true},
		{"https://abc.tunnel.example/sse", true},
		{"https://third-party.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef/sse", false},
	} {
		raw, _ := json.Marshal(mcpServerDeclaration{URL: test.url})
		server, err := parseMCPRuntimeServer("test", raw, nil, runtimeMCPTestConfig())
		if err != nil {
			t.Fatal(err)
		}
		_, isTunnel := server.(tunnelMCPServer)
		if isTunnel != test.tunnel {
			t.Fatalf("%s classified as %T", test.url, server)
		}
	}
}

func TestSessionContextBuildsMCPFromSourceWithCurrentIdentity(t *testing.T) {
	source := runtimeMCPTestSource("sse")
	metadata, err := managedAgentCodeSessionMetadata(ManagedAgentCreateInput{Config: source})
	if err != nil {
		t.Fatal(err)
	}
	record := db.CodeSession{ExternalID: "cse_test", Metadata: metadata}
	for _, token := range []string{"first-epoch-token", "recovered-epoch-token"} {
		identity := runtimeMCPTestIdentity()
		identity.SessionIngressToken = token
		context, err := sessionContextFromCodeSession(record, identity, runtimeMCPTestConfig())
		if err != nil {
			t.Fatal(err)
		}
		direct, err := BuildMCPRuntimeConfig(source, identity, runtimeMCPTestConfig())
		if err != nil {
			t.Fatal(err)
		}
		raw := context["mcp_config"].(json.RawMessage)
		if !bytes.Equal(raw, direct) {
			t.Fatal("session_context differs from runtime builder")
		}
		servers := decodeRuntimeMCPTestDocument(t, raw)
		if servers["local"].Headers["Authorization"] != "Bearer "+token {
			t.Fatal("session_context reused stale credentials")
		}
		if strings.Contains(string(record.Metadata), token) {
			t.Fatal("runtime credential persisted")
		}
	}
}

func runtimeMCPTestSource(channel string) json.RawMessage {
	return json.RawMessage(`{"mcp_servers":[{"name":"local","type":"url","url":"` + runtimeTestTunnelURL + `/` + channel + `"},{"name":"remote","type":"url","url":"https://docs.example/sse"}]}`)
}
func runtimeMCPTestIdentity() MCPRuntimeIdentity {
	return MCPRuntimeIdentity{CodeSessionID: "cse_test", SessionIngressToken: "current-token", APIBaseURL: "https://gateway.example"}
}
func runtimeMCPTestConfig() config.TunnelConfig {
	return config.TunnelConfig{PublicBaseURL: "https://oma.example", DomainSuffix: "tunnel.example"}
}
func decodeRuntimeMCPTestDocument(t *testing.T, raw json.RawMessage) map[string]mcpClientTarget {
	t.Helper()
	var document struct {
		Servers map[string]mcpClientTarget `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	return document.Servers
}

func TestMCPRuntimeConfigAbsentDocument(t *testing.T) {
	for _, source := range []string{"", "null", "{}", `{"mcp_servers":[]}`, `{"mcp_config":null}`} {
		document, err := BuildMCPRuntimeConfig(json.RawMessage(source), MCPRuntimeIdentity{}, config.TunnelConfig{})
		if err != nil || len(document) != 0 {
			t.Fatalf("absent MCP configuration produced %s: %v", document, err)
		}
	}
}
