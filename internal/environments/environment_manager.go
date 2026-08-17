package environments

import (
	"encoding/base64"
	"encoding/json"
	urlpkg "net/url"
	"path"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const (
	defaultEnvironmentManagerPath = "/usr/local/bin/environment-manager"
	defaultClaudeAgentVersion     = "2.1.120"
	defaultClaudePath             = "/opt/claude-code/bin/claude"
	defaultEnvironmentWorkDir     = "/home/user"
	managedAgentMCPConfigPath     = "/tmp/managed-agent-mcp-config.json"
)

func managedAgentSessionConfig(
	session db.Session,
	runtimeResources managedAgentRuntimeResources,
) json.RawMessage {
	agentSnapshot := rawJSONObject(session.AgentSnapshot)
	mcpServers := arrayValue(agentSnapshot["mcp_servers"])
	tools := arrayValue(agentSnapshot["tools"])
	body := map[string]any{
		"origin":   "managed_agents_api",
		"model":    modelIDFromAgentSnapshot(session.AgentSnapshot),
		"sources":  runtimeResources.sources,
		"outcomes": []any{},
	}
	if len(mcpServers) > 0 {
		body["mcp_servers"] = mcpServers
		if mcpConfig := managedAgentMCPConfig(mcpServers, tools); len(mcpConfig) > 0 {
			body["mcp_config"] = mcpConfig
			body["mcp_config_file"] = managedAgentMCPConfigFile(mcpConfig)
			body["claude_code_args"] = map[string]string{"mcp-config": managedAgentMCPConfigPath}
		}
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	if vaultIDs := rawJSONArray(session.VaultIDs); len(vaultIDs) > 0 {
		body["vault_ids"] = vaultIDs
	}
	raw, _ := json.Marshal(body)
	return raw
}

func managedAgentMCPConfig(mcpServers []any, tools []any) map[string]any {
	toolsets := mcpToolsetsByServer(tools)
	servers := map[string]any{}
	for _, value := range mcpServers {
		server, ok := value.(map[string]any)
		if !ok {
			continue
		}
		name := stringFromMap(server, "name")
		serverURL := stringFromMap(server, "url")
		if name == "" || serverURL == "" {
			continue
		}
		config := map[string]any{
			"type": mcpServerTransportType(stringFromMap(server, "type"), serverURL),
			"url":  serverURL,
		}
		if toolset, ok := toolsets[name]; ok {
			if toolConfigs := mcpServerToolConfigs(toolset["configs"]); len(toolConfigs) > 0 {
				config["tools"] = toolConfigs
			}
		}
		servers[name] = config
	}
	if len(servers) == 0 {
		return nil
	}
	return map[string]any{"mcpServers": servers}
}

func managedAgentMCPConfigFile(mcpConfig map[string]any) map[string]any {
	content, err := json.Marshal(mcpConfig)
	if err != nil {
		return nil
	}
	return map[string]any{
		"path":    managedAgentMCPConfigPath,
		"content": base64.StdEncoding.EncodeToString(content),
		"mode":    0o600,
	}
}

func mcpToolsetsByServer(tools []any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, value := range tools {
		tool, ok := value.(map[string]any)
		if !ok || stringFromMap(tool, "type") != "mcp_toolset" {
			continue
		}
		serverName := stringFromMap(tool, "mcp_server_name")
		if serverName == "" {
			continue
		}
		out[serverName] = tool
	}
	return out
}

func mcpServerToolConfigs(value any) []any {
	configs := arrayValue(value)
	out := make([]any, 0, len(configs))
	for _, item := range configs {
		config, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := stringFromMap(config, "name")
		if name == "" {
			continue
		}
		tool := map[string]any{"name": name}
		if enabled, ok := config["enabled"].(bool); ok {
			tool["enabled"] = enabled
		}
		if policy := mcpPermissionPolicy(config["permission_policy"]); policy != "" {
			tool["permission_policy"] = policy
		}
		out = append(out, tool)
	}
	return out
}

func mcpPermissionPolicy(value any) string {
	object, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	switch stringFromMap(object, "type") {
	case "always_allow", "allow":
		return "allow"
	case "always_ask", "ask":
		return "ask"
	default:
		return ""
	}
}

func mcpServerTransportType(serverType string, rawURL string) string {
	switch strings.TrimSpace(strings.ToLower(serverType)) {
	case "sse":
		return "sse"
	case "http", "ws":
		return strings.TrimSpace(strings.ToLower(serverType))
	case "websocket":
		return "ws"
	}
	parsed, err := urlpkg.Parse(strings.TrimSpace(rawURL))
	if err == nil && strings.HasSuffix(strings.TrimRight(strings.ToLower(parsed.Path), "/"), "/sse") {
		return "sse"
	}
	return "http"
}

func rawJSONObject(raw json.RawMessage) map[string]any {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return map[string]any{}
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return map[string]any{}
	}
	return object
}

func rawJSONArray(raw json.RawMessage) []any {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return nil
	}
	var values []any
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil
	}
	return values
}

