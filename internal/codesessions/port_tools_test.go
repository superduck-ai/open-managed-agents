package codesessions

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/sessionports"
)

const portToolSessionUUID = "6f1c3f7e-0d1b-4f53-9d39-1b1e4f0e9a10"

func portToolSettings(t *testing.T) *sessionports.Settings {
	t.Helper()
	codec, err := sessionports.NewLabelCodec(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return sessionports.NewWithCodec(config.SessionPortsConfig{DomainSuffix: "preview.example.com", URLScheme: "https", BlockedPorts: []int{9229}}, 3001, codec)
}

func TestSessionPortAddressRejectsUnavailablePorts(t *testing.T) {
	for name, testCase := range map[string]struct {
		settings *sessionports.Settings
		port     int
		kind     apperr.Kind
		message  string
	}{
		"disabled":         {nil, 4000, apperr.Conflict, "Session port URLs are not enabled on this server"},
		"zero":             {portToolSettings(t), 0, apperr.InvalidArgument, "port must be an integer between 1 and 65535"},
		"too large":        {portToolSettings(t), 65536, apperr.InvalidArgument, "port must be an integer between 1 and 65535"},
		"envd":             {portToolSettings(t), 49983, apperr.InvalidArgument, "port 49983 is reserved by the sandbox and has no session port URL"},
		"sandbox mcp":      {portToolSettings(t), 3001, apperr.InvalidArgument, "port 3001 is reserved by the sandbox and has no session port URL"},
		"configured block": {portToolSettings(t), 9229, apperr.InvalidArgument, "port 9229 is reserved by the sandbox and has no session port URL"},
	} {
		t.Run(name, func(t *testing.T) {
			address, err := sessionPortAddress(testCase.settings, portToolSessionUUID, testCase.port)
			app, ok := errors.AsType[*apperr.Error](err)
			if !ok || app.Kind != testCase.kind || app.PublicMessage != testCase.message || address != "" {
				t.Fatalf("address=%q err=%v", address, err)
			}
		})
	}
}

func TestSessionPortAddressMatchesPublicAPI(t *testing.T) {
	settings := portToolSettings(t)
	address, err := sessionPortAddress(settings, portToolSessionUUID, 4000)
	if err != nil {
		t.Fatal(err)
	}
	want, err := settings.URL(portToolSessionUUID, 4000)
	if err != nil || address != want {
		t.Fatalf("address=%q want=%q err=%v", address, want, err)
	}
}

func TestPortToolRequiresExplicitConfig(t *testing.T) {
	for name, testCase := range map[string]struct {
		tools   string
		enabled bool
	}{
		"no tools":            {`[]`, false},
		"mcp only":            {`[{"type":"mcp_toolset","mcp_server_name":"docs"}]`, false},
		"default toolset":     {`[{"type":"agent_toolset_20260401"}]`, false},
		"default enabled":     {`[{"type":"agent_toolset_20260401","default_config":{"enabled":true,"permission_policy":{"type":"always_allow"}}}]`, false},
		"configured disabled": {`[{"type":"agent_toolset_20260401","configs":[{"name":"get_port_url","enabled":false}]}]`, false},
		"configured":          {`[{"type":"agent_toolset_20260401","default_config":{"enabled":false},"configs":[{"name":"get_port_url","enabled":true,"permission_policy":{"type":"always_allow"}}]}]`, true},
		"configured ask":      {`[{"type":"agent_toolset_20260401","configs":[{"name":"get_port_url","enabled":true,"permission_policy":{"type":"always_ask"}}]}]`, true},
	} {
		t.Run(name, func(t *testing.T) {
			enabled, err := builtinToolEnabled(json.RawMessage(`{"tools":`+testCase.tools+`}`), sessionports.ToolName)
			if err != nil || enabled != testCase.enabled {
				t.Fatalf("enabled=%v err=%v", enabled, err)
			}
		})
	}
}

func TestRuntimeToolsWithoutPortToolAreUnchanged(t *testing.T) {
	legacy := []string{"Bash", "Read", "Write", "Edit", "Grep", "Glob", "web_search", "web_fetch"}
	for name, testCase := range map[string]struct {
		tools string
		want  []string
	}{
		"default":           {`[{"type":"agent_toolset_20260401"}]`, legacy},
		"default enabled":   {`[{"type":"agent_toolset_20260401","default_config":{"enabled":true}}]`, legacy},
		"with question":     {`[{"type":"agent_toolset_20260401","configs":[{"name":"ask_user_question","enabled":true}]}]`, append(append([]string{}, legacy...), "ask_user_question")},
		"read only":         {`[{"type":"agent_toolset_20260401","default_config":{"enabled":false},"configs":[{"name":"read","enabled":true}]}]`, []string{"Read"}},
		"port tool enabled": {`[{"type":"agent_toolset_20260401","default_config":{"enabled":false},"configs":[{"name":"get_port_url","enabled":true}]}]`, []string{"get_port_url"}},
	} {
		t.Run(name, func(t *testing.T) {
			var snapshot toolPermissionSnapshot
			if err := json.Unmarshal([]byte(`{"tools":`+testCase.tools+`}`), &snapshot); err != nil {
				t.Fatal(err)
			}
			if got := enabledSandboxTools(snapshot.Tools); !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("launch tools=%#v want %#v", got, testCase.want)
			}
			if got := runtimeBuiltinTools(snapshot.Tools); !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("runtime tools=%#v want %#v", got, testCase.want)
			}
		})
	}
}

func TestPortToolPublicIdentityMatchesBuiltinTools(t *testing.T) {
	identity := parseClaudeToolIdentity(sessionports.ToolName)
	if identity.Kind != "agent_toolset" || identity.ToolName != sessionports.ToolName {
		t.Fatalf("identity=%#v", identity)
	}
	eventType, name := toolPermissionPublicIdentity(sessionports.ToolName, identity)
	if eventType != "agent.tool_use" || name != sessionports.ToolName {
		t.Fatalf("event=%s name=%s", eventType, name)
	}
}
