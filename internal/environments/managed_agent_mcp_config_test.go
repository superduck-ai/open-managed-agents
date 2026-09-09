package environments

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestManagedAgentMCPLaunchFailureCleansOnlyNewSession(t *testing.T) {
	for _, recoveryID := range []string{"", "cse_test"} {
		t.Run("recovery="+recoveryID, func(t *testing.T) {
			runtime := &mcpConfigCodeSessionRuntime{result: codesessions.ManagedAgentCreateResult{CodeSessionID: "cse_test"}}
			runner := Runner{cfg: mcpConfigTestRuntimeConfig(), codeSessions: runtime}
			_, err := runner.createManagedAgentRuntimeLaunch(t.Context(), db.Environment{}, db.EnvironmentWork{}, managedAgentLaunchPreparation{
				SessionConfig: mcpConfigTestSource(), RecoveryCodeSessionID: recoveryID,
			})
			if !errors.Is(err, codesessions.ErrMCPRuntimeIdentityMissing) {
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
			var transport, remoteTransport string
			_ = json.Unmarshal(mcpConfigField(t, launch.Manager.Payload, "startup_context", "mcp_config", "mcpServers", "local", "type"), &transport)
			_ = json.Unmarshal(mcpConfigField(t, launch.Manager.Payload, "startup_context", "mcp_config", "mcpServers", "remote", "type"), &remoteTransport)
			if transport != "http" || remoteTransport != "sse" {
				t.Fatalf("Tunnel transport=%s, remote transport=%s", transport, remoteTransport)
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
	return json.RawMessage(`{"mcp_servers":[{"name":"local","type":"url","url":"https://oma.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef/sse"},{"name":"remote","type":"url","url":"https://docs.example/sse"}]}`)
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
		{name: "missing session", source: target, token: "test-token", want: codesessions.ErrMCPRuntimeIdentityMissing},
		{name: "missing token", source: target, session: "cse_test", want: codesessions.ErrMCPRuntimeIdentityMissing},
		{name: "missing server name", source: `{"mcp_servers":[{"url":"https://oma.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef"}]}`, want: codesessions.ErrMCPDeclarationInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := buildManagedAgentRuntimeMCPConfig(json.RawMessage(test.source), test.session, test.token, cfg)
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

func TestManagedAgentMCPBuildPreservesExtensionsAndRefreshesIdentity(t *testing.T) {
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
		built, err := buildManagedAgentRuntimeMCPConfig(source, "cse_test", token, cfg)
		if err != nil {
			t.Fatal(err)
		}
		payload, err := buildEnvironmentManagerV0Payload("cse_test", token, "oauth", 1, "", built, cfg, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range [][]string{{"extension"}, {"mcp_config"}, {"claude_code_args", "custom"}} {
			payloadPath := append([]string{"startup_context"}, path...)
			if !bytes.Equal(mcpConfigField(t, built, path...), mcpConfigField(t, payload, payloadPath...)) {
				t.Fatalf("payload changed exact field %v", path)
			}
		}
		for _, path := range [][]string{
			{"extension"}, {"mcp_servers"}, {"mcp_config", "vendor_extension"},
			{"mcp_config", "mcpServers", "docs"}, {"mcp_config", "mcpServers", "local", "vendor_extension"},
			{"mcp_config", "mcpServers", "local", "tools"}, {"claude_code_args", "custom"},
		} {
			if !bytes.Equal(mcpConfigField(t, source, path...), mcpConfigField(t, built, path...)) {
				t.Fatalf("field %v changed", path)
			}
		}
		var auth string
		if err := json.Unmarshal(mcpConfigField(t, built, "mcp_config", "mcpServers", "local", "headers", "Authorization"), &auth); err != nil {
			t.Fatal(err)
		}
		if auth != "Bearer "+token {
			t.Fatal("runtime configuration did not use the current credential")
		}
		var file managedAgentMCPConfigFile
		if err := json.Unmarshal(mcpConfigField(t, built, "mcp_config_file"), &file); err != nil {
			t.Fatal(err)
		}
		content, err := base64.StdEncoding.DecodeString(file.Content)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(content, mcpConfigField(t, built, "mcp_config")) {
			t.Fatal("MCP config file differs from the startup MCP document")
		}
	}
	if !bytes.Equal(source, original) {
		t.Fatal("runtime build mutated the persisted source")
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

func TestBuildManagedAgentRuntimeMCPConfigPreservesOrdinaryMCPFields(t *testing.T) {
	source := json.RawMessage(`{
		"mcp_servers":[{"name":"docs","type":"url","url":"https://mcp.example/mcp"}],
		"mcp_config":{"mcpServers":{"docs":{"type":"http","url":"https://mcp.example/mcp","headers":{"X-Existing":"value"}}}},
		"mcp_config_file":{"path":"/tmp/existing.json","content":"existing","mode":384}
	}`)
	built, err := buildManagedAgentRuntimeMCPConfig(source, "", "", config.Config{})
	if err != nil {
		t.Fatalf("buildManagedAgentRuntimeMCPConfig() error = %v", err)
	}
	if !bytes.Equal(mcpConfigField(t, built, "mcp_config", "mcpServers", "docs"), mcpConfigField(t, source, "mcp_config", "mcpServers", "docs")) {
		t.Fatal("ordinary MCP options changed")
	}
}

func TestBuildManagedAgentRuntimeMCPConfigRequiresSandboxReachableBaseURLForTunnel(t *testing.T) {
	source := json.RawMessage(`{"mcp_config":{"mcpServers":{"local":{"type":"http","url":"https://oma.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef"}}}}`)
	cfg := config.Config{Tunnel: config.TunnelConfig{PublicBaseURL: "https://oma.example", DomainSuffix: "tunnel.example"}}
	if _, err := buildManagedAgentRuntimeMCPConfig(source, "cse_test", "sk-ant-si-secret", cfg); err == nil {
		t.Fatal("buildManagedAgentRuntimeMCPConfig() accepted an empty sandbox API base URL for a Tunnel")
	}
}

func TestBuildManagedAgentRuntimeMCPConfigSeparatesConnections(t *testing.T) {
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
	built, err := buildManagedAgentRuntimeMCPConfig(
		source,
		"cse_test",
		"sk-ant-si-secret",
		config.Config{
			Tunnel:      config.TunnelConfig{PublicBaseURL: "https://oma.example", DomainSuffix: "tunnel.example"},
			CodeSession: config.CodeSessionConfig{SandboxAPIBaseURL: "http://host.docker.internal:38080/"},
		},
	)
	if err != nil {
		t.Fatalf("buildManagedAgentRuntimeMCPConfig() error = %v", err)
	}
	var startup map[string]any
	if err := json.Unmarshal(built, &startup); err != nil {
		t.Fatalf("decode built config: %v", err)
	}
	mcpServers := startup["mcp_servers"].([]any)
	if len(mcpServers) != 2 || mcpServers[0].(map[string]any)["name"] != "docs" || mcpServers[1].(map[string]any)["name"] != "local_tunnel" {
		t.Fatalf("built top-level mcp_servers = %#v", mcpServers)
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
		t.Fatalf("built MCP server = %#v", server)
	}
	headers := server["headers"].(map[string]any)
	if headers["Authorization"] != "Bearer sk-ant-si-secret" {
		t.Fatalf("built MCP headers = %#v", headers)
	}
	if !strings.Contains(string(built), "https://mcp.example/mcp") {
		t.Fatalf("built config lost ordinary MCP target: %s", built)
	}
	if strings.Contains(string(source), "sk-ant-si-secret") {
		t.Fatal("build mutated the persisted source config")
	}
	file := startup["mcp_config_file"].(map[string]any)
	if file["path"] != managedAgentMCPConfigPath || file["mode"] != float64(0o600) {
		t.Fatalf("built MCP config file = %#v", file)
	}
	content, err := base64.StdEncoding.DecodeString(file["content"].(string))
	if err != nil {
		t.Fatalf("decode built MCP config file: %v", err)
	}
	if !strings.Contains(string(content), wantURL) || !strings.Contains(string(content), "sk-ant-si-secret") || !strings.Contains(string(content), "https://mcp.example/mcp") || !strings.Contains(string(content), "X-Existing") {
		t.Fatalf("built MCP config file content = %s", content)
	}
}

func TestBuildManagedAgentRuntimeMCPConfigEscapesServerNamePath(t *testing.T) {
	source := json.RawMessage(`{"mcp_config":{"mcpServers":{"team/tools":{"type":"http","url":"https://oma.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef"}}}}`)
	built, err := buildManagedAgentRuntimeMCPConfig(
		source,
		"cse_test",
		"sk-ant-si-secret",
		config.Config{
			Tunnel:      config.TunnelConfig{PublicBaseURL: "https://oma.example", DomainSuffix: "tunnel.example"},
			CodeSession: config.CodeSessionConfig{SandboxAPIBaseURL: "http://host.docker.internal:38080"},
		},
	)
	if err != nil {
		t.Fatalf("buildManagedAgentRuntimeMCPConfig() error = %v", err)
	}
	if !strings.Contains(string(built), `/v2/ccr-sessions/cse_test/mcp/team%2Ftools`) {
		t.Fatalf("built config did not escape the server name path: %s", built)
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

func TestManagedAgentWithoutMCPDoesNotCreateConfigFile(t *testing.T) {
	source := json.RawMessage(`{"mcp_servers":[],"tools":[],"extension":9007199254740993}`)
	runtime, err := buildManagedAgentRuntimeMCPConfig(source, "", "", config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(runtime, source) {
		t.Fatal("empty MCP source acquired launch fields")
	}
}