func mapStringAnyValue(value any) map[string]any {
	object, ok := value.(map[string]any)
	if ok && object != nil {
		return object
	}
	stringMap, ok := value.(map[string]string)
	if ok {
		object = make(map[string]any, len(stringMap))
		for key, item := range stringMap {
			object[key] = item
		}
		return object
	}
	return map[string]any{}
}

func arrayValue(value any) []any {
	values, _ := value.([]any)
	return values
}

func modelIDFromAgentSnapshot(raw json.RawMessage) string {
	var snapshot map[string]any
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return ""
	}
	model, _ := snapshot["model"].(map[string]any)
	return strings.TrimSpace(stringFromMap(model, "id"))
}

// buildEnvironmentManagerV0Payload 把 code session 映射为 environment-manager v0 合同；relay、runtime API 与 ingress 绑定同一 external ID，真实上游凭证不进入 sandbox。
func buildEnvironmentManagerV0Payload(codeSessionID string, sessionIngressToken string, oauthAccessToken string, workDir string, sessionConfig json.RawMessage, cfg config.Config) ([]byte, error) {
	startupContext := map[string]any{}
	if len(sessionConfig) > 0 && string(sessionConfig) != "null" {
		if err := json.Unmarshal(sessionConfig, &startupContext); err != nil {
			return nil, err
		}
	}
	apiBaseURL := codeSessionSandboxAPIBaseURL(cfg)
	startupContext["api_base_url"] = apiBaseURL
	startupContext["use_code_sessions"] = true
	startupContext["session_id"] = codeSessionID
	environmentVariables := mapStringAnyValue(startupContext["environment_variables"])
	environmentVariables["CLAUDE_CODE_REMOTE"] = "true" // 进入 remote-session 路径并初始化 CCR relay。
	environmentVariables["CLAUDE_CODE_POST_FOR_SESSION_INGRESS_V2"] = "1"
	delete(environmentVariables, "CLAUDE_CODE_SESSION_ACCESS_TOKEN") // 避免遮蔽 environment-manager 注入的 WebSocket auth FD。
	environmentVariables["CLAUDE_CODE_USE_CCR_V2"] = "1"
	environmentVariables["CCR_UPSTREAM_PROXY_ENABLED"] = "1" // 还需 REMOTE_SESSION_ID 和 /run/ccr/session_token 才会注入 HTTPS_PROXY。
	for key, value := range claudeRuntimeModelEnvironment(stringFromMap(startupContext, "model")) {
		environmentVariables[key] = value
	}
	applyCodeSessionOTLPEnvironment(environmentVariables, cfg.Observability, apiBaseURL, codeSessionID, sessionIngressToken)
	startupContext["environment_variables"] = environmentVariables
	if _, ok := startupContext["sources"]; !ok {
		startupContext["sources"] = []any{}
	}
	if _, ok := startupContext["outcomes"]; !ok {
		startupContext["outcomes"] = []any{}
	}
	environment := map[string]any{ // 上游地址和 API key 不进入 environment；Claude 回连 startup_context.api_base_url。
		"environment_type": "anthropic",
		"cwd":              firstNonEmpty(strings.TrimSpace(workDir), defaultEnvironmentWorkDir),
	}
	// 两种 auth 用途独立：session_ingress 供 worker/relay；anthropic_oauth 通过 OAuth FD 鉴权 /v1/messages。
	auth := []AuthConfig{
		{
			Type:  "session_ingress",
			Token: sessionIngressToken,
		},
		{
			Type:  "anthropic_oauth",
			Token: oauthAccessToken,
		},
	}
	// environment-manager 将两个 token 分别接入 WebSocket auth FD 与 OAuth auth FD，不能合并语义。
	return json.Marshal(map[string]any{
		"startup_context": startupContext,
		"environment":     environment,
		"auth":            auth,
	})
}

