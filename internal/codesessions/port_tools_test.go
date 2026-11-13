package codesessions

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
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

var legacyRuntimeTools = []string{"Bash", "Read", "Write", "Edit", "Grep", "Glob", "web_search", "web_fetch"}

func portToolCases() map[string]struct {
	tools       string
	enabledWant []string
	policy      string
} {
	withPort := append(slices.Clone(legacyRuntimeTools), sessionports.ToolName)
	return map[string]struct {
		tools       string
		enabledWant []string
		policy      string
	}{
		"configured disabled":       {`[{"type":"agent_toolset_20260401","configs":[{"name":"get_port_url","enabled":false}]}]`, legacyRuntimeTools, "deny"},
		"default disabled":          {`[{"type":"agent_toolset_20260401","default_config":{"enabled":false}}]`, []string{}, "deny"},
		"default disabled with ask": {`[{"type":"agent_toolset_20260401","default_config":{"enabled":false},"configs":[{"name":"ask_user_question","enabled":true}]}]`, []string{"ask_user_question"}, "deny"},
		"default":                   {`[{"type":"agent_toolset_20260401"}]`, withPort, "allow"},
		"default enabled":           {`[{"type":"agent_toolset_20260401","default_config":{"enabled":true,"permission_policy":{"type":"always_allow"}}}]`, withPort, "allow"},
		"default ask":               {`[{"type":"agent_toolset_20260401","default_config":{"enabled":true,"permission_policy":{"type":"always_ask"}}}]`, withPort, "ask"},
		"configured only":           {`[{"type":"agent_toolset_20260401","default_config":{"enabled":false},"configs":[{"name":"get_port_url","enabled":true}]}]`, []string{sessionports.ToolName}, "allow"},
		"configured ask":            {`[{"type":"agent_toolset_20260401","configs":[{"name":"get_port_url","enabled":true,"permission_policy":{"type":"always_ask"}}]}]`, withPort, "ask"},
	}
}

