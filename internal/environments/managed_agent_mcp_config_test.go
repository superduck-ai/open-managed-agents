package environments

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"strings"
	"testing"
)

func TestManagedAgentMCPLaunchFailureCleansOnlyNewSession(t *testing.T) {
	for _, recoveryID := range []string{"", "cse_test"} {
		t.Run("recovery="+recoveryID, func(t *testing.T) {
			runtime := &mcpConfigCodeSessionRuntime{result: codesessions.ManagedAgentCreateResult{CodeSessionID: "cse_test"}}
			runner := Runner{cfg: mcpConfigTestRuntimeConfig(), codeSessions: runtime}
			_, err := runner.createManagedAgentRuntimeLaunch(t.Context(), db.Environment{}, db.EnvironmentWork{}, managedAgentLaunchPreparation{
				SessionConfig: mcpConfigTestSource(), RecoveryCodeSessionID: recoveryID,
			})
			if !errors.Is(err, errManagedAgentMCPIdentityMissing) {
				t.Fatalf("launch error: %v", err)
			}
			if (len(runtime.terminated) == 1) != (recoveryID == "") {
				t.Fatalf("unexpected termination: %v", runtime.terminated)
			}
		})
	}
}

func TestManagedAgentMCPLaunchUsesRuntimeConfigWithoutPersistingCredentials(t *testing.T) {
	for _, recoveryID := range []string{"", "cse_test"} {
		t.Run("recovery="+recoveryID, func(t *testing.T) {
			runtime := &mcpConfigCodeSessionRuntime{result: codesessions.ManagedAgentCreateResult{
				CodeSessionID: "cse_test", SessionIngressToken: "current-ingress-token", WorkerEpoch: 2,
			}}
			runner := Runner{cfg: mcpConfigTestRuntimeConfig(), codeSessions: runtime}
			source := mcpConfigTestSource()
			launch, err := runner.createManagedAgentRuntimeLaunch(t.Context(), db.Environment{}, db.EnvironmentWork{}, managedAgentLaunchPreparation{
				SessionConfig: source, RecoveryCodeSessionID: recoveryID,
			})
			if err != nil {
				t.Fatal(err)
			}
			if launch.Recovered != (recoveryID != "") || len(runtime.terminated) != 0 {
				t.Fatal("launch changed recovery or cleanup behavior")
			}
			if recoveryID == "" && !bytes.Equal(runtime.createdConfig, source) {
				t.Fatal("persisted config was replaced by runtime configuration")
			}
			if recoveryID != "" && runtime.recoveredID != recoveryID {
				t.Fatal("recovered a different Code Session")
			}
			var url, auth string
			if err := json.Unmarshal(mcpConfigField(t, launch.Manager.Payload, "startup_context", "mcp_config", "mcpServers", "local", "url"), &url); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(mcpConfigField(t, launch.Manager.Payload, "startup_context", "mcp_config", "mcpServers", "local", "headers", "Authorization"), &auth); err != nil {
				t.Fatal(err)
			}
			if url != "http://gateway.test/v2/ccr-sessions/cse_test/mcp/local" || auth != "Bearer current-ingress-token" {
				t.Fatal("launch did not receive the current Gateway target and credential")
			}

			var authEntries []struct {
				Type  string `json:"type"`
				Token string `json:"token"`
			}
			if err := json.Unmarshal(mcpConfigField(t, launch.Manager.Payload, "auth"), &authEntries); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, entry := range authEntries {
				if entry.Type == "session_ingress" {
					found = true
					if auth != "Bearer "+entry.Token {
						t.Fatal("MCP and session ingress received different credentials")
					}
				}
			}
			if !found {
				t.Fatal("missing session ingress startup authentication")
			}
			if !bytes.Equal(source, mcpConfigTestSource()) {
				t.Fatal("launch mutated the source config")
			}
		})
	}
}

type mcpConfigCodeSessionRuntime struct {
	result        codesessions.ManagedAgentCreateResult
	createdConfig json.RawMessage
	recoveredID   string
	terminated    []string
}

func (r *mcpConfigCodeSessionRuntime) CreateManagedAgentCodeSession(_ context.Context, input codesessions.ManagedAgentCreateInput) (codesessions.ManagedAgentCreateResult, error) {
	r.createdConfig = append(json.RawMessage(nil), input.Config...)
	return r.result, nil
}
func (r *mcpConfigCodeSessionRuntime) RecoverManagedAgentCodeSession(_ context.Context, input codesessions.ManagedAgentRecoverInput) (codesessions.ManagedAgentCreateResult, error) {
	r.recoveredID = input.CodeSessionID
	return r.result, nil
}
func (r *mcpConfigCodeSessionRuntime) TerminateManagedAgentCodeSession(_ context.Context, _ db.Session, id string) error {
	r.terminated = append(r.terminated, id)
	return nil
}

