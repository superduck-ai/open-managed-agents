package environments

import (
	"encoding/base64"
	"encoding/json"
	"net"
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

type environmentManagerCommand struct {
	StdinPath    string
	Payload      []byte
	ShellCommand string
}

func managedAgentSessionConfig(session db.Session, resources []db.SessionResource) json.RawMessage {
	agentSnapshot := rawJSONObject(session.AgentSnapshot)
	mcpServers := arrayValue(agentSnapshot["mcp_servers"])
	tools := arrayValue(agentSnapshot["tools"])
	body := map[string]any{
		"origin":   "managed_agents_api",
		"model":    modelIDFromAgentSnapshot(session.AgentSnapshot),
		"sources":  managedAgentSources(resources),
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

func managedAgentMCPAllowedHosts(raw json.RawMessage) []string {
	snapshot := rawJSONObject(raw)
	servers := arrayValue(snapshot["mcp_servers"])
	hosts := make([]string, 0, len(servers))
	for _, value := range servers {
		server, ok := value.(map[string]any)
		if !ok {
			continue
		}
		host := hostnameFromURL(stringFromMap(server, "url"))
		if host != "" {
			hosts = append(hosts, host)
		}
	}
	return uniqueStrings(hosts)
}

func hostnameFromURL(raw string) string {
	parsed, err := urlpkg.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.ToLower(parsed.Hostname()))
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

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func managedAgentWorkDir(resources []db.SessionResource) string {
	for _, resource := range resources {
		var payload map[string]any
		if err := json.Unmarshal(resource.Payload, &payload); err != nil {
			continue
		}
		if mountPath, ok := payload["mount_path"].(string); ok && strings.TrimSpace(mountPath) != "" {
			return strings.TrimSpace(mountPath)
		}
	}
	return defaultEnvironmentWorkDir
}

func managedAgentSources(resources []db.SessionResource) []any {
	sources := make([]any, 0, len(resources))
	for _, resource := range resources {
		var payload map[string]any
		if err := json.Unmarshal(resource.Payload, &payload); err != nil {
			continue
		}
		switch resource.ResourceType {
		case "github_repository":
			source := map[string]any{
				"type":       "git_repository",
				"url":        stringFromMap(payload, "url"),
				"mount_path": stringFromMap(payload, "mount_path"),
			}
			if checkout, ok := payload["checkout"]; ok {
				source["checkout"] = checkout
			}
			sources = append(sources, source)
		case "file", "memory_store":
			sources = append(sources, payload)
		}
	}
	return sources
}

func modelIDFromAgentSnapshot(raw json.RawMessage) string {
	var snapshot map[string]any
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return ""
	}
	model, _ := snapshot["model"].(map[string]any)
	return strings.TrimSpace(stringFromMap(model, "id"))
}

func buildEnvironmentManagerV0Payload(codeSessionID string, workDir string, sessionConfig json.RawMessage, cfg config.Config) ([]byte, error) {
	startupContext := map[string]any{}
	if len(sessionConfig) > 0 && string(sessionConfig) != "null" {
		if err := json.Unmarshal(sessionConfig, &startupContext); err != nil {
			return nil, err
		}
	}
	startupContext["api_base_url"] = codeSessionSandboxAPIBaseURL(cfg)
	startupContext["use_code_sessions"] = true
	startupContext["session_id"] = codeSessionID
	environmentVariables := mapStringAnyValue(startupContext["environment_variables"])
	environmentVariables["CLAUDE_CODE_POST_FOR_SESSION_INGRESS_V2"] = "1"
	environmentVariables["CLAUDE_CODE_SESSION_ACCESS_TOKEN"] = codeSessionID
	environmentVariables["CLAUDE_CODE_USE_CCR_V2"] = "1"
	environmentVariables["CLAUDE_CODE_WORKER_EPOCH"] = "1"
	applyCodeSessionOTLPMetricsEnvironment(environmentVariables, stringFromMap(startupContext, "api_base_url"), codeSessionID, "1")
	startupContext["environment_variables"] = environmentVariables
	if _, ok := startupContext["sources"]; !ok {
		startupContext["sources"] = []any{}
	}
	if _, ok := startupContext["outcomes"]; !ok {
		startupContext["outcomes"] = []any{}
	}

	environment := map[string]any{
		"environment_type": "anthropic",
		"cwd":              firstNonEmpty(strings.TrimSpace(workDir), defaultEnvironmentWorkDir),
	}
	if claudeEnv := claudeRuntimeEnvironment(cfg); len(claudeEnv) > 0 {
		environment["environment"] = claudeEnv
	}

	auth := []any{
		map[string]any{
			"type":  "session_ingress",
			"token": codeSessionID,
		},
	}
	if strings.TrimSpace(cfg.AnthropicUpstreamAPIKey) != "" {
		auth = append(auth, map[string]any{
			"type":  "anthropic_api",
			"token": strings.TrimSpace(cfg.AnthropicUpstreamAPIKey),
		})
	}

	return json.Marshal(map[string]any{
		"startup_context": startupContext,
		"environment":     environment,
		"auth":            auth,
	})
}

func claudeRuntimeEnvironment(cfg config.Config) map[string]string {
	env := map[string]string{}
	if baseURL := strings.TrimRight(strings.TrimSpace(cfg.AnthropicUpstreamBaseURL), "/"); baseURL != "" {
		env["ANTHROPIC_BASE_URL"] = baseURL
	}
	if apiKey := strings.TrimSpace(cfg.AnthropicUpstreamAPIKey); apiKey != "" {
		env["ANTHROPIC_API_KEY"] = apiKey
	}
	return env
}

func applyCodeSessionOTLPMetricsEnvironment(environmentVariables map[string]any, apiBaseURL string, codeSessionID string, workerEpoch string) {
	if environmentVariables == nil {
		return
	}
	if stringFromMap(environmentVariables, "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT") != "" || stringFromMap(environmentVariables, "OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		return
	}
	exporters := stringFromMap(environmentVariables, "OTEL_METRICS_EXPORTER")
	if exporters != "" && !commaListContains(exporters, "otlp") {
		return
	}
	setDefaultEnvironmentVariable(environmentVariables, "OTEL_METRICS_EXPORTER", "otlp")
	setDefaultEnvironmentVariable(environmentVariables, "OTEL_EXPORTER_OTLP_METRICS_PROTOCOL", "http/protobuf")
	setDefaultEnvironmentVariable(environmentVariables, "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", codeSessionWorkerOTLPMetricsEndpoint(apiBaseURL, codeSessionID))
	environmentVariables["OTEL_EXPORTER_OTLP_HEADERS"] = ensureOTLPHeaders(
		stringFromMap(environmentVariables, "OTEL_EXPORTER_OTLP_HEADERS"),
		[]string{
			"Authorization=Bearer " + codeSessionID,
			"x-worker-epoch=" + workerEpoch,
		},
	)
}

func codeSessionWorkerOTLPMetricsEndpoint(apiBaseURL string, codeSessionID string) string {
	return strings.TrimRight(strings.TrimSpace(apiBaseURL), "/") + "/v1/code/sessions/" + urlpkg.PathEscape(codeSessionID) + "/worker/otlp/metrics"
}

func setDefaultEnvironmentVariable(environmentVariables map[string]any, key string, value string) {
	if stringFromMap(environmentVariables, key) == "" {
		environmentVariables[key] = value
	}
}

func ensureOTLPHeaders(raw string, required []string) string {
	pairs := make([]string, 0, len(required)+2)
	seen := map[string]struct{}{}
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		pairs = append(pairs, pair)
		key, _, ok := strings.Cut(pair, "=")
		if ok {
			seen[strings.ToLower(strings.TrimSpace(key))] = struct{}{}
		}
	}
	for _, pair := range required {
		key, _, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if _, ok := seen[normalizedKey]; ok {
			continue
		}
		pairs = append(pairs, pair)
		seen[normalizedKey] = struct{}{}
	}
	return strings.Join(pairs, ",")
}

func commaListContains(raw string, want string) bool {
	want = strings.TrimSpace(strings.ToLower(want))
	for _, item := range strings.Split(raw, ",") {
		if strings.TrimSpace(strings.ToLower(item)) == want {
			return true
		}
	}
	return false
}

func codeSessionSandboxAPIBaseURL(cfg config.Config) string {
	if baseURL := strings.TrimRight(firstNonEmpty(cfg.CodeSessionSandboxAPIBaseURL, cfg.CodeSessionAPIBaseURL, cfg.PublicBaseURL), "/"); baseURL != "" {
		return baseURL
	}
	port := "8080"
	addr := strings.TrimSpace(cfg.Addr)
	if addr != "" {
		if _, parsedPort, err := net.SplitHostPort(addr); err == nil && parsedPort != "" {
			port = parsedPort
		} else if strings.HasPrefix(addr, ":") && strings.TrimPrefix(addr, ":") != "" {
			port = strings.TrimPrefix(addr, ":")
		}
	}
	return "http://host.docker.internal:" + port
}

func buildEnvironmentManagerCommand(codeSessionID string, cfg config.Config, payload []byte) environmentManagerCommand {
	safeSessionID := strings.NewReplacer("/", "_", "\\", "_", " ", "_").Replace(codeSessionID)
	baseDir := path.Join("/tmp/claude-code-sessions", safeSessionID)
	stdinPath := path.Join(baseDir, "environment-manager.v0.json")
	logPath := path.Join(baseDir, "environment-manager.log")
	managerPath := firstNonEmpty(strings.TrimSpace(cfg.EnvironmentManagerPath), defaultEnvironmentManagerPath)
	agentVersion := firstNonEmpty(strings.TrimSpace(cfg.ClaudeAgentVersion), defaultClaudeAgentVersion)
	claudePath := firstNonEmpty(strings.TrimSpace(cfg.ClaudePath), defaultClaudePath)
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
		"nohup " + shellQuote(managerPath) +
			" task-run" +
			" --stdin" +
			" --session " + shellQuote(codeSessionID) +
			" --session-mode resume-cached" +
			" --claude-agent-version " + shellQuote("current") +
			" --claude-path " + shellQuote(claudePath) +
			" < " + shellQuote(stdinPath) +
			" > " + shellQuote(logPath) + " 2>&1 &",
		"printf '%s\\n' " + shellQuote("started environment-manager for "+codeSessionID),
	}, "\n")
	return environmentManagerCommand{
		StdinPath:    stdinPath,
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