func claudeRuntimeModelEnvironment(modelID string) map[string]string {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return nil
	}
	return map[string]string{
		"ANTHROPIC_MODEL":                modelID,
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   modelID,
		"ANTHROPIC_DEFAULT_SONNET_MODEL": modelID,
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":  modelID,
	}
}

// applyCodeSessionOTLPEnvironment 对采集选项只补默认值；连接 OMA 所需的
// endpoint 和 Authorization 由平台覆盖，兼容不会动态配置 OTLP 的旧版
// environment-manager。OTLP ingress 仍会校验 session token 与 active lease。
func applyCodeSessionOTLPEnvironment(environmentVariables map[string]any, cfg config.ObservabilityConfig, apiBaseURL, codeSessionID, sessionIngressToken string) {
	if !cfg.Enabled {
		return
	}
	setDefaultEnvironmentVariable(environmentVariables, "CLAUDE_CODE_ENABLE_TELEMETRY", "1")
	// Runner shell defaults this to 1 to skip marketplace/auto-update. Claude Code
	// treats a non-empty value as "essential traffic only" and will not export OTEL.
	setDefaultEnvironmentVariable(environmentVariables, "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "")
	setDefaultEnvironmentVariable(environmentVariables, "OTEL_METRICS_EXPORTER", "otlp")
	setDefaultEnvironmentVariable(environmentVariables, "OTEL_EXPORTER_OTLP_METRICS_PROTOCOL", "http/protobuf")
	setDefaultEnvironmentVariable(environmentVariables, "OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE", "delta")
	setDefaultEnvironmentVariable(environmentVariables, "OTEL_LOGS_EXPORTER", "otlp")
	setDefaultEnvironmentVariable(environmentVariables, "OTEL_EXPORTER_OTLP_LOGS_PROTOCOL", "http/protobuf")
	setDefaultEnvironmentVariable(environmentVariables, "ENABLE_BETA_TRACING_DETAILED", "1")
	if cfg.ContentCaptureEnabled {
		// Without these grants Claude Code exports user_prompt="<REDACTED>" and
		// omits tool input/output; detailed tracing alone only carries structure.
		setDefaultEnvironmentVariable(environmentVariables, "OTEL_LOG_USER_PROMPTS", "1")
		setDefaultEnvironmentVariable(environmentVariables, "OTEL_LOG_TOOL_DETAILS", "1")
		setDefaultEnvironmentVariable(environmentVariables, "OTEL_LOG_TOOL_CONTENT", "1")
	}

	otlpBaseURL := strings.TrimRight(apiBaseURL, "/") + "/v1/code/sessions/" + urlpkg.PathEscape(codeSessionID) + "/worker/otlp"
	headers := "Authorization=Bearer " + sessionIngressToken
	environmentVariables["OTEL_EXPORTER_OTLP_HEADERS"] = ""
	environmentVariables["OTEL_EXPORTER_OTLP_METRICS_ENDPOINT"] = otlpBaseURL + "/metrics"
	environmentVariables["OTEL_EXPORTER_OTLP_METRICS_HEADERS"] = headers
	environmentVariables["OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"] = otlpBaseURL + "/logs"
	environmentVariables["OTEL_EXPORTER_OTLP_LOGS_HEADERS"] = headers
	environmentVariables["BETA_TRACING_ENDPOINT"] = otlpBaseURL
	environmentVariables["OTEL_EXPORTER_OTLP_TRACES_HEADERS"] = headers
}

func setDefaultEnvironmentVariable(environmentVariables map[string]any, key string, value string) {
	if _, configured := environmentVariables[key]; !configured {
		environmentVariables[key] = value
	}
}

func codeSessionSandboxAPIBaseURL(cfg config.Config) string {
	return strings.TrimRight(strings.TrimSpace(cfg.CodeSession.SandboxAPIBaseURL), "/")
}

func buildEnvironmentManagerCommand(codeSessionID string, cfg config.Config, payload []byte) environmentManagerCommand {
	safeSessionID := strings.NewReplacer("/", "_", "\\", "_", " ", "_").Replace(codeSessionID)
	baseDir := path.Join("/tmp/claude-code-sessions", safeSessionID)
	logPath := path.Join(baseDir, "environment-manager.log")
	managerPath := firstNonEmpty(strings.TrimSpace(cfg.EnvironmentRunner.ManagerPath), defaultEnvironmentManagerPath)
	agentVersion := firstNonEmpty(strings.TrimSpace(cfg.EnvironmentRunner.ClaudeAgentVersion), defaultClaudeAgentVersion)
	claudePath := firstNonEmpty(strings.TrimSpace(cfg.EnvironmentRunner.ClaudePath), defaultClaudePath)
	versionPattern := `s/.*\([0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*\).*/\1/p`
	command := strings.Join([]string{
		"set -eu",
		"mkdir -p " + shellQuote(baseDir),
		"if [ ! -x " + shellQuote(managerPath) + " ]; then printf '%s\\n' " + shellQuote("environment-manager binary missing or not executable: "+managerPath) + " >&2; exit 1; fi",
		"if [ ! -x " + shellQuote(claudePath) + " ]; then printf '%s\\n' " + shellQuote("Claude binary missing or not executable: "+claudePath) + " >&2; exit 1; fi",
		"claude_version=$(" + shellQuote(claudePath) + " --version | sed -n " + shellQuote(versionPattern) + " | head -n 1)",
		"if [ \"$claude_version\" != " + shellQuote(agentVersion) + " ]; then printf '%s\\n' " + shellQuote("Claude binary version mismatch: expected "+agentVersion) + " >&2; exit 1; fi",
		"export CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=${CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC:-1}",
		"export CLAUDE_CODE_DISABLE_OFFICIAL_MARKETPLACE_AUTOINSTALL=${CLAUDE_CODE_DISABLE_OFFICIAL_MARKETPLACE_AUTOINSTALL:-1}",
		"export CLAUDE_CODE_ENABLE_BACKGROUND_PLUGIN_REFRESH=${CLAUDE_CODE_ENABLE_BACKGROUND_PLUGIN_REFRESH:-0}",
		"export SKIP_PLUGIN_MARKETPLACE=${SKIP_PLUGIN_MARKETPLACE:-true}",
		// E2B 负责把该命令作为后台进程启动；payload 通过进程 stdin 发送，不进入命令行或沙箱文件系统。
		"exec " + shellQuote(managerPath) +
			" task-run" +
			" --session " + shellQuote(codeSessionID) +
			" --session-mode resume-cached" +
			" --claude-agent-version " + shellQuote("current") +
			" --claude-path " + shellQuote(claudePath) +
			" > " + shellQuote(logPath) + " 2>&1",
	}, "\n")
	return environmentManagerCommand{
		Payload:      append([]byte(nil), payload...),
		ShellCommand: command,
	}
}

func stringFromMap(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