func mcpConfigTestRuntimeConfig() config.Config {
	return config.Config{Tunnel: config.TunnelConfig{PublicBaseURL: "https://oma.example"}, CodeSession: config.CodeSessionConfig{SandboxAPIBaseURL: "http://gateway.test"}}
}
func mcpConfigTestSource() json.RawMessage {
	return json.RawMessage(`{"mcp_config":{"mcpServers":{"local":{"type":"http","url":"https://oma.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef"}}}}`)
}

func TestManagedAgentMCPConfigRejectsInvalidRuntimeInput(t *testing.T) {
	cfg := config.Config{
		Tunnel:      config.TunnelConfig{PublicBaseURL: "https://oma.example"},
		CodeSession: config.CodeSessionConfig{SandboxAPIBaseURL: "http://gateway.test"},
	}
	target := `{"mcp_config":{"mcpServers":{"local":{"url":"https://oma.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef"}}}}`
	for _, test := range []struct {
		name, source, session, token string
		want                         error
	}{
		{name: "malformed startup JSON", source: `{`},
		{name: "malformed MCP document", source: `{"mcp_config":[]}`},
		{name: "missing session", source: target, token: "test-token", want: errManagedAgentMCPIdentityMissing},
		{name: "missing token", source: target, session: "cse_test", want: errManagedAgentMCPIdentityMissing},
		{name: "missing launchable config", source: `{"mcp_servers":[{"url":"https://oma.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef"}]}`, want: errManagedAgentMCPConfigMissing},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := projectManagedAgentRuntimeMCPConfig(json.RawMessage(test.source), test.session, test.token, cfg)
			if err == nil || (test.want != nil && !errors.Is(err, test.want)) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestManagedAgentSessionConfigReturnsEncodingFailure(t *testing.T) {
	_, err := managedAgentSessionConfig(db.Session{}, managedAgentRuntimeResources{sources: []json.RawMessage{json.RawMessage(`{`)}})
	if err == nil {
		t.Fatal("invalid runtime resource JSON was silently discarded")
	}
}

func TestManagedAgentMCPProjectionPreservesExtensionsAndRefreshesIdentity(t *testing.T) {
	source := json.RawMessage(`{
  "extension":{"precise":9007199254740993,"empty":[],"optional":null},
  "mcp_servers":[{"name":"local","url":"https://oma.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef"}],
  "mcp_config":{
   "vendor_extension":{"precise":9007199254740993},
   "mcpServers":{
    "docs":{"url":"https://docs.example/mcp","headers":{"X-Existing":"value"},"vendor_extension":{"precise":9007199254740993}},
    "local":{"type":"http","url":"https://oma.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef","tools":[{"name":"echo","enabled":false,"vendor_extension":9007199254740993}],"vendor_extension":{"precise":9007199254740993}}
   }
  },
  "claude_code_args":{"custom":{"precise":9007199254740993},"mcp-config":"old"}
 }`)
	original := append(json.RawMessage(nil), source...)
	cfg := config.Config{
		Tunnel:      config.TunnelConfig{PublicBaseURL: "https://oma.example"},
		CodeSession: config.CodeSessionConfig{SandboxAPIBaseURL: "http://gateway.test"},
	}
	for _, token := range []string{"first-epoch-token", "recovered-epoch-token"} {
		projected, err := projectManagedAgentRuntimeMCPConfig(source, "cse_test", token, cfg)
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range [][]string{
			{"extension"}, {"mcp_servers"}, {"mcp_config", "vendor_extension"},
			{"mcp_config", "mcpServers", "docs"}, {"mcp_config", "mcpServers", "local", "vendor_extension"},
			{"mcp_config", "mcpServers", "local", "tools"}, {"claude_code_args", "custom"},
		} {
			if !bytes.Equal(mcpConfigField(t, source, path...), mcpConfigField(t, projected, path...)) {
				t.Fatalf("field %v changed", path)
			}
		}
		var auth string
		if err := json.Unmarshal(mcpConfigField(t, projected, "mcp_config", "mcpServers", "local", "headers", "Authorization"), &auth); err != nil {
			t.Fatal(err)
		}
		if auth != "Bearer "+token {
			t.Fatal("runtime configuration did not use the current credential")
		}
		var file managedAgentMCPConfigFile
		if err := json.Unmarshal(mcpConfigField(t, projected, "mcp_config_file"), &file); err != nil {
			t.Fatal(err)
		}
		content, err := base64.StdEncoding.DecodeString(file.Content)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(content, mcpConfigField(t, projected, "mcp_config")) {
			t.Fatal("MCP config file differs from the startup MCP document")
		}
	}
	if !bytes.Equal(source, original) {
		t.Fatal("runtime projection mutated the persisted source")
	}
}

func mcpConfigField(t *testing.T, raw json.RawMessage, path ...string) json.RawMessage {
	t.Helper()
	for _, field := range path {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			t.Fatal(err)
		}
		var found bool
		raw, found = object[field]
		if !found {
			t.Fatalf("missing field %s", field)
		}
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		t.Fatal(err)
	}
	return compact.Bytes()
}

func TestProjectManagedAgentRuntimeMCPConfigPreservesOrdinaryMCPBytes(t *testing.T) {
	source := json.RawMessage(`{
		"mcp_servers":[{"name":"docs","type":"url","url":"https://mcp.example/mcp"}],
		"mcp_config":{"mcpServers":{"docs":{"type":"http","url":"https://mcp.example/mcp","headers":{"X-Existing":"value"}}}},
		"mcp_config_file":{"path":"/tmp/existing.json","content":"existing","mode":384}
	}`)
	projected, err := projectManagedAgentRuntimeMCPConfig(source, "", "", config.Config{})
	if err != nil {
		t.Fatalf("projectManagedAgentRuntimeMCPConfig() error = %v", err)
	}
	if !bytes.Equal(projected, source) {
		t.Fatalf("ordinary MCP config changed:\n got: %s\nwant: %s", projected, source)
	}
}

func TestProjectManagedAgentRuntimeMCPConfigRequiresSandboxReachableBaseURLForTunnel(t *testing.T) {
	source := json.RawMessage(`{"mcp_config":{"mcpServers":{"local":{"type":"http","url":"https://oma.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef"}}}}`)
	cfg := config.Config{Tunnel: config.TunnelConfig{PublicBaseURL: "https://oma.example", DomainSuffix: "tunnel.example"}}
	if _, err := projectManagedAgentRuntimeMCPConfig(source, "cse_test", "sk-ant-si-secret", cfg); err == nil {
		t.Fatal("projectManagedAgentRuntimeMCPConfig() accepted an empty sandbox API base URL for a Tunnel")
	}
}

func TestProjectManagedAgentRuntimeMCPConfigProjectsOnlyTunnel(t *testing.T) {
	source := json.RawMessage(`{
		"mcp_servers":[
			{"name":"docs","type":"url","url":"https://mcp.example/mcp"},
			{"name":"local_tunnel","type":"url","url":"https://oma.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef"}
		],
		"mcp_config":{"mcpServers":{
			"docs":{"type":"http","url":"https://mcp.example/mcp","headers":{"X-Existing":"value"},"tools":[{"name":"search","enabled":true}]},
			"local_tunnel":{"type":"http","url":"https://oma.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef","tools":[{"name":"echo","enabled":true}]}
		}},
		"mcp_config_file":{"path":"/tmp/stale.json","content":"stale","mode":384},
		"claude_code_args":{"mcp-config":"/tmp/stale.json"}
	}`)
	projected, err := projectManagedAgentRuntimeMCPConfig(
		source,
		"cse_test",
		"sk-ant-si-secret",
		config.Config{
			Tunnel:      config.TunnelConfig{PublicBaseURL: "https://oma.example", DomainSuffix: "tunnel.example"},
			CodeSession: config.CodeSessionConfig{SandboxAPIBaseURL: "http://host.docker.internal:38080/"},
		},
	)
	if err != nil {
		t.Fatalf("projectManagedAgentRuntimeMCPConfig() error = %v", err)
	}
	var startup map[string]any
	if err := json.Unmarshal(projected, &startup); err != nil {
		t.Fatalf("decode projected config: %v", err)
	}
	mcpServers := startup["mcp_servers"].([]any)
	if len(mcpServers) != 2 || mcpServers[0].(map[string]any)["name"] != "docs" || mcpServers[1].(map[string]any)["name"] != "local_tunnel" {
		t.Fatalf("projected top-level mcp_servers = %#v", mcpServers)
	}
	mcpConfig := startup["mcp_config"].(map[string]any)
	servers := mcpConfig["mcpServers"].(map[string]any)
	docs := servers["docs"].(map[string]any)
	if docs["url"] != "https://mcp.example/mcp" || docs["type"] != "http" {
		t.Fatalf("ordinary MCP server changed = %#v", docs)
	}
	docsHeaders := docs["headers"].(map[string]any)
	if docsHeaders["X-Existing"] != "value" || docsHeaders["Authorization"] != nil {
		t.Fatalf("ordinary MCP headers changed = %#v", docsHeaders)
	}
	server := servers["local_tunnel"].(map[string]any)
	wantURL := "http://host.docker.internal:38080/v2/ccr-sessions/cse_test/mcp/local_tunnel"
	if server["url"] != wantURL || server["type"] != "http" {
		t.Fatalf("projected MCP server = %#v", server)
	}
	headers := server["headers"].(map[string]any)
	if headers["Authorization"] != "Bearer sk-ant-si-secret" {
		t.Fatalf("projected MCP headers = %#v", headers)
	}
	if !strings.Contains(string(projected), "https://mcp.example/mcp") {
		t.Fatalf("projected config lost ordinary MCP target: %s", projected)
	}
	if strings.Contains(string(source), "sk-ant-si-secret") {
		t.Fatal("projection mutated the persisted source config")
	}
	file := startup["mcp_config_file"].(map[string]any)
	if file["path"] != managedAgentMCPConfigPath || file["mode"] != float64(0o600) {
		t.Fatalf("projected MCP config file = %#v", file)
	}
	content, err := base64.StdEncoding.DecodeString(file["content"].(string))
	if err != nil {
		t.Fatalf("decode projected MCP config file: %v", err)
	}
	if !strings.Contains(string(content), wantURL) || !strings.Contains(string(content), "sk-ant-si-secret") || !strings.Contains(string(content), "https://mcp.example/mcp") || !strings.Contains(string(content), "X-Existing") {
		t.Fatalf("projected MCP config file content = %s", content)
	}
}

func TestProjectManagedAgentRuntimeMCPConfigEscapesServerNamePath(t *testing.T) {
	source := json.RawMessage(`{"mcp_config":{"mcpServers":{"team/tools":{"type":"http","url":"https://oma.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef"}}}}`)
	projected, err := projectManagedAgentRuntimeMCPConfig(
		source,
		"cse_test",
		"sk-ant-si-secret",
		config.Config{
			Tunnel:      config.TunnelConfig{PublicBaseURL: "https://oma.example", DomainSuffix: "tunnel.example"},
			CodeSession: config.CodeSessionConfig{SandboxAPIBaseURL: "http://host.docker.internal:38080"},
		},
	)
	if err != nil {
		t.Fatalf("projectManagedAgentRuntimeMCPConfig() error = %v", err)
	}
	if !strings.Contains(string(projected), `/v2/ccr-sessions/cse_test/mcp/team%2Ftools`) {
		t.Fatalf("projected config did not escape the server name path: %s", projected)
	}
}

func TestManagedAgentMCPURLTunnelRecognition(t *testing.T) {
	t.Parallel()
	cfg := config.TunnelConfig{PublicBaseURL: "https://oma.example", DomainSuffix: "tunnel.example"}
	for _, test := range []struct {
		name       string
		url        string
		recognized bool
		wantError  bool
	}{
		{name: "malformed URL escape", url: "https://oma.example/v1/mcp/%zz", wantError: true},
		{name: "invalid canonical query", url: "https://oma.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef?x=1", recognized: true, wantError: true},
		{name: "canonical", url: "https://oma.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef", recognized: true},
		{name: "hostname alias", url: "https://abc.tunnel.example/main", recognized: true},
		{name: "third party matching path", url: "https://third-party.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef", recognized: false},
		{name: "ordinary", url: "https://mcp.example/mcp", recognized: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			recognized, err := managedAgentMCPURLIsTunnel(test.url, cfg)
			if (err != nil) != test.wantError {
				t.Fatalf("managedAgentMCPURLIsTunnel() error = %v, wantError %v", err, test.wantError)
			}
			if recognized != test.recognized {
				t.Fatalf("managedAgentMCPURLIsTunnel() = %v, want %v", recognized, test.recognized)
			}
		})
	}
}

func managedAgentRuntimeSourceValues(
	t *testing.T,
	sources []json.RawMessage,
) []any {
	t.Helper()
	raw, err := json.Marshal(sources)
	if err != nil {
		t.Fatalf("marshal runtime sources: %v", err)
	}
	var values []any
	if err := json.Unmarshal(raw, &values); err != nil {
		t.Fatalf("decode runtime sources: %v", err)
	}
	return values
}
