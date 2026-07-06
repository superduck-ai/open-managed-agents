package environments

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestBuildEnvironmentManagerPayloadAndCommand(t *testing.T) {
	cfg := config.Config{
		CodeSessionAPIBaseURL:        "http://127.0.0.1:18081/",
		CodeSessionSandboxAPIBaseURL: "http://host.docker.internal:18081/",
		AnthropicUpstreamBaseURL:     "https://api.anthropic.test/",
		AnthropicUpstreamAPIKey:      "sk-ant-test-secret",
		EnvironmentManagerPath:       "/opt/env manager/bin/environment-manager",
		ClaudeAgentVersion:           "2.1.120",
		ClaudePath:                   "/opt/claude path/bin/claude",
	}
	sessionConfig := json.RawMessage(`{"model":"claude-opus-4-8","sources":[{"type":"git_repository","url":"https://github.com/acme/widgets"}]}`)
	payload, err := buildEnvironmentManagerV0Payload("cse_test", "/workspace/widgets", sessionConfig, cfg)
	if err != nil {
		t.Fatalf("build payload: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	startup := body["startup_context"].(map[string]any)
	if startup["api_base_url"] != "http://host.docker.internal:18081" || startup["session_id"] != "cse_test" || startup["use_code_sessions"] != true {
		t.Fatalf("unexpected startup context: %#v", startup)
	}
	startupEnv := startup["environment_variables"].(map[string]any)
	if startupEnv["CLAUDE_CODE_SESSION_ACCESS_TOKEN"] != "cse_test" ||
		startupEnv["CLAUDE_CODE_POST_FOR_SESSION_INGRESS_V2"] != "1" ||
		startupEnv["CLAUDE_CODE_USE_CCR_V2"] != "1" ||
		startupEnv["CLAUDE_CODE_WORKER_EPOCH"] != "1" {
		t.Fatalf("unexpected startup environment variables: %#v", startupEnv)
	}
	if startupEnv["OTEL_METRICS_EXPORTER"] != "otlp" ||
		startupEnv["OTEL_EXPORTER_OTLP_METRICS_PROTOCOL"] != "http/protobuf" ||
		startupEnv["OTEL_EXPORTER_OTLP_METRICS_ENDPOINT"] != "http://host.docker.internal:18081/v1/code/sessions/cse_test/worker/otlp/metrics" ||
		startupEnv["OTEL_EXPORTER_OTLP_HEADERS"] != "Authorization=Bearer cse_test,x-worker-epoch=1" {
		t.Fatalf("unexpected otlp environment variables: %#v", startupEnv)
	}
	auths := body["auth"].([]any)
	sessionAuth := auths[0].(map[string]any)
	if sessionAuth["type"] != "session_ingress" || sessionAuth["token"] != "cse_test" {
		t.Fatalf("unexpected session auth: %#v", sessionAuth)
	}
	environment := body["environment"].(map[string]any)
	if environment["cwd"] != "/workspace/widgets" || environment["environment_type"] != "anthropic" {
		t.Fatalf("unexpected environment: %#v", environment)
	}
	claudeEnv := environment["environment"].(map[string]any)
	if claudeEnv["ANTHROPIC_BASE_URL"] != "https://api.anthropic.test" || claudeEnv["ANTHROPIC_API_KEY"] != "sk-ant-test-secret" {
		t.Fatalf("unexpected claude env: %#v", claudeEnv)
	}

	command := buildEnvironmentManagerCommand("cse_session with 'quote'/and/slash", cfg, payload)
	if !strings.Contains(command.StdinPath, "/tmp/claude-code-sessions/cse_session_with_'quote'_and_slash/environment-manager.v0.json") {
		t.Fatalf("unexpected stdin path: %s", command.StdinPath)
	}
	for _, want := range []string{
		"environment-manager binary missing or not executable: /opt/env manager/bin/environment-manager",
		"Claude binary missing or not executable: /opt/claude path/bin/claude",
		"task-run --stdin --session 'cse_session with '\"'\"'quote'\"'\"'/and/slash'",
		"--session-mode resume-cached",
		"--claude-agent-version 'current'",
		"--claude-path '/opt/claude path/bin/claude'",
		"export SKIP_PLUGIN_MARKETPLACE=${SKIP_PLUGIN_MARKETPLACE:-true}",
		"Claude binary version mismatch: expected 2.1.120",
	} {
		if !strings.Contains(command.ShellCommand, want) {
			t.Fatalf("command missing %q in:\n%s", want, command.ShellCommand)
		}
	}
	if strings.Contains(command.ShellCommand, "sk-ant-test-secret") {
		t.Fatalf("command leaked anthropic api key:\n%s", command.ShellCommand)
	}
}

func TestBuildEnvironmentManagerPayloadPreservesCustomOTLPMetricsEnvironment(t *testing.T) {
	cfg := config.Config{CodeSessionSandboxAPIBaseURL: "http://host.docker.internal:18081/"}
	sessionConfig := json.RawMessage(`{"environment_variables":{
		"OTEL_METRICS_EXPORTER":"console",
		"OTEL_EXPORTER_OTLP_HEADERS":"x-custom=value"
	}}`)
	payload, err := buildEnvironmentManagerV0Payload("cse_test", "", sessionConfig, cfg)
	if err != nil {
		t.Fatalf("build payload: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	startup := body["startup_context"].(map[string]any)
	startupEnv := startup["environment_variables"].(map[string]any)
	if startupEnv["OTEL_METRICS_EXPORTER"] != "console" {
		t.Fatalf("OTEL_METRICS_EXPORTER = %q, want console", startupEnv["OTEL_METRICS_EXPORTER"])
	}
	if _, ok := startupEnv["OTEL_EXPORTER_OTLP_METRICS_ENDPOINT"]; ok {
		t.Fatalf("unexpected default otlp metrics endpoint for custom exporter: %#v", startupEnv)
	}
	if startupEnv["OTEL_EXPORTER_OTLP_HEADERS"] != "x-custom=value" {
		t.Fatalf("OTEL_EXPORTER_OTLP_HEADERS = %q, want x-custom=value", startupEnv["OTEL_EXPORTER_OTLP_HEADERS"])
	}
	if startupEnv["CLAUDE_CODE_WORKER_EPOCH"] != "1" {
		t.Fatalf("CLAUDE_CODE_WORKER_EPOCH = %q, want 1", startupEnv["CLAUDE_CODE_WORKER_EPOCH"])
	}
}

func TestManagedAgentSessionConfigIncludesMCPConfig(t *testing.T) {
	session := db.Session{
		AgentSnapshot: json.RawMessage(`{
			"model":{"id":"claude-opus-4-8"},
			"mcp_servers":[{"type":"url","name":"notion","url":"https://mcp.notion.com/mcp"}],
			"tools":[{
				"type":"mcp_toolset",
				"mcp_server_name":"notion",
				"default_config":{"enabled":true,"permission_policy":{"type":"always_ask"}},
				"configs":[
					{"name":"search","enabled":true,"permission_policy":{"type":"always_allow"}},
					{"name":"delete_page","enabled":false,"permission_policy":{"type":"always_ask"}}
				]
			}]
		}`),
		VaultIDs: json.RawMessage(`["vault_cred_123"]`),
	}

	raw := managedAgentSessionConfig(session, nil)
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode session config: %v", err)
	}
	if body["model"] != "claude-opus-4-8" {
		t.Fatalf("model = %v", body["model"])
	}
	mcpConfig := body["mcp_config"].(map[string]any)
	servers := mcpConfig["mcpServers"].(map[string]any)
	notion := servers["notion"].(map[string]any)
	if notion["type"] != "http" || notion["url"] != "https://mcp.notion.com/mcp" {
		t.Fatalf("unexpected notion mcp config: %#v", notion)
	}
	toolConfigs := notion["tools"].([]any)
	search := toolConfigs[0].(map[string]any)
	if search["name"] != "search" || search["enabled"] != true || search["permission_policy"] != "allow" {
		t.Fatalf("unexpected search tool config: %#v", search)
	}
	deletePage := toolConfigs[1].(map[string]any)
	if deletePage["name"] != "delete_page" || deletePage["enabled"] != false || deletePage["permission_policy"] != "ask" {
		t.Fatalf("unexpected delete_page tool config: %#v", deletePage)
	}
	vaultIDs := body["vault_ids"].([]any)
	if len(vaultIDs) != 1 || vaultIDs[0] != "vault_cred_123" {
		t.Fatalf("unexpected vault ids: %#v", vaultIDs)
	}
	if hosts := managedAgentMCPAllowedHosts(session.AgentSnapshot); !reflect.DeepEqual(hosts, []string{"mcp.notion.com"}) {
		t.Fatalf("mcp hosts = %#v", hosts)
	}
	claudeArgs := body["claude_code_args"].(map[string]any)
	if claudeArgs["mcp-config"] != managedAgentMCPConfigPath {
		t.Fatalf("claude args = %#v", claudeArgs)
	}
	mcpConfigFile := body["mcp_config_file"].(map[string]any)
	if mcpConfigFile["path"] != managedAgentMCPConfigPath || mcpConfigFile["mode"] != float64(384) {
		t.Fatalf("unexpected mcp config file metadata: %#v", mcpConfigFile)
	}
	content, err := base64.StdEncoding.DecodeString(mcpConfigFile["content"].(string))
	if err != nil {
		t.Fatalf("decode mcp config file content: %v", err)
	}
	var fileConfig map[string]any
	if err := json.Unmarshal(content, &fileConfig); err != nil {
		t.Fatalf("decode mcp config file json: %v", err)
	}
	if !reflect.DeepEqual(fileConfig, mcpConfig) {
		t.Fatalf("mcp config file = %#v, want %#v", fileConfig, mcpConfig)
	}
}