func portToolSnapshot(t *testing.T, tools string) toolPermissionSnapshot {
	t.Helper()
	var snapshot toolPermissionSnapshot
	if err := json.Unmarshal([]byte(`{"tools":`+tools+`}`), &snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestPortToolHiddenWhenServerDisabled(t *testing.T) {
	disabled := builtinToolCatalog{}
	for name, testCase := range portToolCases() {
		t.Run(name, func(t *testing.T) {
			snapshot := portToolSnapshot(t, testCase.tools)
			want := slices.DeleteFunc(slices.Clone(testCase.enabledWant), func(tool string) bool { return tool == sessionports.ToolName })
			if got := disabled.runtimeTools(snapshot.Tools); !reflect.DeepEqual(got, want) {
				t.Fatalf("runtime tools=%#v want %#v", got, want)
			}
			if got := disabled.enabledSandboxTools(snapshot.Tools); !reflect.DeepEqual(got, want) {
				t.Fatalf("launch tools=%#v want %#v", got, want)
			}
			policies := disabled.toolPolicies(snapshot.Tools)
			if _, found := policies[sessionports.ToolName]; found || len(policies) != len(sandboxToolNames)-1 {
				t.Fatalf("policies=%v", policies)
			}
			enabled, err := disabled.toolEnabled(json.RawMessage(`{"tools":`+testCase.tools+`}`), sessionports.ToolName)
			if err != nil || enabled {
				t.Fatalf("enabled=%v err=%v", enabled, err)
			}
			var child runtimeAgentSnapshot
			if err := json.Unmarshal([]byte(`{"name":"child","tools":`+testCase.tools+`}`), &child); err != nil {
				t.Fatal(err)
			}
			definition, err := runtimeSubagentDefinition(child, disabled)
			if err != nil || !reflect.DeepEqual(definition.Tools, want) {
				t.Fatalf("subagent tools=%#v want %#v err=%v", definition.Tools, want, err)
			}
		})
	}
}

func TestPortToolFollowsAgentToolsetWhenServerEnabled(t *testing.T) {
	enabled := builtinToolCatalogFor(portToolSettings(t))
	for name, testCase := range portToolCases() {
		t.Run(name, func(t *testing.T) {
			snapshot := portToolSnapshot(t, testCase.tools)
			if got := enabled.runtimeTools(snapshot.Tools); !reflect.DeepEqual(got, testCase.enabledWant) {
				t.Fatalf("runtime tools=%#v want %#v", got, testCase.enabledWant)
			}
			if got := enabled.enabledSandboxTools(snapshot.Tools); !reflect.DeepEqual(got, testCase.enabledWant) {
				t.Fatalf("launch tools=%#v want %#v", got, testCase.enabledWant)
			}
			if got := enabled.toolPolicies(snapshot.Tools)[sessionports.ToolName]; got != testCase.policy {
				t.Fatalf("policy=%q want %q", got, testCase.policy)
			}
			available, err := enabled.toolEnabled(json.RawMessage(`{"tools":`+testCase.tools+`}`), sessionports.ToolName)
			if err != nil || available != slices.Contains(testCase.enabledWant, sessionports.ToolName) {
				t.Fatalf("enabled=%v err=%v", available, err)
			}
			var child runtimeAgentSnapshot
			if err := json.Unmarshal([]byte(`{"name":"child","tools":`+testCase.tools+`}`), &child); err != nil {
				t.Fatal(err)
			}
			definition, err := runtimeSubagentDefinition(child, enabled)
			if err != nil || !reflect.DeepEqual(definition.Tools, testCase.enabledWant) {
				t.Fatalf("subagent tools=%#v want %#v err=%v", definition.Tools, testCase.enabledWant, err)
			}
		})
	}
}

func TestPortToolRequiresAgentToolset(t *testing.T) {
	enabled := builtinToolCatalogFor(portToolSettings(t))
	for _, tools := range []string{`[]`, `[{"type":"mcp_toolset","mcp_server_name":"docs"}]`} {
		available, err := enabled.toolEnabled(json.RawMessage(`{"tools":`+tools+`}`), sessionports.ToolName)
		if err != nil || available || len(enabled.runtimeTools(portToolSnapshot(t, tools).Tools)) != 0 {
			t.Fatalf("%s: enabled=%v err=%v", tools, available, err)
		}
	}
}

func TestSessionContextPublishesPortToolOnlyWhenServerEnabled(t *testing.T) {
	record := db.CodeSession{Metadata: json.RawMessage(`{"config":{"tools":[{"type":"agent_toolset_20260401"}]}}`)}
	for _, testCase := range []struct {
		catalog builtinToolCatalog
		want    []string
	}{
		{builtinToolCatalog{}, legacyRuntimeTools},
		{builtinToolCatalogFor(portToolSettings(t)), append(slices.Clone(legacyRuntimeTools), sessionports.ToolName)},
	} {
		context, err := sessionContextFromCodeSession(record, MCPRuntimeIdentity{}, config.Config{}, testCase.catalog)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(context["runtime_tools"], testCase.want) {
			t.Fatalf("runtime_tools=%#v want %#v", context["runtime_tools"], testCase.want)
		}
		_, published := context["runtime_tool_permissions"].(map[string]string)[sessionports.ToolName]
		if published != testCase.catalog.portURL {
			t.Fatalf("runtime_tool_permissions=%v", context["runtime_tool_permissions"])
		}
	}
}

func TestManagedAgentRuntimeConfigPublishesPortToolWhenServerEnabled(t *testing.T) {
	session := db.Session{AgentSnapshot: json.RawMessage(`{"tools":[{"type":"agent_toolset_20260401","default_config":{"enabled":false},"configs":[{"name":"get_port_url","enabled":true}]}]}`)}
	for _, testCase := range []struct {
		settings    *sessionports.Settings
		tools, perm string
	}{
		{nil, `[]`, `{"ask_user_question":"deny","bash":"deny","edit":"deny","glob":"deny","grep":"deny","read":"deny","web_fetch":"deny","web_search":"deny","write":"deny"}`},
		{portToolSettings(t), `["get_port_url"]`, `{"ask_user_question":"deny","bash":"deny","edit":"deny","get_port_url":"allow","glob":"deny","grep":"deny","read":"deny","web_fetch":"deny","web_search":"deny","write":"deny"}`},
	} {
		result, err := (&Service{}).WithSessionPorts(testCase.settings).managedAgentRuntimeConfig(t.Context(), session, json.RawMessage(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(result, &fields); err != nil {
			t.Fatal(err)
		}
		if string(fields["runtime_tools"]) != testCase.tools || string(fields["runtime_tool_permissions"]) != testCase.perm {
			t.Fatalf("runtime_tools=%s permissions=%s", fields["runtime_tools"], fields["runtime_tool_permissions"])
		}
	}
}

func TestPortToolPermissionDeniedWhenServerDisabled(t *testing.T) {
	permission, identity, err := (&Service{}).resolveToolPermission(t.Context(), "cse_port", sessionports.ToolName, "")
	if err != nil || permission != resolvedToolPermissionDeny || identity.ToolName != sessionports.ToolName {
		t.Fatalf("permission=%s identity=%#v err=%v", permission, identity, err)
	}
	catalog := builtinToolCatalogFor(portToolSettings(t))
	if !catalog.available(sessionports.ToolName) || !(builtinToolCatalog{}).available("Bash") || (builtinToolCatalog{}).available(sessionports.ToolName) {
		t.Fatal("port tool availability does not follow server settings")
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
